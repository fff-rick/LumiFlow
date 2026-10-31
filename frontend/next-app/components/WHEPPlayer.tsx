"use client"

import { useEffect, useRef, useState } from "react"

export default function WHEPPlayer({ url }: { url: string }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    const controller = new AbortController()
    const peer = new RTCPeerConnection()
    let resourceURL = ""

    peer.addTransceiver("audio", { direction: "recvonly" })
    peer.addTransceiver("video", { direction: "recvonly" })
    peer.ontrack = (event) => {
      if (!videoRef.current) return
      videoRef.current.srcObject =
        event.streams[0] ?? new MediaStream([event.track])
    }

    async function play() {
      const offer = await peer.createOffer()
      await peer.setLocalDescription(offer)
      await waitForICE(peer)
      const response = await fetch(url, {
        method: "POST",
        headers: { "content-type": "application/sdp" },
        body: peer.localDescription?.sdp,
        signal: controller.signal,
      })
      if (!response.ok) throw new Error(`WHEP 协商失败（${response.status}）`)
      const location = response.headers.get("location")
      if (location) resourceURL = new URL(location, url).toString()
      await peer.setRemoteDescription({
        type: "answer",
        sdp: await response.text(),
      })
      await videoRef.current?.play()
    }

    void play().catch((reason) => {
      if (controller.signal.aborted) return
      setError(reason instanceof Error ? reason.message : "视频播放失败")
    })
    return () => {
      controller.abort()
      peer.close()
      if (resourceURL)
        void fetch(resourceURL, { method: "DELETE", keepalive: true }).catch(
          () => undefined
        )
    }
  }, [url])

  return (
    <div className="absolute inset-0 bg-black">
      <video
        autoPlay
        className="h-full w-full object-contain"
        controls
        muted
        playsInline
        ref={videoRef}
      />
      {error && (
        <p className="absolute inset-x-5 top-5 rounded-xl bg-red-950/90 px-4 py-3 text-sm text-red-100">
          {error}
        </p>
      )}
    </div>
  )
}

function waitForICE(peer: RTCPeerConnection) {
  if (peer.iceGatheringState === "complete") return Promise.resolve()
  return new Promise<void>((resolve) => {
    const timeout = window.setTimeout(done, 3000)
    function done() {
      window.clearTimeout(timeout)
      peer.removeEventListener("icegatheringstatechange", change)
      resolve()
    }
    function change() {
      if (peer.iceGatheringState === "complete") done()
    }
    peer.addEventListener("icegatheringstatechange", change)
  })
}

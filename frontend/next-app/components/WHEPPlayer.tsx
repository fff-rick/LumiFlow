"use client"

import { useEffect, useRef, useState } from "react"

export default function WHEPPlayer({ url }: { url: string }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [error, setError] = useState("")
  const [attempt, setAttempt] = useState(0)

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
    peer.onconnectionstatechange = () => {
      if (peer.connectionState === "failed") setError("视频连接失败，请重试")
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
      setError("")
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
  }, [attempt, url])

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
        <div className="absolute inset-x-5 top-5 flex items-center justify-between gap-4 rounded-xl bg-red-950/90 px-4 py-3 text-sm text-red-100">
          <p>{error}</p>
          <button
            className="shrink-0 rounded-lg border border-red-200/40 px-3 py-1 font-semibold hover:bg-red-100/10"
            onClick={() => {
              setError("")
              setAttempt((value) => value + 1)
            }}
            type="button"
          >
            重试
          </button>
        </div>
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

"use client"

import Hls from "hls.js"
import { useEffect, useRef, useState } from "react"

export default function HLSPlayer({ url }: { url: string }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    let retry = 0

    if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url
      void video.play().catch(() => undefined)
      return () => {
        video.removeAttribute("src")
        video.load()
      }
    }
    if (!Hls.isSupported()) {
      const unsupported = window.setTimeout(
        () => setError("当前浏览器不支持 HLS 自适应播放"),
        0
      )
      return () => window.clearTimeout(unsupported)
    }

    const hls = new Hls({ liveSyncDurationCount: 3 })
    hls.loadSource(url)
    hls.attachMedia(video)
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      setError("")
      void video.play().catch(() => undefined)
    })
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (!data.fatal) return
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
        setError("自适应流正在生成，正在重试…")
        window.clearTimeout(retry)
        retry = window.setTimeout(() => hls.startLoad(), 1500)
        return
      }
      if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
        hls.recoverMediaError()
        return
      }
      setError("自适应播放失败，请切换到低延迟模式")
    })
    return () => {
      window.clearTimeout(retry)
      hls.destroy()
    }
  }, [url])

  return (
    <div className="absolute inset-0 z-10 bg-black">
      <video
        autoPlay
        className="h-full w-full object-contain"
        controls
        muted
        playsInline
        ref={videoRef}
      />
      {error && (
        <p className="absolute inset-x-5 top-16 rounded-xl bg-amber-950/90 px-4 py-3 text-sm text-amber-100">
          {error}
        </p>
      )}
    </div>
  )
}

"use client"

import SessionAction from "@/components/SessionAction"
import Link from "next/link"
import { useEffect, useState } from "react"

type Room = {
  room_id: number
  anchor_id: number
  anchor_nickname?: string
  title: string
  viewer_count: number
  like_count: number
}

type Video = {
  video_id: number
  title: string
  author_id: number
  author_nickname?: string
  cover_url?: string
  media_url?: string
  like_count: number
  comment_count: number
}

type ListResponse<T> = { items?: T[] }

export default function DiscoveryHome() {
  const [rooms, setRooms] = useState<Room[]>([])
  const [videos, setVideos] = useState<Video[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    Promise.all([
      getItems<Room>("/api/platform/v1/rooms?status=LIVING&limit=6"),
      getItems<Video>("/api/platform/v1/feed-items?scene=timeline&limit=6"),
    ]).then(([nextRooms, nextVideos]) => {
      if (!active) return
      setRooms(nextRooms)
      setVideos(nextVideos)
      setLoading(false)
    })
    return () => {
      active = false
    }
  }, [])

  return (
    <main className="min-h-svh bg-zinc-950 text-white">
      <header className="sticky top-0 z-20 border-b border-white/10 bg-zinc-950/80 px-6 py-4 backdrop-blur sm:px-10">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-6">
          <a className="text-lg font-black tracking-tight" href="#top">
            LumiFlow
          </a>
          <nav
            className="flex items-center gap-5 text-sm font-medium text-zinc-400"
            aria-label="主导航"
          >
            <a className="transition hover:text-white" href="#live">
              直播
            </a>
            <a className="transition hover:text-white" href="#videos">
              短视频
            </a>
          </nav>
        </div>
      </header>

      <section
        className="relative mx-auto min-h-[min(680px,calc(100svh-4rem))] max-w-6xl overflow-hidden border-x border-white/10"
        id="top"
      >
        <div
          aria-hidden="true"
          className="absolute inset-0 bg-cover bg-center"
          style={{ backgroundImage: "url('/images/lumiflow-hero.jpg')" }}
        />
        <div className="absolute inset-0 bg-gradient-to-r from-zinc-950 via-zinc-950/55 to-zinc-950/10" />
        <div className="absolute inset-0 bg-gradient-to-t from-zinc-950 via-transparent to-zinc-950/25" />
        <div className="relative z-10 grid min-h-[min(680px,calc(100svh-4rem))] items-end gap-10 p-8 sm:p-12 lg:grid-cols-[minmax(0,1fr)_260px]">
          <div className="max-w-xl pb-2">
            <p className="mb-3 text-sm font-medium tracking-[0.2em] text-violet-200">
              VIDEO · LIVE · COMMUNITY
            </p>
            <h1 className="text-5xl font-bold tracking-tight sm:text-7xl">
              一个账号，进入每个此刻。
            </h1>
            <p className="mt-5 text-base leading-7 text-zinc-200 sm:text-lg">
              在直播互动与 FluxFeed 短视频之间自由切换。
            </p>
            <div className="pointer-events-auto mt-7 flex flex-wrap gap-3">
              <a
                className="rounded-xl bg-white px-5 py-3 text-sm font-semibold text-zinc-950 transition hover:bg-violet-100"
                href="#live"
              >
                浏览直播大厅
              </a>
              <a
                className="rounded-xl border border-white/20 bg-black/20 px-5 py-3 text-sm font-semibold transition hover:bg-white/10"
                href="#videos"
              >
                发现短视频
              </a>
            </div>
            <SessionAction />
          </div>
          <div className="grid gap-3 self-end pb-2" aria-label="LumiFlow 服务入口">
            <FeatureCard title="实时互动" detail="弹幕、点赞与房间心跳" tag="LIVE" />
            <FeatureCard title="内容发现" detail="接入 FluxFeed 内容流" tag="FEED" />
            <FeatureCard title="一个账号" detail="统一身份进入直播和视频" tag="ACCOUNT" />
          </div>
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-6 py-20 sm:px-10" id="live">
        <SectionHeading
          eyebrow="LUMI LIVE"
          title="正在直播"
          detail="实时弹幕、点赞与礼物互动都从这里开始。"
        />
        <div className="mt-7 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {loading ? (
            <Cards count={6} />
          ) : rooms.length ? (
            rooms.map((room, index) => (
              <RoomCard key={room.room_id} room={room} index={index} />
            ))
          ) : (
            <Empty text="现在还没有正在直播的房间。" />
          )}
        </div>
      </section>

      <section className="border-y border-white/10 bg-zinc-900/40" id="videos">
        <div className="mx-auto max-w-6xl px-6 py-20 sm:px-10">
          <SectionHeading
            eyebrow="LUMI × FLUXFEED"
            title="精彩短视频"
            detail="FluxFeed 的内容流已经接入 LumiFlow。"
          />
          <div className="mt-7 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {loading ? (
              <Cards count={6} />
            ) : videos.length ? (
              videos.map((video) => (
                <VideoCard key={video.video_id} video={video} />
              ))
            ) : (
              <Empty text="暂无短视频，稍后再来看看。" />
            )}
          </div>
        </div>
      </section>
    </main>
  )
}

function FeatureCard({ title, detail, tag }: { title: string; detail: string; tag: string }) {
  return (
    <div className="rounded-2xl border border-white/15 bg-zinc-950/45 p-4 backdrop-blur-md">
      <p className="text-[10px] font-bold tracking-[0.2em] text-cyan-200">{tag}</p>
      <h2 className="mt-1 font-bold">{title}</h2>
      <p className="mt-1 text-xs leading-5 text-zinc-300">{detail}</p>
    </div>
  )
}

function SectionHeading({
  eyebrow,
  title,
  detail,
}: {
  eyebrow: string
  title: string
  detail: string
}) {
  return (
    <div>
      <p className="text-xs font-bold tracking-[0.2em] text-violet-300">
        {eyebrow}
      </p>
      <h2 className="mt-2 text-3xl font-bold tracking-tight">{title}</h2>
      <p className="mt-2 text-sm text-zinc-400">{detail}</p>
    </div>
  )
}

function RoomCard({ room, index }: { room: Room; index: number }) {
  const gradients = [
    "from-violet-600/70 via-zinc-900 to-cyan-700/70",
    "from-fuchsia-600/70 via-zinc-900 to-violet-800/70",
    "from-cyan-600/70 via-zinc-900 to-blue-800/70",
  ]
  return (
    <Link
      className="overflow-hidden rounded-2xl border border-white/10 bg-zinc-900 transition hover:-translate-y-1 hover:border-violet-300/50"
      href={`/rooms/${room.room_id}`}
    >
      <div
        className={`relative aspect-video bg-gradient-to-br ${gradients[index % gradients.length]}`}
      >
        <span className="absolute top-3 left-3 rounded-full bg-red-500 px-2 py-1 text-[11px] font-bold tracking-wide">
          LIVE
        </span>
        <span className="absolute right-3 bottom-3 rounded-full bg-black/45 px-2 py-1 text-xs">
          ◉ {metric(room.viewer_count)} · ♥ {metric(room.like_count)}
        </span>
      </div>
      <div className="p-4">
        <h3 className="truncate font-bold">{room.title}</h3>
        <p className="mt-2 text-sm text-zinc-400">
          {room.anchor_nickname || `主播 #${room.anchor_id}`}
        </p>
      </div>
    </Link>
  )
}

function VideoCard({ video }: { video: Video }) {
  const cover = assetURL(video.cover_url || video.media_url)
  return (
    <article className="overflow-hidden rounded-2xl border border-white/10 bg-zinc-950">
      <div
        className="relative aspect-video bg-gradient-to-br from-violet-950 via-zinc-900 to-cyan-950"
        style={
          cover
            ? {
                backgroundImage: `linear-gradient(rgb(0 0 0 / 10%), rgb(0 0 0 / 50%)), url("${cover}")`,
                backgroundPosition: "center",
                backgroundSize: "cover",
              }
            : undefined
        }
      >
        <span className="absolute top-1/2 left-1/2 grid size-11 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-white/90 pl-0.5 text-zinc-950">
          ▶
        </span>
      </div>
      <div className="p-4">
        <h3 className="truncate font-bold">{video.title || "FluxFeed 视频"}</h3>
        <p className="mt-2 text-sm text-zinc-400">
          {video.author_nickname || `创作者 #${video.author_id}`} · ♥{" "}
          {metric(video.like_count)} · {metric(video.comment_count)} 评论
        </p>
      </div>
    </article>
  )
}

function Cards({ count }: { count: number }) {
  return (
    <>
      {Array.from({ length: count }, (_, index) => (
        <div
          className="h-56 animate-pulse rounded-2xl bg-white/5"
          key={index}
        />
      ))}
    </>
  )
}
function Empty({ text }: { text: string }) {
  return (
    <p className="col-span-full rounded-2xl border border-dashed border-white/15 p-10 text-center text-sm text-zinc-400">
      {text}
    </p>
  )
}

async function getItems<T>(url: string) {
  try {
    const response = await fetch(url, { cache: "no-store" })
    if (!response.ok) return []
    const data = (await response.json()) as ListResponse<T>
    return Array.isArray(data.items) ? data.items : []
  } catch {
    return []
  }
}

function assetURL(value?: string) {
  if (!value) return ""
  try {
    const url = new URL(value, window.location.origin)
    if (
      url.origin === window.location.origin &&
      url.pathname.startsWith("/uploads/")
    )
      return `/api/platform/v1/content-assets${url.pathname.slice("/uploads".length)}${url.search}`
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : ""
  } catch {
    return ""
  }
}

function metric(value: number) {
  return new Intl.NumberFormat("zh-CN", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(Number(value) || 0)
}

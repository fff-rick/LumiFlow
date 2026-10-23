"use client"

import Link from "next/link"
import {
  Heart,
  MessageCircle,
  Play,
  Radio,
  Sparkles,
  Upload,
  UserRound,
  X,
} from "lucide-react"
import { useEffect, useState } from "react"
import AppNav from "@/components/AppNav"
import "./FluxFeedHome.css"

type FeedScene = "timeline" | "recommend" | "hot"

type FeedItem = {
  video_id: number
  author_id: number
  author_nickname?: string
  author_avatar_url?: string
  title?: string
  description?: string
  media_url?: string
  cover_url?: string
  like_count: number
  comment_count: number
  liked?: boolean
}

type Room = {
  room_id: number
  anchor_id: number
  anchor_nickname?: string
  title: string
  viewer_count: number
  like_count: number
}

const scenes: Array<{ key: FeedScene; label: string; detail: string }> = [
  { key: "timeline", label: "最新", detail: "创作者刚刚分享的内容" },
  { key: "recommend", label: "发现", detail: "为你推荐的精彩瞬间" },
  { key: "hot", label: "热门", detail: "正在被讨论的作品" },
]

export default function FluxFeedHome() {
  const [scene, setScene] = useState<FeedScene>("timeline")
  const [videos, setVideos] = useState<FeedItem[]>([])
  const [rooms, setRooms] = useState<Room[]>([])
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<FeedItem | null>(null)
  const [notice, setNotice] = useState("")

  useEffect(() => {
    let active = true
    getItems<FeedItem>(`/api/platform/v1/feed-items?scene=${scene}&limit=18`)
      .then((items) => active && setVideos(items))
      .catch(() => active && setVideos([]))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [scene])

  useEffect(() => {
    let active = true
    getItems<Room>("/api/platform/v1/rooms?status=LIVING&limit=4")
      .then((items) => active && setRooms(items))
      .catch(() => active && setRooms([]))
    return () => {
      active = false
    }
  }, [])

  function selectScene(nextScene: FeedScene) {
    if (nextScene === scene) return
    setLoading(true)
    setScene(nextScene)
  }

  async function toggleLike(video: FeedItem) {
    const nextLiked = !video.liked
    const response = await fetch(
      `/api/platform/v1/videos/${video.video_id}/like`,
      {
        method: nextLiked ? "PUT" : "DELETE",
        headers: {
          "idempotency-key": `web-like-${video.video_id}-${Date.now()}`,
        },
      }
    )
    if (response.status === 401) {
      setNotice("登录后才能点赞视频。")
      return
    }
    if (!response.ok) {
      setNotice("点赞操作暂时失败，请稍后重试。")
      return
    }
    const data = (await response.json()) as {
      active?: boolean
      like_count?: number
    }
    setVideos((items) =>
      items.map((item) =>
        item.video_id === video.video_id
          ? {
              ...item,
              liked: data.active ?? nextLiked,
              like_count: data.like_count ?? item.like_count,
            }
          : item
      )
    )
    setSelected((item) =>
      item?.video_id === video.video_id
        ? {
            ...item,
            liked: data.active ?? nextLiked,
            like_count: data.like_count ?? item.like_count,
          }
        : item
    )
  }

  const activeScene = scenes.find((item) => item.key === scene) ?? scenes[0]

  return (
    <main className="flux-home">
      <AppNav />

      <div className="flux-layout">
        <aside className="flux-sidebar" aria-label="内容导航">
          <p className="flux-sidebar-title">FluxFeed</p>
          {scenes.map((item) => (
            <button
              className={scene === item.key ? "active" : ""}
              key={item.key}
              onClick={() => selectScene(item.key)}
              type="button"
            >
              <span>
                {item.key === "timeline"
                  ? "◉"
                  : item.key === "recommend"
                    ? "✦"
                    : "↗"}
              </span>
              {item.label}
            </button>
          ))}
          <div className="flux-sidebar-separator" />
          <Link href="/live">
            <Radio size={17} /> 直播大厅
          </Link>
          <Link href="/publish">
            <Upload size={17} /> 发布视频
          </Link>
          <Link href="/me">
            <UserRound size={17} /> 我的作品
          </Link>
        </aside>

        <section className="flux-feed-panel">
          <div className="flux-feed-heading">
            <div>
              <p>FLUXFEED · {scene.toUpperCase()}</p>
              <h1>{activeScene.label}</h1>
              <span>{activeScene.detail}</span>
            </div>
            <div className="flux-live-indicator">
              <i /> 内容流已接入
            </div>
          </div>
          {notice && (
            <p className="flux-notice">
              {notice} <Link href="/login">去登录</Link>
            </p>
          )}

          <div className="flux-video-grid">
            {loading ? (
              <FeedSkeletons />
            ) : videos.length ? (
              videos.map((video, index) => (
                <VideoCard
                  key={video.video_id}
                  index={index}
                  onLike={toggleLike}
                  onOpen={setSelected}
                  video={video}
                />
              ))
            ) : (
              <EmptyFeed />
            )}
          </div>
        </section>

        <aside className="flux-live-rail" id="live">
          <div className="flux-live-heading">
            <Radio size={17} /> 正在直播
          </div>
          {rooms.length ? (
            rooms.map((room, index) => (
              <RoomCard index={index} key={room.room_id} room={room} />
            ))
          ) : (
            <div className="flux-live-empty">
              <Sparkles size={19} />
              <strong>直播间即将开播</strong>
              <span>创建房间后，这里会实时出现。</span>
            </div>
          )}
        </aside>
      </div>
      {selected && (
        <VideoPreview
          onClose={() => setSelected(null)}
          onLike={toggleLike}
          video={selected}
        />
      )}
    </main>
  )
}

function VideoCard({
  video,
  index,
  onOpen,
  onLike,
}: {
  video: FeedItem
  index: number
  onOpen: (item: FeedItem) => void
  onLike: (item: FeedItem) => void
}) {
  const cover = assetURL(video.cover_url || video.media_url)
  return (
    <article className={`flux-video-card card-${index % 5}`}>
      <button
        aria-label={`播放 ${video.title || "视频"}`}
        className="flux-cover"
        onClick={() => onOpen(video)}
        style={cover ? { backgroundImage: `url("${cover}")` } : undefined}
        type="button"
      >
        <span className="flux-cover-shade" />
        <span className="flux-play">
          <Play fill="currentColor" size={18} />
        </span>
        <span className="flux-video-stat">{video.comment_count || 0} 评论</span>
      </button>
      <div className="flux-video-copy">
        <button onClick={() => onOpen(video)} type="button">
          <strong>{video.title || "FluxFeed Moment"}</strong>
        </button>
        <p>{video.author_nickname || `创作者 #${video.author_id}`}</p>
        <button
          className={video.liked ? "flux-like liked" : "flux-like"}
          onClick={() => onLike(video)}
          type="button"
        >
          <Heart fill={video.liked ? "currentColor" : "none"} size={15} />{" "}
          {formatMetric(video.like_count)}
        </button>
      </div>
    </article>
  )
}

function RoomCard({ room, index }: { room: Room; index: number }) {
  return (
    <Link
      className={`flux-room room-${index % 3}`}
      href={`/rooms/${room.room_id}`}
    >
      <span>LIVE</span>
      <strong>{room.title}</strong>
      <p>{room.anchor_nickname || `主播 #${room.anchor_id}`}</p>
      <small>
        ◉ {formatMetric(room.viewer_count)} · ♥ {formatMetric(room.like_count)}
      </small>
    </Link>
  )
}

function VideoPreview({
  video,
  onClose,
  onLike,
}: {
  video: FeedItem
  onClose: () => void
  onLike: (item: FeedItem) => void
}) {
  const media = assetURL(video.media_url || video.cover_url)
  const videoSource = Boolean(media && /\.(mp4|webm|ogg)(?:$|\?)/i.test(media))
  return (
    <div className="flux-preview" onClick={onClose} role="presentation">
      <section
        className="flux-preview-card"
        onClick={(event) => event.stopPropagation()}
      >
        <button
          aria-label="关闭预览"
          className="flux-close"
          onClick={onClose}
          type="button"
        >
          <X size={19} />
        </button>
        {videoSource ? (
          <video
            autoPlay
            controls
            playsInline
            poster={assetURL(video.cover_url)}
            src={media}
          />
        ) : (
          <div
            className="flux-preview-image"
            style={media ? { backgroundImage: `url("${media}")` } : undefined}
          />
        )}
        <div className="flux-preview-copy">
          <p>{video.author_nickname || `创作者 #${video.author_id}`}</p>
          <h2>{video.title || "FluxFeed Moment"}</h2>
          <span>{video.description || "来自 FluxFeed 的内容分享。"}</span>
          <button
            className={video.liked ? "flux-like liked" : "flux-like"}
            onClick={() => onLike(video)}
            type="button"
          >
            <Heart fill={video.liked ? "currentColor" : "none"} size={16} />{" "}
            {formatMetric(video.like_count)}
          </button>
          <Link
            className="flux-comments-link"
            href={`/videos/${video.video_id}`}
          >
            <MessageCircle size={15} /> 查看 {video.comment_count || 0} 条评论
          </Link>
        </div>
      </section>
    </div>
  )
}

function FeedSkeletons() {
  return (
    <>
      {Array.from({ length: 9 }, (_, index) => (
        <div className={`flux-skeleton card-${index % 5}`} key={index} />
      ))}
    </>
  )
}

function EmptyFeed() {
  return (
    <div className="flux-feed-empty">
      <Sparkles size={26} />
      <h2>内容流已经就绪</h2>
      <p>当前没有已发布视频。登录后可通过 FluxFeed 发布第一条内容。</p>
      <Link href="/login">登录并开始使用</Link>
    </div>
  )
}

async function getItems<T>(path: string) {
  const response = await fetch(path, { cache: "no-store" })
  if (!response.ok) throw new Error("request failed")
  const data = (await response.json()) as { items?: T[] }
  return data.items ?? []
}

function assetURL(value?: string) {
  if (!value || value.startsWith("http://") || value.startsWith("https://"))
    return value
  if (value.startsWith("/uploads/")) {
    return `/api/platform/v1/content-assets/${value.slice("/uploads/".length)}`
  }
  if (value.startsWith("/")) return value
  return `/api/platform/v1/content-assets/${value.replace(/^\/+/, "")}`
}

function formatMetric(value: number) {
  if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`
  return String(value || 0)
}

"use client"

import Link from "next/link"
import Image from "next/image"
import { useEffect, useState } from "react"
import { Film, Heart, UserPlus, Users } from "lucide-react"
import AppNav from "@/components/AppNav"
import { useSession } from "@/components/SessionProvider"
import {
  assetURL,
  metric,
  platformRequest,
  RelationUser,
  statusLabel,
  Video,
  videoID,
} from "@/lib/platform"
import "./Portal.css"

type Tab = "works" | "following" | "followers"
type RelationResponse = {
  items?: RelationUser[]
  has_more?: boolean
  next_cursor?: string
}

export default function ProfileHub({
  initialTab = "works",
}: {
  initialTab?: Tab
}) {
  const { user, loading, refreshSession } = useSession()
  const [tab, setTab] = useState<Tab>(initialTab)
  const [videos, setVideos] = useState<Video[]>([])
  const [relations, setRelations] = useState<RelationUser[]>([])
  const [following, setFollowing] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")

  useEffect(() => {
    if (!user) return
    let active = true
    void Promise.resolve().then(async () => {
      if (!active) return
      setBusy(true)
      setMessage("")
      try {
        if (tab === "works") {
          const data = await platformRequest<{ items?: Video[] }>(
            "/users/me/videos?limit=40"
          )
          if (active) setVideos(data.items ?? [])
        } else {
          const data = await platformRequest<RelationResponse>(
            `/users/me/${tab}?limit=50`
          )
          if (!active) return
          const items = data.items ?? []
          setRelations(items)
          if (tab === "following")
            setFollowing(new Set(items.map((item) => item.user_id)))
        }
      } catch (error) {
        if (active)
          setMessage(error instanceof Error ? error.message : "内容加载失败")
      } finally {
        if (active) setBusy(false)
      }
    })
    return () => {
      active = false
    }
  }, [tab, user])

  async function toggleFollow(targetUserID: number) {
    const active = following.has(targetUserID)
    setMessage("")
    try {
      const result = await platformRequest<{ following: boolean }>(
        `/users/me/following/${targetUserID}`,
        {
          method: active ? "DELETE" : "PUT",
          headers: {
            "idempotency-key": `lumi-follow-${targetUserID}-${Date.now()}`,
          },
        }
      )
      setFollowing((current) => {
        const next = new Set(current)
        if (result.following) next.add(targetUserID)
        else next.delete(targetUserID)
        return next
      })
      if (tab === "following" && !result.following)
        setRelations((items) =>
          items.filter((item) => item.user_id !== targetUserID)
        )
      await refreshSession()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "关注操作失败")
    }
  }

  return (
    <main className="portal-page">
      <AppNav />
      <div className="portal-shell">
        {!loading && !user ? (
          <section className="portal-empty">
            <Users size={34} />
            <h1>登录后查看创作者中心</h1>
            <p>作品、关注与粉丝会集中显示在这里。</p>
            <Link className="portal-primary" href="/login">
              登录 / 注册
            </Link>
          </section>
        ) : (
          user && (
            <>
              <section className="profile-hero">
                <Avatar name={user.nickname} src={user.avatar_url} />
                <div>
                  <p>LUMIFLOW · CREATOR</p>
                  <h1>{user.nickname || "LumiFlow 创作者"}</h1>
                  <span>{user.bio || "记录值得停留的每一帧。"}</span>
                </div>
                <div className="profile-counts">
                  <button onClick={() => setTab("following")}>
                    <strong>{metric(user.following_count)}</strong>
                    <span>关注</span>
                  </button>
                  <button onClick={() => setTab("followers")}>
                    <strong>{metric(user.follower_count)}</strong>
                    <span>粉丝</span>
                  </button>
                  <button onClick={() => setTab("works")}>
                    <strong>{metric(user.work_count ?? videos.length)}</strong>
                    <span>作品</span>
                  </button>
                </div>
              </section>
              <section className="profile-content">
                <header>
                  <div className="profile-tabs">
                    <button
                      className={tab === "works" ? "active" : ""}
                      onClick={() => setTab("works")}
                    >
                      <Film size={16} />
                      作品
                    </button>
                    <button
                      className={tab === "following" ? "active" : ""}
                      onClick={() => setTab("following")}
                    >
                      <Heart size={16} />
                      关注
                    </button>
                    <button
                      className={tab === "followers" ? "active" : ""}
                      onClick={() => setTab("followers")}
                    >
                      <Users size={16} />
                      粉丝
                    </button>
                  </div>
                  <Link className="portal-primary compact" href="/publish">
                    发布新作品
                  </Link>
                </header>
                {message && <p className="portal-message error">{message}</p>}
                {busy ? (
                  <CardSkeletons />
                ) : tab === "works" ? (
                  <WorkGrid videos={videos} />
                ) : (
                  <RelationGrid
                    following={following}
                    items={relations}
                    onFollow={toggleFollow}
                  />
                )}
              </section>
            </>
          )
        )}
      </div>
    </main>
  )
}

export function WorkGrid({ videos }: { videos: Video[] }) {
  if (!videos.length)
    return (
      <div className="portal-empty inline">
        <Film size={30} />
        <h2>还没有作品</h2>
        <p>发布第一条视频，开始你的 LumiFlow 创作。</p>
      </div>
    )
  return (
    <div className="work-grid">
      {videos.map((video) => (
        <Link href={`/videos/${videoID(video)}`} key={videoID(video)}>
          <div
            className="work-cover"
            style={
              assetURL(video.cover_url)
                ? { backgroundImage: `url("${assetURL(video.cover_url)}")` }
                : undefined
            }
          >
            <span>{statusLabel(video)}</span>
          </div>
          <strong>{video.title || "未命名作品"}</strong>
          <p>
            {metric(video.like_count)} 点赞 · {metric(video.comment_count)} 评论
          </p>
          {video.review_reason && <small>{video.review_reason}</small>}
        </Link>
      ))}
    </div>
  )
}

function RelationGrid({
  items,
  following,
  onFollow,
}: {
  items: RelationUser[]
  following: Set<number>
  onFollow: (id: number) => void
}) {
  if (!items.length)
    return (
      <div className="portal-empty inline">
        <UserPlus size={30} />
        <h2>这里还没有人</h2>
        <p>从视频详情进入创作者主页，可以开始关注。</p>
      </div>
    )
  return (
    <div className="relation-grid">
      {items.map((item) => (
        <article key={item.user_id}>
          <Link href={`/users/${item.user_id}`}>
            <Avatar name={item.nickname} src={item.avatar_url} />
            <span>
              <strong>{item.nickname || `用户 #${item.user_id}`}</strong>
              <small>{item.bio || "这个用户还没有填写简介。"}</small>
            </span>
          </Link>
          <button
            className={following.has(item.user_id) ? "active" : ""}
            onClick={() => onFollow(item.user_id)}
          >
            {following.has(item.user_id) ? "已关注" : "关注"}
          </button>
        </article>
      ))}
    </div>
  )
}

export function Avatar({ name, src }: { name?: string; src?: string }) {
  return assetURL(src) ? (
    <Image
      className="portal-avatar"
      alt=""
      height={64}
      src={assetURL(src)!}
      unoptimized
      width={64}
    />
  ) : (
    <i className="portal-avatar">{(name || "L").slice(0, 1)}</i>
  )
}

function CardSkeletons() {
  return (
    <div className="work-grid">
      {Array.from({ length: 6 }, (_, index) => (
        <div className="portal-skeleton" key={index} />
      ))}
    </div>
  )
}

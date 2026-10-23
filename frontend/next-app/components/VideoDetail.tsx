"use client"

import Link from "next/link"
import { FormEvent, useCallback, useEffect, useState } from "react"
import { Heart, MessageCircle, Send, Trash2, UserPlus } from "lucide-react"
import { useRouter } from "next/navigation"
import AppNav from "@/components/AppNav"
import { Avatar } from "@/components/ProfileHub"
import { useSession } from "@/components/SessionProvider"
import {
  assetURL,
  metric,
  platformRequest,
  RelationUser,
  SessionUser,
  Video,
} from "@/lib/platform"
import "./Portal.css"

type Comment = {
  id: number
  user_id: number
  user_nickname?: string
  user_avatar_url?: string
  content: string
  created_at: string
  status_name?: string
}

export default function VideoDetail({ videoId }: { videoId: string }) {
  const router = useRouter()
  const { user } = useSession()
  const [video, setVideo] = useState<Video | null>(null)
  const [author, setAuthor] = useState<SessionUser | null>(null)
  const [comments, setComments] = useState<Comment[]>([])
  const [following, setFollowing] = useState(false)
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    setMessage("")
    try {
      const nextVideo = await platformRequest<Video>(`/videos/${videoId}`)
      setVideo(nextVideo)
      const [commentResult, authorResult] = await Promise.allSettled([
        platformRequest<{ items?: Comment[] }>(
          `/videos/${videoId}/comments?limit=50`
        ),
        platformRequest<SessionUser>(`/users/${nextVideo.author_id}`),
      ])
      if (commentResult.status === "fulfilled")
        setComments(commentResult.value.items ?? [])
      else
        setMessage(
          commentResult.reason instanceof Error
            ? commentResult.reason.message
            : "评论暂时不可用"
        )
      if (authorResult.status === "fulfilled") setAuthor(authorResult.value)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "视频加载失败")
    } finally {
      setLoading(false)
    }
  }, [videoId])

  useEffect(() => {
    void Promise.resolve().then(load)
  }, [load])

  useEffect(() => {
    if (!user || !video || user.id === video.author_id) return
    platformRequest<{ items?: RelationUser[] }>("/users/me/following?limit=100")
      .then((data) =>
        setFollowing(
          Boolean(data.items?.some((item) => item.user_id === video.author_id))
        )
      )
      .catch(() => {})
  }, [user, video])

  function requireLogin() {
    if (user) return true
    router.push("/login")
    return false
  }

  async function toggleLike() {
    if (!video || !requireLogin()) return
    setPending(true)
    try {
      const active = !video.liked
      const result = await platformRequest<{
        active: boolean
        like_count: number
      }>(`/videos/${videoId}/like`, {
        method: active ? "PUT" : "DELETE",
        headers: { "idempotency-key": `lumi-like-${videoId}-${Date.now()}` },
      })
      setVideo({
        ...video,
        liked: result.active,
        like_count: result.like_count,
      })
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "点赞失败")
    } finally {
      setPending(false)
    }
  }

  async function toggleFollow() {
    if (!video || !requireLogin()) return
    setPending(true)
    try {
      const result = await platformRequest<{ following: boolean }>(
        `/users/me/following/${video.author_id}`,
        {
          method: following ? "DELETE" : "PUT",
          headers: {
            "idempotency-key": `lumi-follow-${video.author_id}-${Date.now()}`,
          },
        }
      )
      setFollowing(result.following)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "关注操作失败")
    } finally {
      setPending(false)
    }
  }

  async function addComment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!requireLogin()) return
    const form = event.currentTarget
    const content = String(new FormData(form).get("content") ?? "").trim()
    if (!content) return
    setPending(true)
    try {
      const comment = await platformRequest<Comment>(
        `/videos/${videoId}/comments`,
        {
          method: "POST",
          headers: {
            "idempotency-key": `lumi-comment-${videoId}-${crypto.randomUUID()}`,
          },
          body: JSON.stringify({ content }),
        }
      )
      setComments((items) => [comment, ...items])
      setVideo((value) =>
        value ? { ...value, comment_count: value.comment_count + 1 } : value
      )
      form.reset()
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "评论发布失败")
    } finally {
      setPending(false)
    }
  }

  async function removeComment(comment: Comment) {
    if (!window.confirm("删除这条评论？")) return
    try {
      await platformRequest(`/comments/${comment.id}`, { method: "DELETE" })
      setComments((items) => items.filter((item) => item.id !== comment.id))
      setVideo((value) =>
        value
          ? { ...value, comment_count: Math.max(0, value.comment_count - 1) }
          : value
      )
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "评论删除失败")
    }
  }

  return (
    <main className="portal-page video-detail-page">
      <AppNav />
      <div className="portal-shell">
        {loading ? (
          <section className="video-loading">正在载入视频…</section>
        ) : !video ? (
          <section className="portal-empty">
            <MessageCircle size={34} />
            <h1>暂时无法打开视频</h1>
            <p>{message || "视频可能仍在审核或已下架。"}</p>
            <Link className="portal-primary" href="/">
              返回发现
            </Link>
          </section>
        ) : (
          <div className="video-detail-grid">
            <section className="video-player-card">
              <video
                controls
                playsInline
                poster={assetURL(video.cover_url)}
                src={assetURL(video.media_url)}
              />
              <div className="video-meta">
                <div>
                  <p>VIDEO #{videoId}</p>
                  <h1>{video.title || "LumiFlow Moment"}</h1>
                  <span>
                    {video.description || "来自 LumiFlow 的视频分享。"}
                  </span>
                </div>
                <button
                  className={video.liked ? "video-like active" : "video-like"}
                  disabled={pending}
                  onClick={toggleLike}
                >
                  <Heart fill={video.liked ? "currentColor" : "none"} />
                  {metric(video.like_count)}
                </button>
              </div>
              <div className="video-author">
                <Link href={`/users/${video.author_id}`}>
                  <Avatar
                    name={author?.nickname || video.author_nickname}
                    src={author?.avatar_url}
                  />
                  <span>
                    <small>创作者</small>
                    <strong>
                      {author?.nickname ||
                        video.author_nickname ||
                        `创作者 #${video.author_id}`}
                    </strong>
                  </span>
                </Link>
                {user?.id !== video.author_id && (
                  <button
                    className={following ? "active" : ""}
                    disabled={pending}
                    onClick={toggleFollow}
                  >
                    <UserPlus size={16} />
                    {following ? "已关注" : "关注"}
                  </button>
                )}
              </div>
            </section>
            <aside className="comments-card">
              <header>
                <span>
                  <MessageCircle size={18} />
                  评论
                </span>
                <small>{metric(video.comment_count)} 条</small>
              </header>
              <form onSubmit={addComment}>
                <input
                  maxLength={500}
                  name="content"
                  placeholder={user ? "说点友善的话…" : "登录后参与评论"}
                />
                <button disabled={pending} aria-label="发布评论">
                  <Send size={17} />
                </button>
              </form>
              {message && <p className="portal-message error">{message}</p>}
              <div className="comment-list">
                {comments.length ? (
                  comments.map((comment) => (
                    <article key={comment.id}>
                      <Avatar
                        name={comment.user_nickname}
                        src={comment.user_avatar_url}
                      />
                      <div>
                        <strong>
                          {comment.user_nickname || `用户 #${comment.user_id}`}
                        </strong>
                        <p>{comment.content}</p>
                        <small>
                          {comment.status_name === "pending_review"
                            ? "审核中"
                            : formatTime(comment.created_at)}
                        </small>
                      </div>
                      {user?.id === comment.user_id && (
                        <button
                          aria-label="删除评论"
                          onClick={() => void removeComment(comment)}
                        >
                          <Trash2 size={14} />
                        </button>
                      )}
                    </article>
                  ))
                ) : (
                  <div className="comments-empty">
                    <MessageCircle size={28} />
                    <span>还没有评论，来留下第一条吧。</span>
                  </div>
                )}
              </div>
            </aside>
          </div>
        )}
      </div>
    </main>
  )
}

function formatTime(value: string) {
  if (!value) return "刚刚"
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium" }).format(
    new Date(value)
  )
}

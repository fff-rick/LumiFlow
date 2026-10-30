"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { UserPlus } from "lucide-react"
import { useRouter } from "next/navigation"
import AppNav from "@/components/AppNav"
import { Avatar, WorkGrid } from "@/components/ProfileHub"
import { useSession } from "@/components/SessionProvider"
import {
  metric,
  platformRequest,
  RelationUser,
  SessionUser,
  Video,
} from "@/lib/platform"
import "./Portal.css"

export default function CreatorProfile({ userId }: { userId: string }) {
  const router = useRouter()
  const { user } = useSession()
  const [profile, setProfile] = useState<SessionUser | null>(null)
  const [videos, setVideos] = useState<Video[]>([])
  const [following, setFollowing] = useState(false)
  const [message, setMessage] = useState("")

  useEffect(() => {
    let active = true
    Promise.all([
      platformRequest<SessionUser>(`/users/${userId}`),
      platformRequest<{ items?: Video[] }>(`/users/${userId}/videos?limit=40`),
    ])
      .then(([nextProfile, result]) => {
        if (!active) return
        setProfile(nextProfile)
        setVideos(result.items ?? [])
      })
      .catch(
        (error) =>
          active &&
          setMessage(
            error instanceof Error ? error.message : "创作者主页加载失败"
          )
      )
    return () => {
      active = false
    }
  }, [userId])

  useEffect(() => {
    if (!user || user.id === Number(userId)) return
    platformRequest<{ items?: RelationUser[] }>("/users/me/following?limit=100")
      .then((data) =>
        setFollowing(
          Boolean(data.items?.some((item) => item.user_id === Number(userId)))
        )
      )
      .catch(() => {})
  }, [user, userId])

  async function toggleFollow() {
    if (!user) return router.push("/login")
    try {
      const result = await platformRequest<{ following: boolean }>(
        `/users/me/following/${userId}`,
        {
          method: following ? "DELETE" : "PUT",
          headers: { "idempotency-key": `lumi-follow-${userId}-${Date.now()}` },
        }
      )
      setFollowing(result.following)
      setProfile((value) =>
        value
          ? {
              ...value,
              follower_count: Math.max(
                0,
                (value.follower_count ?? 0) + (result.following ? 1 : -1)
              ),
            }
          : value
      )
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "关注操作失败")
    }
  }

  return (
    <main className="portal-page">
      <AppNav />
      <div className="portal-shell">
        {!profile ? (
          <section className="portal-empty">
            <UserPlus size={34} />
            <h1>{message || "正在载入创作者主页…"}</h1>
            {message && (
              <Link className="portal-primary" href="/discover">
                返回发现
              </Link>
            )}
          </section>
        ) : (
          <>
            <section className="profile-hero">
              <Avatar name={profile.nickname} src={profile.avatar_url} />
              <div>
                <p>LUMIFLOW · CREATOR</p>
                <h1>{profile.nickname || `创作者 #${userId}`}</h1>
                <span>{profile.bio || "这个创作者还没有填写简介。"}</span>
              </div>
              <div className="profile-counts">
                <span>
                  <strong>{metric(profile.following_count)}</strong>
                  <small>关注</small>
                </span>
                <span>
                  <strong>{metric(profile.follower_count)}</strong>
                  <small>粉丝</small>
                </span>
                <span>
                  <strong>{metric(profile.work_count ?? videos.length)}</strong>
                  <small>作品</small>
                </span>
              </div>
              {user?.id !== Number(userId) && (
                <button
                  className={
                    following ? "profile-follow active" : "profile-follow"
                  }
                  onClick={toggleFollow}
                >
                  <UserPlus size={16} />
                  {following ? "已关注" : "关注"}
                </button>
              )}
            </section>
            <section className="profile-content">
              <header>
                <h2>全部作品</h2>
              </header>
              {message && <p className="portal-message error">{message}</p>}
              <WorkGrid videos={videos} />
            </section>
          </>
        )}
      </div>
    </main>
  )
}

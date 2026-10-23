"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"

type User = { nickname?: string }

export default function SessionAction() {
  const router = useRouter()
  const [user, setUser] = useState<User | null | undefined>(undefined)

  useEffect(() => {
    let active = true
    fetch("/api/platform/v1/me", { cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) {
          if (response.status === 401 || response.status === 403) {
            void fetch("/api/session", { method: "DELETE" })
          }
          throw new Error("session lookup failed")
        }
        return (await response.json()) as User
      })
      .then((value) => active && setUser(value))
      .catch(() => {
        if (!active) return
        setUser(null)
      })
    return () => {
      active = false
    }
  }, [])

  async function logout() {
    await fetch("/api/session", { method: "DELETE" })
    setUser(null)
    router.refresh()
  }

  if (user === undefined) return null
  if (!user) {
    return (
      <Link
        className="pointer-events-auto mt-7 inline-flex rounded-xl bg-white px-5 py-3 text-sm font-semibold text-zinc-950 transition hover:bg-violet-100"
        href="/login"
      >
        登录或注册
      </Link>
    )
  }

  return (
    <div className="pointer-events-auto mt-7 flex flex-wrap items-center gap-3">
      <span className="rounded-xl border border-white/20 bg-black/20 px-4 py-3 text-sm font-medium">
        欢迎回来，{user.nickname || "朋友"}
      </span>
      <button
        className="rounded-xl bg-white px-5 py-3 text-sm font-semibold text-zinc-950 transition hover:bg-violet-100"
        onClick={logout}
        type="button"
      >
        退出登录
      </button>
    </div>
  )
}

"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { FormEvent, useEffect, useState } from "react"
import { Centrifuge } from "centrifuge"
import AppNav from "@/components/AppNav"
import { useSession } from "@/components/SessionProvider"

type Room = {
  room_id: number
  anchor_id: number
  anchor_nickname?: string
  title: string
  status: string
}
type Stats = { viewer_count: number; like_count: number }
type Message = {
  message_id: string
  nickname?: string
  content?: string
  type?: string
  gift_name?: string
  count?: number
}
type Subscription = { channel: string; token: string }
type JoinResponse = {
  stats?: Stats
  heartbeat_interval_seconds?: number
  subscriptions: Subscription[]
  personal_channel: string
}
type RealtimeState = "offline" | "connecting" | "online"

export default function LiveRoom({ roomId }: { roomId: string }) {
  const router = useRouter()
  const { user } = useSession()
  const [room, setRoom] = useState<Room | null>(null)
  const [stats, setStats] = useState<Stats>({ viewer_count: 0, like_count: 0 })
  const [messages, setMessages] = useState<Message[]>([])
  const [joined, setJoined] = useState(false)
  const [heartbeatSeconds, setHeartbeatSeconds] = useState(20)
  const [realtimeSession, setRealtimeSession] = useState<JoinResponse | null>(
    null
  )
  const [realtimeState, setRealtimeState] = useState<RealtimeState>("offline")
  const [notice, setNotice] = useState("")

  useEffect(() => {
    let active = true
    Promise.all([
      get<Room>(`/api/platform/v1/rooms/${roomId}`),
      get<Stats>(`/api/platform/v1/rooms/${roomId}/stats`),
      get<{ items?: Message[] }>(
        `/api/platform/v1/rooms/${roomId}/messages?limit=30`
      ),
    ])
      .then(([nextRoom, nextStats, history]) => {
        if (!active) return
        if (!nextRoom) {
          setNotice("找不到该直播间，或服务暂不可用。")
          return
        }
        setRoom(nextRoom)
        if (nextStats) setStats(nextStats)
        setMessages(history?.items ?? [])
      })
      .catch(() => active && setNotice("直播间暂不可用，请稍后重试。"))
    return () => {
      active = false
    }
  }, [roomId])

  useEffect(() => {
    if (!joined) return
    const timer = window.setInterval(
      async () => {
        const result = await post<{ viewer_count?: number }>(
          `/api/platform/v1/rooms/${roomId}/heartbeat`
        )
        if (result.ok && result.data.viewer_count !== undefined)
          setStats((value) => ({
            ...value,
            viewer_count: result.data.viewer_count ?? value.viewer_count,
          }))
      },
      Math.max(5, heartbeatSeconds) * 1000
    )
    return () => {
      window.clearInterval(timer)
      void fetch(`/api/platform/v1/rooms/${roomId}/leave`, {
        method: "POST",
        keepalive: true,
      })
    }
  }, [heartbeatSeconds, joined, roomId])

  useEffect(() => {
    if (!realtimeSession) return
    const session = realtimeSession
    let active = true
    let client: Centrifuge | null = null

    async function connect() {
      const token = await realtimeToken()
      if (!active) return
      client = new Centrifuge(realtimeURL(), {
        token,
        getToken: () => realtimeToken(),
      })
      client.on("connected", () => {
        if (!active) return
        setRealtimeState("online")
        setNotice("已进入直播间；实时广播已连接。")
      })
      client.on("connecting", () => active && setRealtimeState("connecting"))
      client.on("disconnected", () => active && setRealtimeState("offline"))
      client.on("error", (context) => console.warn("centrifuge", context))

      const handlePublication = (value: unknown) => {
        if (!active) return
        const event = parseRealtimeEvent(value)
        if (!event) return
        if (event.type === "stats") {
          const patch = parseStats(event.data)
          if (patch)
            setStats((current) => ({
              viewer_count: patch.viewer_count ?? current.viewer_count,
              like_count: patch.like_count ?? current.like_count,
            }))
          return
        }
        if (event.type === "danmaku" || event.type === "gift") {
          const message = parseRealtimeMessage(event.type, event.data)
          if (message)
            setMessages((current) => appendUniqueMessage(current, message))
          return
        }
        if (event.type === "room_muted") {
          setNotice(eventReason(event.data, "你已被禁言。"))
        }
        if (event.type === "room_banned") {
          setNotice(eventReason(event.data, "你已被移出直播间。"))
          setJoined(false)
          setRealtimeSession(null)
        }
      }

      for (const subscription of session.subscriptions) {
        const channel = subscription.channel
        const roomSubscription = client.newSubscription(channel, {
          token: subscription.token,
          getToken: () => subscriptionToken(roomId, channel),
        })
        roomSubscription.on("publication", (context) =>
          handlePublication(context.data)
        )
        roomSubscription.on("error", (context) =>
          console.warn("centrifuge subscription", channel, context)
        )
        roomSubscription.subscribe()
      }
      const personal = client.newSubscription(session.personal_channel)
      personal.on("publication", (context) => handlePublication(context.data))
      personal.subscribe()
      client.connect()
    }

    void connect().catch((error) => {
      if (!active) return
      setRealtimeState("offline")
      setNotice(
        error instanceof Error
          ? error.message
          : "实时广播连接失败，请稍后重试。"
      )
    })
    return () => {
      active = false
      client?.disconnect()
    }
  }, [realtimeSession, roomId])

  async function join() {
    if (!user) {
      router.push("/login")
      return
    }
    const result = await post<JoinResponse>(
      `/api/platform/v1/rooms/${roomId}/join`
    )
    if (!result.ok) return setNotice(result.message)
    setJoined(true)
    if (result.data.stats) setStats(result.data.stats)
    setHeartbeatSeconds(result.data.heartbeat_interval_seconds ?? 20)
    setRealtimeState("connecting")
    setRealtimeSession(result.data)
    setNotice("已进入直播间；正在连接实时广播。")
  }

  async function sendDanmaku(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const content = String(form.get("content") ?? "").trim()
    if (!content) return
    if (!joined) return setNotice("进入直播间后才可以发送弹幕。")
    const result = await post<Message>(
      `/api/platform/v1/rooms/${roomId}/danmaku`,
      { content }
    )
    if (!result.ok) return setNotice(result.message)
    setMessages((value) => appendUniqueMessage(value, result.data))
    event.currentTarget.reset()
  }

  async function like() {
    if (!joined) return setNotice("进入直播间后才可以点赞。")
    const result = await post<{ total?: number }>(
      `/api/platform/v1/rooms/${roomId}/like`,
      { count: 1 }
    )
    if (!result.ok) return setNotice(result.message)
    if (result.data.total !== undefined)
      setStats((value) => ({
        ...value,
        like_count: result.data.total ?? value.like_count,
      }))
  }

  if (!room)
    return (
      <main className="grid min-h-svh place-items-center bg-zinc-950 p-6 text-zinc-300">
        {notice || "正在载入直播间…"}
      </main>
    )

  const living = room.status === "LIVING"
  return (
    <main className="min-h-svh bg-zinc-950 text-white">
      <AppNav />
      <div className="mx-auto max-w-6xl px-6 py-6 sm:px-10">
        <Link
          className="text-sm text-zinc-400 transition hover:text-white"
          href="/"
        >
          ← 返回 LumiFlow
        </Link>
        <div className="mt-7 flex flex-wrap items-start justify-between gap-5">
          <div>
            <p className="text-xs font-bold tracking-[0.2em] text-violet-300">
              LUMI LIVE
            </p>
            <h1 className="mt-2 text-3xl font-bold">{room.title}</h1>
            <p className="mt-2 text-sm text-zinc-400">
              {room.anchor_nickname || `主播 #${room.anchor_id}`} · 房间 #
              {room.room_id}
            </p>
          </div>
          <button
            className="rounded-xl bg-violet-500 px-5 py-3 text-sm font-semibold transition hover:bg-violet-400 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={!living || joined}
            onClick={join}
            type="button"
          >
            {joined ? "已进入直播间" : living ? "进入直播间" : "直播已结束"}
          </button>
        </div>
        {notice && (
          <p className="mt-5 rounded-xl border border-violet-300/20 bg-violet-400/10 px-4 py-3 text-sm text-violet-100">
            {notice}
          </p>
        )}
        <div className="mt-7 grid gap-5 lg:grid-cols-[minmax(0,1fr)_360px]">
          <section className="relative aspect-video overflow-hidden rounded-3xl border border-white/10 bg-[radial-gradient(circle_at_50%_35%,rgba(139,92,246,.45),transparent_32%),linear-gradient(135deg,#15152b,#09090b)]">
            <div className="absolute inset-0 bg-[linear-gradient(rgba(255,255,255,.03)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,.03)_1px,transparent_1px)] bg-[size:30px_30px]" />
            <div className="relative grid h-full place-items-center p-8 text-center">
              <div>
                <span className="rounded-full bg-red-500 px-3 py-1 text-xs font-bold">
                  {living ? "LIVE" : room.status}
                </span>
                <h2 className="mt-5 text-xl font-bold">媒体流接入位</h2>
                <p className="mt-3 max-w-md text-sm leading-6 text-zinc-300">
                  直播互动已经接入；接入 SRS、LiveKit
                  或云直播后会在这里播放媒体流。
                </p>
              </div>
            </div>
            <div className="absolute bottom-0 flex w-full justify-between border-t border-white/10 bg-black/30 px-5 py-3 text-sm text-zinc-300">
              <span>◉ {metric(stats.viewer_count)} 在线</span>
              <span>♥ {metric(stats.like_count)} 点赞</span>
            </div>
          </section>
          <aside className="flex min-h-[440px] flex-col rounded-3xl border border-white/10 bg-zinc-900">
            <div className="flex items-center justify-between border-b border-white/10 px-5 py-4">
              <strong>直播互动</strong>
              <span className="text-xs text-zinc-400">
                {realtimeLabel(joined, realtimeState)}
              </span>
            </div>
            <div className="flex-1 space-y-3 overflow-y-auto p-5">
              {messages.length ? (
                messages.map((message) => (
                  <p
                    className="text-sm leading-6 text-zinc-300"
                    key={message.message_id}
                  >
                    <b className="text-white">{message.nickname || "用户"}</b>
                    {message.type === "gift"
                      ? ` 赠送 ${message.gift_name} × ${message.count}`
                      : `：${message.content}`}
                  </p>
                ))
              ) : (
                <p className="text-sm text-zinc-500">还没有互动消息。</p>
              )}
            </div>
            <form
              className="border-t border-white/10 p-4"
              onSubmit={sendDanmaku}
            >
              <div className="flex gap-2">
                <input
                  className="min-w-0 flex-1 rounded-xl border border-white/10 bg-zinc-950 px-3 py-2 text-sm outline-none focus:border-violet-400"
                  disabled={!joined}
                  maxLength={200}
                  name="content"
                  placeholder={
                    joined ? "发一条友善的弹幕吧" : "进入直播间后发送弹幕"
                  }
                />
                <button
                  className="rounded-xl bg-white px-3 text-sm font-semibold text-zinc-950 disabled:opacity-50"
                  disabled={!joined}
                >
                  发送
                </button>
              </div>
            </form>
          </aside>
        </div>
        <div className="mt-5 flex justify-end">
          <button
            className="rounded-xl border border-white/15 px-5 py-3 text-sm font-semibold transition hover:bg-white/10 disabled:opacity-50"
            disabled={!joined}
            onClick={like}
            type="button"
          >
            ♥ 点赞
          </button>
        </div>
      </div>
    </main>
  )
}

async function get<T>(url: string): Promise<T | null> {
  try {
    const response = await fetch(url, { cache: "no-store" })
    return response.ok ? ((await response.json()) as T) : null
  } catch {
    return null
  }
}
async function post<T>(
  url: string,
  body?: unknown
): Promise<{ ok: true; data: T } | { ok: false; message: string }> {
  try {
    const response = await fetch(url, {
      method: "POST",
      headers: body ? { "content-type": "application/json" } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    })
    const data = await response.json().catch(() => ({}))
    return response.ok
      ? { ok: true, data: data as T }
      : {
          ok: false,
          message: data.message ?? data.error ?? "请求失败，请稍后重试",
        }
  } catch {
    return { ok: false, message: "网络异常，请稍后重试" }
  }
}
function metric(value: number) {
  return new Intl.NumberFormat("zh-CN", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(Number(value) || 0)
}

async function realtimeToken() {
  const result = await post<{ token?: string }>(
    "/api/platform/v1/realtime/token"
  )
  if (!result.ok || !result.data.token)
    throw new Error(result.ok ? "实时令牌无效" : result.message)
  return result.data.token
}

async function subscriptionToken(roomId: string, channel: string) {
  const result = await post<JoinResponse>(
    `/api/platform/v1/rooms/${roomId}/join`
  )
  if (!result.ok) throw new Error(result.message)
  const token = result.data.subscriptions.find(
    (subscription) => subscription.channel === channel
  )?.token
  if (!token) throw new Error("实时订阅令牌无效")
  return token
}

function realtimeURL() {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
  const local = ["localhost", "127.0.0.1"].includes(window.location.hostname)
  const host = local ? `${window.location.hostname}:8000` : window.location.host
  return `${protocol}//${host}/connection/websocket`
}

function realtimeLabel(joined: boolean, state: RealtimeState) {
  if (!joined) return "进入后开启互动"
  if (state === "online") return "● WebSocket 实时"
  if (state === "connecting") return "○ WebSocket 连接中"
  return "○ WebSocket 已断开"
}

function appendUniqueMessage(messages: Message[], message: Message) {
  if (messages.some((item) => item.message_id === message.message_id))
    return messages
  return [...messages.slice(-29), message]
}

function parseRealtimeEvent(value: unknown) {
  if (!isRecord(value) || typeof value.type !== "string") return null
  return { type: value.type, data: value.data }
}

function parseStats(value: unknown): Partial<Stats> | null {
  if (!isRecord(value)) return null
  const viewerCount = finiteNumber(value.viewer_count)
  const likeCount = finiteNumber(value.like_count)
  if (viewerCount === undefined && likeCount === undefined) return null
  return { viewer_count: viewerCount, like_count: likeCount }
}

function parseRealtimeMessage(type: "danmaku" | "gift", value: unknown) {
  if (!isRecord(value) || typeof value.message_id !== "string") return null
  if (type === "danmaku" && typeof value.content !== "string") return null
  if (type === "gift" && typeof value.gift_name !== "string") return null
  return {
    message_id: value.message_id,
    type,
    nickname:
      typeof value.nickname === "string"
        ? value.nickname
        : `用户 #${finiteNumber(value.user_id) ?? "?"}`,
    content: typeof value.content === "string" ? value.content : undefined,
    gift_name:
      typeof value.gift_name === "string" ? value.gift_name : undefined,
    count: finiteNumber(value.count),
  } satisfies Message
}

function eventReason(value: unknown, fallback: string) {
  if (!isRecord(value) || typeof value.reason !== "string") return fallback
  return `${fallback.slice(0, -1)}：${value.reason}`
}

function finiteNumber(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null
}

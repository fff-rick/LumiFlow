"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { FormEvent, useEffect, useState } from "react"
import { Centrifuge } from "centrifuge"
import AppNav from "@/components/AppNav"
import { useSession } from "@/components/SessionProvider"
import WHEPPlayer from "@/components/WHEPPlayer"

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
type FloatingMessage = Message & { lane: number }
type Gift = { gift_id: number; name: string; price: number }
type Wallet = { balance: number }
type GiftSendResult = {
  order: {
    order_no: string
    gift_name: string
    gift_count: number
    total_amount: number
  }
}
type Subscription = { channel: string; token: string }
type JoinResponse = {
  stats?: Stats
  heartbeat_interval_seconds?: number
  subscriptions: Subscription[]
  personal_channel: string
}
type RealtimeState = "offline" | "connecting" | "online"
type MediaPlayback = {
  provider?: string
  status?: string
  whep_url?: string
}
type MediaCredentials = MediaPlayback & {
  server_url: string
  stream_key: string
}
type Recording = {
  recording_id: number
  playback_url: string
  created_at: string
}

export default function LiveRoom({ roomId }: { roomId: string }) {
  const router = useRouter()
  const { user } = useSession()
  const [room, setRoom] = useState<Room | null>(null)
  const [stats, setStats] = useState<Stats>({ viewer_count: 0, like_count: 0 })
  const [messages, setMessages] = useState<Message[]>([])
  const [floatingMessages, setFloatingMessages] = useState<FloatingMessage[]>(
    []
  )
  const [gifts, setGifts] = useState<Gift[]>([])
  const [balance, setBalance] = useState<number | null>(null)
  const [sendingGiftId, setSendingGiftId] = useState<number | null>(null)
  const [crediting, setCrediting] = useState(false)
  const [joined, setJoined] = useState(false)
  const [heartbeatSeconds, setHeartbeatSeconds] = useState(20)
  const [realtimeSession, setRealtimeSession] = useState<JoinResponse | null>(
    null
  )
  const [realtimeState, setRealtimeState] = useState<RealtimeState>("offline")
  const [playback, setPlayback] = useState<MediaPlayback>({})
  const [recordings, setRecordings] = useState<Recording[]>([])
  const [selectedRecordingId, setSelectedRecordingId] = useState<number | null>(
    null
  )
  const [credentials, setCredentials] = useState<MediaCredentials | null>(null)
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
    let active = true
    Promise.all([
      get<{ items?: Gift[] }>("/api/platform/v1/gifts"),
      user ? get<Wallet>("/api/platform/v1/wallet") : Promise.resolve(null),
    ]).then(([catalog, wallet]) => {
      if (!active) return
      setGifts(catalog?.items ?? [])
      setBalance(wallet?.balance ?? null)
    })
    return () => {
      active = false
    }
  }, [user])

  useEffect(() => {
    let active = true
    async function refresh() {
      const [nextPlayback, nextRoom, nextRecordings] = await Promise.all([
        get<MediaPlayback>(`/api/platform/v1/rooms/${roomId}/playback`),
        get<Room>(`/api/platform/v1/rooms/${roomId}`),
        get<{ items?: Recording[] }>(
          `/api/platform/v1/rooms/${roomId}/recordings`
        ),
      ])
      if (!active) return
      if (nextPlayback) setPlayback(nextPlayback)
      if (nextRoom)
        setRoom((current) =>
          current?.status === "CLOSED" ? current : nextRoom
        )
      if (nextRecordings) setRecordings(nextRecordings.items ?? [])
    }
    void refresh()
    const timer = window.setInterval(refresh, 3000)
    return () => {
      active = false
      window.clearInterval(timer)
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
          if (message) {
            setMessages((current) => appendUniqueMessage(current, message))
            setFloatingMessages((current) =>
              appendFloatingMessage(current, message)
            )
          }
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
    setFloatingMessages((value) => appendFloatingMessage(value, result.data))
    event.currentTarget.reset()
  }

  async function sendGift(gift: Gift) {
    if (!joined) return setNotice("进入直播间后才可以赠送礼物。")
    setSendingGiftId(gift.gift_id)
    const result = await post<GiftSendResult>(
      `/api/platform/v1/rooms/${roomId}/gifts`,
      { gift_id: gift.gift_id, count: 1 },
      { "Idempotency-Key": crypto.randomUUID() }
    )
    setSendingGiftId(null)
    if (!result.ok) return setNotice(result.message)
    const message: Message = {
      message_id: `gift:${result.data.order.order_no}`,
      nickname: user?.nickname || "我",
      type: "gift",
      gift_name: result.data.order.gift_name,
      count: result.data.order.gift_count,
    }
    setMessages((current) => appendUniqueMessage(current, message))
    setFloatingMessages((current) => appendFloatingMessage(current, message))
    setBalance((current) =>
      current === null ? current : current - result.data.order.total_amount
    )
    setNotice(`已送出 ${result.data.order.gift_name}。`)
  }

  async function creditWallet() {
    setCrediting(true)
    const result = await post<Wallet>("/api/platform/v1/wallet/dev-credit", {
      amount: 100000,
    })
    setCrediting(false)
    if (!result.ok) return setNotice(result.message)
    setBalance(result.data.balance)
    setNotice("已领取 100,000 测试币。")
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

  async function issueMediaSession() {
    const result = await post<MediaCredentials>(
      `/api/platform/v1/rooms/${roomId}/media-session`
    )
    if (!result.ok) return setNotice(result.message)
    setCredentials(result.data)
    setPlayback(result.data)
    setNotice("OBS 推流凭证已生成；开始推流后媒体状态会自动更新。")
  }

  async function startLive() {
    const result = await post<Room>(`/api/platform/v1/rooms/${roomId}/start`)
    if (!result.ok) return setNotice(result.message)
    setRoom(result.data)
    setNotice("直播已开始。")
  }

  async function stopLive() {
    if (!window.confirm("确定结束直播吗？结束后该直播间不能重新开启。")) return
    const result = await post<Room>(`/api/platform/v1/rooms/${roomId}/stop`)
    if (!result.ok) return setNotice(result.message)
    setRoom(result.data)
    setPlayback({})
    setCredentials(null)
    setJoined(false)
    setRealtimeSession(null)
    setNotice("直播已结束，请在 OBS 中停止推流。")
  }

  if (!room)
    return (
      <main className="grid min-h-svh place-items-center bg-zinc-950 p-6 text-zinc-300">
        {notice || "正在载入直播间…"}
      </main>
    )

  const living = room.status === "LIVING"
  const closed = room.status === "CLOSED"
  const playing = playback.status === "PUBLISHING" && Boolean(playback.whep_url)
  const selectedRecording =
    recordings.find((item) => item.recording_id === selectedRecordingId) ??
    recordings[0]
  const replaying = closed && Boolean(selectedRecording?.playback_url)
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
            {joined
              ? "已进入直播间"
              : living
                ? "进入直播间"
                : closed
                  ? "直播已结束"
                  : "等待开播"}
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
            {playing && playback.whep_url && (
              <WHEPPlayer url={playback.whep_url} />
            )}
            {replaying && selectedRecording && (
              <video
                className="relative z-10 h-full w-full bg-black object-contain"
                controls
                key={selectedRecording.recording_id}
                playsInline
                preload="metadata"
                src={selectedRecording.playback_url}
              />
            )}
            {!playing && !replaying && (
              <div className="relative grid h-full place-items-center p-8 text-center">
                <div>
                  <span className="rounded-full bg-red-500 px-3 py-1 text-xs font-bold">
                    {living ? "LIVE" : room.status}
                  </span>
                  <h2 className="mt-5 text-xl font-bold">
                    {closed
                      ? recordings.length
                        ? "正在加载直播回放"
                        : "直播已结束，录像生成后会显示在这里"
                      : playback.status === "PUBLISHING"
                        ? "推流已就绪，等待开播"
                        : playback.status === "DISCONNECTED"
                          ? "主播暂时断开推流"
                          : "等待主播推流"}
                  </h2>
                  <p className="mt-3 max-w-md text-sm leading-6 text-zinc-300">
                    {playback.status
                      ? `SRS 媒体状态：${playback.status}`
                      : "该直播间尚未配置媒体会话。"}
                  </p>
                </div>
              </div>
            )}
            <div
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 z-30 overflow-hidden"
            >
              {floatingMessages.map((message) => (
                <span
                  className={`live-danmaku absolute left-full rounded-full px-4 py-2 text-sm font-semibold whitespace-nowrap shadow-lg ${message.type === "gift" ? "bg-amber-400/90 text-amber-950" : "bg-black/65 text-white"}`}
                  key={message.message_id}
                  onAnimationEnd={() =>
                    setFloatingMessages((current) =>
                      current.filter(
                        (item) => item.message_id !== message.message_id
                      )
                    )
                  }
                  style={{ top: `${12 + message.lane * 13}%` }}
                >
                  {message.type === "gift"
                    ? `🎁 ${message.nickname} 送出 ${message.gift_name} ×${message.count}`
                    : `${message.nickname}：${message.content}`}
                </span>
              ))}
            </div>
            <div
              className={`pointer-events-none absolute z-20 flex w-full justify-between border-t border-white/10 bg-black/30 px-5 py-3 text-sm text-zinc-300 ${playing || replaying ? "bottom-14" : "bottom-0"}`}
            >
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
            <section className="border-t border-white/10 p-4">
              <div className="flex items-center justify-between text-sm">
                <strong>赠送礼物</strong>
                <span className="text-xs text-amber-300">
                  余额 {balance === null ? "--" : metric(balance)} 币
                </span>
              </div>
              <div className="mt-3 grid grid-cols-3 gap-2">
                {gifts.map((gift) => (
                  <button
                    className="rounded-xl border border-white/10 bg-zinc-950 px-2 py-2 text-xs transition hover:border-amber-300/50 disabled:cursor-not-allowed disabled:opacity-40"
                    disabled={
                      !joined ||
                      balance === null ||
                      balance < gift.price ||
                      sendingGiftId !== null
                    }
                    key={gift.gift_id}
                    onClick={() => sendGift(gift)}
                    title={`${gift.name} · ${gift.price} 币`}
                    type="button"
                  >
                    <span className="block text-lg">{giftIcon(gift.name)}</span>
                    <span className="block truncate">{gift.name}</span>
                    <span className="text-amber-300">{metric(gift.price)}</span>
                  </button>
                ))}
              </div>
              {user && (
                <button
                  className="mt-3 w-full rounded-lg border border-dashed border-amber-300/30 py-2 text-xs text-amber-200 hover:bg-amber-300/10 disabled:opacity-50"
                  disabled={crediting}
                  onClick={creditWallet}
                  type="button"
                >
                  {crediting ? "领取中…" : "领取 100,000 测试币"}
                </button>
              )}
            </section>
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
        {closed && recordings.length > 0 && (
          <section className="mt-5 rounded-3xl border border-white/10 bg-zinc-900 p-5">
            <h2 className="font-bold">本场直播回放</h2>
            <p className="mt-1 text-sm text-zinc-400">
              断流重连会生成多个录像片段，可在这里切换播放。
            </p>
            <div className="mt-4 flex flex-wrap gap-2">
              {recordings.map((recording, index) => (
                <button
                  className={`rounded-xl border px-4 py-2 text-sm transition ${selectedRecording?.recording_id === recording.recording_id ? "border-violet-400 bg-violet-400/15 text-violet-100" : "border-white/15 text-zinc-300 hover:bg-white/10"}`}
                  key={recording.recording_id}
                  onClick={() => setSelectedRecordingId(recording.recording_id)}
                  type="button"
                >
                  录像片段 {recordings.length - index} · {recordingTime(recording.created_at)}
                </button>
              ))}
            </div>
          </section>
        )}
        {user?.id === room.anchor_id && room.status !== "CLOSED" && (
          <section className="mt-5 rounded-3xl border border-white/10 bg-zinc-900 p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="font-bold">OBS 推流</h2>
                <p className="mt-1 text-sm text-zinc-400">
                  先生成凭证，在 OBS 中填写服务器和推流码，然后开始推流。
                </p>
              </div>
              <div className="flex gap-2">
                <button
                  className="rounded-xl border border-white/15 px-4 py-2 text-sm font-semibold hover:bg-white/10"
                  onClick={issueMediaSession}
                  type="button"
                >
                  生成推流凭证
                </button>
                {room.status === "PREPARING" && (
                  <button
                    className="rounded-xl bg-red-500 px-4 py-2 text-sm font-semibold disabled:opacity-50"
                    disabled={playback.status !== "PUBLISHING"}
                    onClick={startLive}
                    type="button"
                  >
                    开始直播
                  </button>
                )}
                {living && (
                  <button
                    className="rounded-xl border border-red-400/50 px-4 py-2 text-sm font-semibold text-red-200 hover:bg-red-400/10"
                    onClick={stopLive}
                    type="button"
                  >
                    结束直播
                  </button>
                )}
              </div>
            </div>
            {living && playback.status === "DISCONNECTED" && (
              <p className="mt-4 rounded-xl border border-amber-300/20 bg-amber-300/10 px-4 py-3 text-sm text-amber-100">
                OBS 推流已断开。重新开始推流可继续直播；不再重连时请结束直播。
              </p>
            )}
            {credentials && (
              <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
                <div className="rounded-xl bg-black/30 p-3">
                  <dt className="text-zinc-500">服务器</dt>
                  <dd className="mt-1 font-mono break-all text-zinc-200">
                    {credentials.server_url}
                  </dd>
                </div>
                <div className="rounded-xl bg-black/30 p-3">
                  <dt className="text-zinc-500">推流码（请勿分享）</dt>
                  <dd className="mt-1 font-mono break-all text-zinc-200">
                    {credentials.stream_key}
                  </dd>
                </div>
              </dl>
            )}
          </section>
        )}
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
  body?: unknown,
  extraHeaders?: Record<string, string>
): Promise<{ ok: true; data: T } | { ok: false; message: string }> {
  try {
    const response = await fetch(url, {
      method: "POST",
      headers: body
        ? { "content-type": "application/json", ...extraHeaders }
        : extraHeaders,
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

function recordingTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? "时间未知"
    : date.toLocaleString("zh-CN", { hour12: false })
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

function appendFloatingMessage(messages: FloatingMessage[], message: Message) {
  if (messages.some((item) => item.message_id === message.message_id))
    return messages
  return [...messages.slice(-7), { ...message, lane: messages.length % 5 }]
}

function giftIcon(name: string) {
  if (name === "Rose") return "🌹"
  if (name === "Rocket") return "🚀"
  if (name === "Castle") return "🏰"
  return "🎁"
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

"use client"

import Link from "next/link"
import { Radio, RefreshCw, Sparkles, Users } from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import AppNav from "@/components/AppNav"
import { metric, platformRequest } from "@/lib/platform"
import "./Portal.css"

type Room = {
  room_id: number
  anchor_id: number
  anchor_nickname?: string
  title: string
  status: string
  viewer_count: number
  like_count: number
}

export default function LiveLobby() {
  const [rooms, setRooms] = useState<Room[]>([])
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState("")

  const load = useCallback(() => {
    setLoading(true)
    setMessage("")
    platformRequest<{ items?: Room[] }>("/rooms?status=LIVING&limit=30")
      .then((data) => setRooms(data.items ?? []))
      .catch((error) =>
        setMessage(error instanceof Error ? error.message : "直播大厅加载失败")
      )
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    void Promise.resolve().then(load)
  }, [load])

  return (
    <main className="portal-page">
      <AppNav />
      <div className="portal-shell">
        <header className="live-lobby-heading">
          <div>
            <p>LUMI LIVE</p>
            <h1>直播大厅</h1>
            <span>进入正在直播的房间，参与弹幕、点赞与实时互动。</span>
          </div>
          <div className="live-lobby-status">
            <i /> {rooms.length} 个直播间
          </div>
        </header>

        {message && (
          <div className="portal-message error live-lobby-error">
            <span>{message}</span>
            <button onClick={load} type="button">
              <RefreshCw size={14} /> 重试
            </button>
          </div>
        )}

        {loading ? (
          <div className="live-room-grid">
            {Array.from({ length: 6 }, (_, index) => (
              <div className="portal-skeleton" key={index} />
            ))}
          </div>
        ) : rooms.length ? (
          <div className="live-room-grid">
            {rooms.map((room, index) => (
              <Link
                className={`live-lobby-card room-${index % 3}`}
                href={`/rooms/${room.room_id}`}
                key={room.room_id}
              >
                <div className="live-lobby-cover">
                  <span>
                    <i /> LIVE
                  </span>
                  <Radio size={34} />
                  <small>
                    <Users size={13} /> {metric(room.viewer_count)} 人在线
                  </small>
                </div>
                <div>
                  <strong>{room.title || `直播间 #${room.room_id}`}</strong>
                  <p>{room.anchor_nickname || `主播 #${room.anchor_id}`}</p>
                  <small>♥ {metric(room.like_count)} 点赞</small>
                </div>
              </Link>
            ))}
          </div>
        ) : (
          <section className="portal-empty live-lobby-empty inline">
            <Sparkles size={32} />
            <h2>暂时没有正在直播的房间</h2>
            <p>有主播开播后会自动出现在这里。</p>
            <Link className="portal-secondary" href="/">
              去看看视频
            </Link>
          </section>
        )}
      </div>
    </main>
  )
}

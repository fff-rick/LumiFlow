"use client"

import HalftoneReveal from "@/components/HalftoneReveal"
import StarBorder from "@/components/StarBorder"
import Link from "next/link"
import { useState } from "react"

export default function Page() {
  const [revealComplete, setRevealComplete] = useState(false)

  return (
    <main className="min-h-svh bg-zinc-950 p-6 text-white sm:p-10">
      <section className="relative mx-auto h-[min(680px,calc(100svh-3rem))] max-w-6xl overflow-hidden rounded-3xl border border-white/10 bg-zinc-900">
        <HalftoneReveal
          src="/images/lumiflow-hero.jpg"
          className="absolute inset-0"
          mode="mono"
          inkColor="#d4d4d8"
          paperColor="#09090b"
          cellSize={7}
          revealRadius={180}
          clickBurst={false}
          rainReveal
          onRevealComplete={() => setRevealComplete(true)}
        />

        {revealComplete && (
          <div className="absolute right-8 bottom-8 z-20 sm:right-12 sm:bottom-12">
            <StarBorder
              as={Link}
              aria-label="进入 LumiFlow 功能页"
              backgroundColor="rgba(53, 58, 117, 0.34)"
              borderColor="rgba(216, 220, 255, 0.22)"
              className="text-sm tracking-[0.1em] shadow-[0_10px_30px_rgba(37,35,91,0.22)] backdrop-blur-md"
              color="#d8d4ff"
              duration={3.6}
              glow={0.3}
              href="/discover"
              radius={999}
              sparkle
              stars={1}
              style={{ fontWeight: 600, padding: "1rem 2rem" }}
              textColor="#f1f3ff"
              thickness={1}
              trailColor="#7dd3fc"
              trailLength={0.22}
            >
              立即前往
            </StarBorder>
          </div>
        )}

        <div className="pointer-events-none relative z-10 flex h-full items-end p-8 sm:p-12">
          <div className="max-w-xl">
            <p className="mb-3 text-sm font-medium tracking-[0.2em] text-violet-200">
              VIDEO · LIVE · COMMUNITY
            </p>
            <h1 className="text-5xl font-bold tracking-tight sm:text-7xl">
              LumiFlow
            </h1>
            <p className="mt-5 text-base leading-7 text-zinc-200 sm:text-lg">
              视频、直播与实时互动汇聚于同一处。
            </p>
            <Link
              className="pointer-events-auto mt-7 inline-flex rounded-xl bg-white px-5 py-3 text-sm font-semibold text-zinc-950 transition hover:bg-violet-100"
              href="/login"
            >
              登录或注册
            </Link>
          </div>
        </div>
      </section>
    </main>
  )
}

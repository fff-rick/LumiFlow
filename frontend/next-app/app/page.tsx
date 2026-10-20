import HalftoneReveal from "@/components/HalftoneReveal"
import Link from "next/link"

export default function Page() {
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
        />

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

"use client"

import Link from "next/link"
import { FormEvent, useState } from "react"
import { useRouter } from "next/navigation"

type Mode = "login" | "register"

export default function LoginPage() {
  const router = useRouter()
  const [mode, setMode] = useState<Mode>("login")
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState("")

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setPending(true)
    setMessage("")
    const response = await fetch(`/api/platform/v1/auth/${mode}`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        account: form.get("account"),
        password: form.get("password"),
        ...(mode === "register" ? { nickname: form.get("nickname") } : {}),
      }),
    })
    const body = (await response.json().catch(() => ({}))) as {
      message?: string
    }
    setPending(false)
    if (!response.ok) {
      setMessage(body.message ?? "请求失败，请稍后重试")
      return
    }
    router.replace("/")
    router.refresh()
  }

  return (
    <main className="grid min-h-svh place-items-center bg-zinc-950 p-6 text-white">
      <section className="w-full max-w-md rounded-3xl border border-white/10 bg-zinc-900 p-7 shadow-2xl sm:p-9">
        <Link
          href="/"
          className="text-sm text-zinc-400 transition hover:text-white"
        >
          ← LumiFlow
        </Link>
        <p className="mt-8 text-xs font-semibold tracking-[0.22em] text-violet-300">
          FLUXFEED IDENTITY
        </p>
        <h1 className="mt-2 text-3xl font-bold">
          {mode === "login" ? "欢迎回来" : "创建你的账号"}
        </h1>
        <p className="mt-2 text-sm leading-6 text-zinc-400">
          一个账号即可使用视频、直播和实时互动。
        </p>

        <form className="mt-7 space-y-4" onSubmit={submit}>
          {mode === "register" && (
            <Field
              label="昵称"
              name="nickname"
              autoComplete="nickname"
              required
            />
          )}
          <Field
            label="邮箱"
            name="account"
            type="email"
            autoComplete="email"
            required
          />
          <Field
            label="密码"
            name="password"
            type="password"
            autoComplete={
              mode === "login" ? "current-password" : "new-password"
            }
            minLength={8}
            required
          />
          {message && (
            <p className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-200">
              {message}
            </p>
          )}
          <button
            className="w-full rounded-xl bg-violet-500 px-4 py-3 text-sm font-semibold transition hover:bg-violet-400 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={pending}
          >
            {pending ? "处理中…" : mode === "login" ? "登录" : "注册并登录"}
          </button>
        </form>

        <button
          className="mt-6 text-sm text-zinc-400 underline-offset-4 hover:text-white hover:underline"
          onClick={() => {
            setMode(mode === "login" ? "register" : "login")
            setMessage("")
          }}
          type="button"
        >
          {mode === "login" ? "没有账号？立即注册" : "已有账号？去登录"}
        </button>
      </section>
    </main>
  )
}

function Field({
  label,
  ...props
}: React.ComponentProps<"input"> & { label: string }) {
  return (
    <label className="block text-sm font-medium text-zinc-200">
      {label}
      <input
        className="mt-2 w-full rounded-xl border border-white/10 bg-zinc-950 px-3 py-3 text-white transition outline-none placeholder:text-zinc-600 focus:border-violet-400"
        {...props}
      />
    </label>
  )
}

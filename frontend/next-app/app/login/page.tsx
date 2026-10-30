"use client"

import Link from "next/link"
import { CSSProperties, FormEvent, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { useSession } from "@/components/SessionProvider"
import "./login.css"

type Mode = "login" | "register"
type Capabilities = {
  oauth_providers: string[]
}

export default function LoginPage() {
  const router = useRouter()
  const { refreshSession } = useSession()
  const [mode, setMode] = useState<Mode>("login")
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState("")
  const [capabilities, setCapabilities] = useState<Capabilities>({
    oauth_providers: [],
  })

  useEffect(() => {
    fetch("/api/platform/v1/auth/capabilities")
      .then(async (response) => {
        if (!response.ok) return
        const result = (await response.json()) as Capabilities
        setCapabilities({ oauth_providers: result.oauth_providers ?? [] })
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    const hash = new URLSearchParams(window.location.hash.slice(1))
    const code = hash.get("oauth-code")
    const error = hash.get("auth-error")
    if (error) {
      const timer = window.setTimeout(() => setMessage(error), 0)
      window.history.replaceState({}, "", "/login")
      return () => window.clearTimeout(timer)
    }
    if (!code) return

    let active = true
    Promise.resolve().then(async () => {
      if (!active) return
      setPending(true)
      const result = await requestOAuthSession(code)
      if (!active) return
      if (!result.ok) {
        setMessage(result.message)
      } else {
        await refreshSession()
        router.replace("/discover")
        router.refresh()
      }
      setPending(false)
      window.history.replaceState({}, "", "/login")
    })
    return () => {
      active = false
    }
  }, [refreshSession, router])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const account = String(form.get("account") ?? "").trim()
    const password = String(form.get("password") ?? "")
    const nickname = String(form.get("nickname") ?? "").trim()
    setPending(true)
    setMessage("")
    try {
      // FluxFeed creates the account first, then establishes a fresh session.
      // Keep that sequence so registration and normal sign-in share one path.
      if (mode === "register") {
        const registration = await request("register", {
          account,
          password,
          nickname,
        })
        if (!registration.ok) {
          setMessage(registration.message)
          return
        }
      }
      const login = await request("login", { account, password })
      if (!login.ok) {
        setMessage(login.message)
        return
      }
      await refreshSession()
      router.replace("/discover")
      router.refresh()
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="flux-auth-page">
      <section className="flux-auth-visual" aria-label="LumiFlow">
        <AeroShards />
        <div className="flux-auth-logo-scene">
          <StrokeLogo text="LumiFlow" />
          <p>VIDEO · LIVE · COMMUNITY</p>
          <span>发现值得停留的每一帧</span>
        </div>
      </section>
      <section className="flux-auth-panel">
        <div className="flux-auth-card">
          <Link href="/" className="flux-auth-back">
            ← 返回 LumiFlow
          </Link>
          <div className="flux-brand-block">
            <span className="flux-brand-mark" aria-hidden="true">
              ▶
            </span>
            <div>
              <h1>
                {mode === "login"
                  ? "欢迎来到 LumiFlow"
                  : "创建你的 LumiFlow 账号"}
              </h1>
              <p>登录后继续发现值得停留的每一帧。</p>
            </div>
          </div>

          <form className="flux-auth-form" onSubmit={submit}>
            <div className="flux-auth-mode-tabs" aria-label="登录或注册">
              <button
                className={mode === "login" ? "active" : ""}
                onClick={() => {
                  setMode("login")
                  setMessage("")
                }}
                type="button"
              >
                登录
              </button>
              <button
                className={mode === "register" ? "active" : ""}
                onClick={() => {
                  setMode("register")
                  setMessage("")
                }}
                type="button"
              >
                注册
              </button>
            </div>
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
              autoComplete="username"
              placeholder="name@example.com"
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
              placeholder="输入密码"
              required
            />
            {message && <p className="flux-form-message">{message}</p>}
            <button className="flux-primary-button" disabled={pending}>
              {pending ? "处理中…" : mode === "login" ? "登录" : "注册并登录"}
            </button>
            {mode === "login" && (
              <OAuthButtons
                providers={capabilities.oauth_providers}
                pending={pending}
              />
            )}
          </form>
        </div>
      </section>
    </main>
  )
}

function OAuthButtons({
  providers,
  pending,
}: {
  providers: string[]
  pending: boolean
}) {
  const enabled = new Set(providers.map((provider) => provider.toLowerCase()))
  return (
    <div className="flux-oauth-section">
      <span>或使用以下方式登录</span>
      <div className="flux-oauth-buttons">
        <OAuthButton
          provider="google"
          enabled={enabled.has("google")}
          pending={pending}
        />
        <OAuthButton
          provider="github"
          enabled={enabled.has("github")}
          pending={pending}
        />
      </div>
      {!enabled.size && (
        <small>管理员配置 Google 或 GitHub OAuth 后即可启用。</small>
      )}
    </div>
  )
}

function OAuthButton({
  provider,
  enabled,
  pending,
}: {
  provider: "google" | "github"
  enabled: boolean
  pending: boolean
}) {
  const label = provider === "google" ? "Google" : "GitHub"
  const content = (
    <>
      <strong>{provider === "google" ? "G" : "GH"}</strong>
      {label}
    </>
  )
  if (!enabled || pending) {
    return (
      <span
        className="flux-oauth-button is-disabled"
        title={`${label} 登录尚未配置`}
      >
        {content}
      </span>
    )
  }
  return (
    <a
      className="flux-oauth-button"
      href={`/api/oauth/${provider}`}
      title={enabled ? `使用 ${label} 登录` : `${label} 登录尚未配置`}
    >
      {content}
    </a>
  )
}

function Field({
  label,
  ...props
}: React.ComponentProps<"input"> & { label: string }) {
  return (
    <label className="flux-field">
      <span>{label}</span>
      <input {...props} />
    </label>
  )
}

async function request(path: Mode, body: Record<string, string>) {
  try {
    const response = await fetch(`/api/platform/v1/auth/${path}`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    })
    const payload = (await response.json().catch(() => ({}))) as {
      error?: string
      message?: string
    }
    return {
      ok: response.ok,
      message: payload.message ?? payload.error ?? "请求失败，请稍后重试",
    }
  } catch {
    return { ok: false, message: "网络异常，请稍后重试" }
  }
}

async function requestOAuthSession(code: string) {
  try {
    const response = await fetch("/api/platform/v1/auth/oauth/sessions", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ code }),
    })
    const payload = (await response.json().catch(() => ({}))) as {
      error?: string
      message?: string
    }
    return {
      ok: response.ok,
      message: payload.message ?? payload.error ?? "第三方登录失败",
    }
  } catch {
    return { ok: false, message: "第三方登录服务暂不可用" }
  }
}

function StrokeLogo({ text }: { text: string }) {
  return (
    <div className="flux-stroke-logo" aria-label={text}>
      <svg viewBox="0 0 820 210" role="img">
        <defs>
          <linearGradient id="strokeGradient" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor="#72e7ff" />
            <stop offset="48%" stopColor="#a78bfa" />
            <stop offset="100%" stopColor="#ff78c8" />
          </linearGradient>
        </defs>
        <text
          x="50%"
          y="66%"
          textAnchor="middle"
          className="flux-stroke-logo-text"
        >
          {text}
        </text>
      </svg>
    </div>
  )
}

function AeroShards() {
  return (
    <div className="flux-aero-shards" aria-hidden="true">
      {Array.from({ length: 14 }, (_, index) => (
        <i key={index} style={{ "--i": index } as CSSProperties} />
      ))}
      <div className="flux-aero-glow flux-glow-one" />
      <div className="flux-aero-glow flux-glow-two" />
    </div>
  )
}

import { NextResponse } from "next/server"
import { cookies } from "next/headers"

const sessionCookie = "lumiflow_access_token"

type RouteContext = { params: Promise<{ path: string[] }> }

export async function GET(request: Request, context: RouteContext) {
  return proxy(request, context)
}

export async function POST(request: Request, context: RouteContext) {
  return proxy(request, context)
}

export async function PUT(request: Request, context: RouteContext) {
  return proxy(request, context)
}

export async function PATCH(request: Request, context: RouteContext) {
  return proxy(request, context)
}

export async function DELETE(request: Request, context: RouteContext) {
  return proxy(request, context)
}

async function proxy(request: Request, context: RouteContext) {
  const { path } = await context.params
  const upstream = new URL(`/api/${path.join("/")}`, apiOrigin())
  upstream.search = new URL(request.url).search

  const headers = new Headers()
  const contentType = request.headers.get("content-type")
  const idempotencyKey = request.headers.get("idempotency-key")
  if (contentType) headers.set("content-type", contentType)
  if (idempotencyKey) headers.set("idempotency-key", idempotencyKey)
  const token = (await cookies()).get(sessionCookie)?.value
  if (token) headers.set("authorization", `Bearer ${token}`)

  const method = request.method
  const body =
    method === "GET" || method === "HEAD"
      ? undefined
      : await request.arrayBuffer()
  let upstreamResponse: Response
  try {
    upstreamResponse = await fetch(upstream, {
      method,
      headers,
      body,
      cache: "no-store",
    })
  } catch {
    return NextResponse.json(
      { code: "API_UNAVAILABLE", message: "平台服务暂不可用" },
      { status: 503 }
    )
  }

  const payload = await upstreamResponse.text()
  if (isSessionRoute(path) && upstreamResponse.ok) {
    return sessionResponse(payload, upstreamResponse.status)
  }
  return new NextResponse(payload, {
    status: upstreamResponse.status,
    headers: {
      "content-type":
        upstreamResponse.headers.get("content-type") ?? "application/json",
    },
  })
}

function apiOrigin() {
  return process.env.LUMIFLOW_API_ORIGIN ?? "http://127.0.0.1:8080"
}

function isSessionRoute(path: string[]) {
  const route = path.join("/")
  return route === "v1/auth/login" || route === "v1/auth/register" || route === "v1/auth/oauth/sessions"
}

function sessionResponse(payload: string, status: number) {
  try {
    const value = JSON.parse(payload) as {
      access_token?: string
      expires_in_seconds?: number
    }
    if (!value.access_token) throw new Error("missing access token")
    const { access_token: token, ...safeBody } = value
    const response = NextResponse.json(safeBody, { status })
    response.cookies.set(sessionCookie, token, {
      httpOnly: true,
      sameSite: "lax",
      secure: process.env.NODE_ENV === "production",
      path: "/",
      maxAge: Math.max(1, value.expires_in_seconds ?? 900),
    })
    return response
  } catch {
    return NextResponse.json(
      { code: "AUTH_RESPONSE_INVALID", message: "登录服务返回无效响应" },
      { status: 502 }
    )
  }
}

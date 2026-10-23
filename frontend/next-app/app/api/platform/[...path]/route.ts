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
  const range = request.headers.get("range")
  const accept = request.headers.get("accept")
  if (contentType) headers.set("content-type", contentType)
  if (idempotencyKey) headers.set("idempotency-key", idempotencyKey)
  if (range) headers.set("range", range)
  if (accept) headers.set("accept", accept)
  const token = (await cookies()).get(sessionCookie)?.value
  if (token) headers.set("authorization", `Bearer ${token}`)

  const method = request.method
  const body = method === "GET" || method === "HEAD" ? undefined : request.body
  let upstreamResponse: Response
  try {
    const init: RequestInit & { duplex?: "half" } = {
      method,
      headers,
      body,
      cache: "no-store",
    }
    if (body) init.duplex = "half"
    upstreamResponse = await fetch(upstream, init)
  } catch {
    return NextResponse.json(
      { code: "API_UNAVAILABLE", message: "平台服务暂不可用" },
      { status: 503 }
    )
  }

  if (isSessionRoute(path) && upstreamResponse.ok) {
    const payload = await upstreamResponse.text()
    return sessionResponse(
      payload,
      upstreamResponse.status,
      isSecureRequest(request)
    )
  }
  const responseHeaders = new Headers()
  for (const name of [
    "accept-ranges",
    "content-disposition",
    "content-length",
    "content-range",
    "content-type",
    "etag",
    "last-modified",
  ]) {
    const value = upstreamResponse.headers.get(name)
    if (value) responseHeaders.set(name, value)
  }
  return new NextResponse(upstreamResponse.body, {
    status: upstreamResponse.status,
    headers: responseHeaders,
  })
}

function apiOrigin() {
  return process.env.LUMIFLOW_API_ORIGIN ?? "http://127.0.0.1:8080"
}

function isSessionRoute(path: string[]) {
  const route = path.join("/")
  return (
    route === "v1/auth/login" ||
    route === "v1/auth/register" ||
    route === "v1/auth/oauth/sessions"
  )
}

function sessionResponse(payload: string, status: number, secure: boolean) {
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
      secure,
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

function isSecureRequest(request: Request) {
  const forwardedProto = request.headers
    .get("x-forwarded-proto")
    ?.split(",", 1)[0]
    .trim()
  return forwardedProto ? forwardedProto === "https" : request.url.startsWith("https:")
}

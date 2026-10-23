import { NextResponse } from "next/server"

type RouteContext = { params: Promise<{ path: string[] }> }

export async function GET(request: Request, context: RouteContext) {
  const { path } = await context.params
  const upstream = new URL(
    `/api/v1/oauth/${path.join("/")}`,
    process.env.LUMIFLOW_API_ORIGIN ?? "http://127.0.0.1:8080"
  )
  upstream.search = new URL(request.url).search

  try {
    const response = await fetch(upstream, {
      headers: forwardHeaders(request),
      redirect: "manual",
      cache: "no-store",
    })
    const next = new NextResponse(null, {
      status: response.status,
      headers: copyHeaders(response),
    })
    for (const cookie of response.headers.getSetCookie()) {
      next.headers.append("set-cookie", cookie)
    }
    return next
  } catch {
    return NextResponse.json({ message: "第三方登录服务暂不可用" }, { status: 503 })
  }
}

function forwardHeaders(request: Request) {
  const headers = new Headers()
  const cookie = request.headers.get("cookie")
  if (cookie) headers.set("cookie", cookie)
  return headers
}

function copyHeaders(response: Response) {
  const headers = new Headers()
  const location = response.headers.get("location")
  if (location) headers.set("location", location)
  return headers
}

export type SessionUser = {
  id: number
  account?: string
  nickname?: string
  avatar_url?: string
  bio?: string
  following_count?: number
  follower_count?: number
  work_count?: number
}

export type Video = {
  id: number
  video_id?: number
  author_id: number
  author_nickname?: string
  title: string
  description?: string
  media_url?: string
  cover_url?: string
  status?: number
  status_name?: string
  review_reason?: string
  like_count: number
  comment_count: number
  liked?: boolean
  created_at?: string
  published_at?: string
}

export type RelationUser = {
  user_id: number
  nickname: string
  avatar_url?: string
  bio?: string
  followed_at?: string
}

export class PlatformError extends Error {
  constructor(
    message: string,
    readonly status: number
  ) {
    super(message)
  }
}

export async function platformRequest<T>(
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData)) {
    headers.set("content-type", "application/json")
  }
  const response = await fetch(`/api/platform/v1${path}`, {
    ...init,
    headers,
    cache: "no-store",
  })
  const payload = await response.text()
  const data = payload ? safeJSON(payload) : null
  if (!response.ok) {
    const message = isRecord(data)
      ? String(data.message ?? data.error ?? "请求失败，请稍后重试")
      : "请求失败，请稍后重试"
    throw new PlatformError(message, response.status)
  }
  return data as T
}

export async function uploadAsset(
  file: File,
  kind: "video" | "cover" | "avatar"
) {
  const body = new FormData()
  body.append("file", file)
  body.append("kind", kind)
  return platformRequest<{ url: string; filename: string; size: number }>(
    "/content-assets",
    { method: "POST", body }
  )
}

export function assetURL(value?: string) {
  if (!value || value.startsWith("http://") || value.startsWith("https://"))
    return value
  if (value.startsWith("/uploads/")) {
    return `/api/platform/v1/content-assets/${value.slice("/uploads/".length)}`
  }
  if (value.startsWith("/")) return value
  return `/api/platform/v1/content-assets/${value.replace(/^\/+/, "")}`
}

export function videoID(video: Video) {
  return video.id || video.video_id || 0
}

export function metric(value = 0) {
  return new Intl.NumberFormat("zh-CN", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(Number(value) || 0)
}

export function statusLabel(video: Video) {
  const status = video.status_name
  if (status === "published" || video.status === 2) return "已发布"
  if (status === "rejected" || video.status === 4) return "未通过"
  if (status === "deleted" || video.status === 3) return "已删除"
  return "审核中"
}

function safeJSON(value: string): unknown {
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null
}

"use client"

import Link from "next/link"
import { FormEvent, useEffect, useMemo, useState } from "react"
import {
  CheckCircle2,
  FileVideo,
  Image as ImageIcon,
  Send,
  UploadCloud,
} from "lucide-react"
import AppNav from "@/components/AppNav"
import { useSession } from "@/components/SessionProvider"
import { platformRequest, uploadAsset, Video } from "@/lib/platform"
import "./Portal.css"

const maxVideoBytes = 512 * 1024 * 1024
const maxCoverBytes = 20 * 1024 * 1024

export default function PublishVideo() {
  const { user, loading } = useSession()
  const [videoFile, setVideoFile] = useState<File | null>(null)
  const [coverFile, setCoverFile] = useState<File | null>(null)
  const coverPreview = useMemo(
    () => (coverFile ? URL.createObjectURL(coverFile) : ""),
    [coverFile]
  )
  const [pending, setPending] = useState(false)
  const [stage, setStage] = useState("")
  const [error, setError] = useState("")
  const [published, setPublished] = useState<Video | null>(null)

  useEffect(() => {
    if (!coverPreview) return
    return () => URL.revokeObjectURL(coverPreview)
  }, [coverPreview])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setError("")
    setPublished(null)
    try {
      validateFiles(videoFile, coverFile)
      setPending(true)
      setStage("正在上传视频与封面…")
      const [media, cover] = await Promise.all([
        uploadAsset(videoFile!, "video"),
        uploadAsset(coverFile!, "cover"),
      ])
      setStage("文件已上传，正在提交内容审核…")
      const result = await platformRequest<Video>("/videos", {
        method: "POST",
        headers: { "idempotency-key": `lumi-publish-${crypto.randomUUID()}` },
        body: JSON.stringify({
          title: String(form.get("title") ?? "").trim(),
          description: String(form.get("description") ?? "").trim(),
          media_url: media.url,
          cover_url: cover.url,
        }),
      })
      setPublished(result)
      setStage("")
    } catch (reason) {
      setError(
        reason instanceof Error ? reason.message : "发布失败，请稍后重试"
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="portal-page">
      <AppNav />
      <div className="portal-shell">
        {!loading && !user ? (
          <LoginRequired title="登录后发布作品" />
        ) : published ? (
          <section className="portal-result">
            <CheckCircle2 size={42} />
            <p>LUMIFLOW · PUBLISH</p>
            <h1>作品已提交审核</h1>
            <span>审核状态会显示在个人作品页；通过后将进入内容流。</span>
            <div>
              <Link className="portal-primary" href="/me">
                查看我的作品
              </Link>
              <Link className="portal-secondary" href="/">
                返回发现
              </Link>
            </div>
          </section>
        ) : (
          <section className="publish-card">
            <header className="portal-heading">
              <div>
                <p>LUMIFLOW · CREATOR</p>
                <h1>发布视频</h1>
                <span>上传内容后会进入 FluxFeed 审核流程。</span>
              </div>
              <UploadCloud size={36} />
            </header>
            <form className="publish-grid" onSubmit={submit}>
              <div className="publish-form">
                <label>
                  <span>标题</span>
                  <input
                    maxLength={80}
                    name="title"
                    placeholder="为作品起一个清晰的标题"
                    required
                  />
                </label>
                <label>
                  <span>简介</span>
                  <textarea
                    maxLength={1000}
                    name="description"
                    placeholder="介绍这段视频的内容"
                    rows={5}
                  />
                </label>
                <FileField
                  accept="video/mp4,video/webm,video/quicktime"
                  icon={<FileVideo />}
                  label="视频文件"
                  note="MP4 / MOV / WebM，最大 512 MB"
                  onChange={setVideoFile}
                  value={videoFile}
                />
                <FileField
                  accept="image/jpeg,image/png,image/webp"
                  icon={<ImageIcon />}
                  label="封面图片"
                  note="JPG / PNG / WebP，最大 20 MB"
                  onChange={setCoverFile}
                  value={coverFile}
                />
                {(stage || error) && (
                  <p
                    className={
                      error ? "portal-message error" : "portal-message"
                    }
                  >
                    {error || stage}
                  </p>
                )}
                <button
                  className="portal-primary publish-submit"
                  disabled={loading || !user || pending}
                >
                  <Send size={17} /> {pending ? "处理中…" : "提交审核"}
                </button>
              </div>
              <aside className="publish-preview">
                <div
                  style={
                    coverPreview
                      ? { backgroundImage: `url("${coverPreview}")` }
                      : undefined
                  }
                >
                  {!coverPreview && <ImageIcon size={40} />}
                </div>
                <strong>{coverFile ? coverFile.name : "封面预览"}</strong>
                <span>
                  {videoFile
                    ? `${videoFile.name} · ${formatBytes(videoFile.size)}`
                    : "请选择视频与封面文件"}
                </span>
              </aside>
            </form>
          </section>
        )}
      </div>
    </main>
  )
}

function FileField({
  accept,
  icon,
  label,
  note,
  onChange,
  value,
}: {
  accept: string
  icon: React.ReactNode
  label: string
  note: string
  onChange: (file: File | null) => void
  value: File | null
}) {
  return (
    <label>
      <span>{label}</span>
      <span className="portal-file">
        {icon}
        <span>
          <strong>{value?.name || `选择${label}`}</strong>
          <small>{value ? formatBytes(value.size) : note}</small>
        </span>
        <input
          accept={accept}
          onChange={(event) => onChange(event.target.files?.[0] ?? null)}
          type="file"
          required
        />
      </span>
    </label>
  )
}

function LoginRequired({ title }: { title: string }) {
  return (
    <section className="portal-empty">
      <UploadCloud size={34} />
      <h1>{title}</h1>
      <p>登录状态会在视频、直播和创作者页面之间保持一致。</p>
      <Link className="portal-primary" href="/login">
        登录 / 注册
      </Link>
    </section>
  )
}

function validateFiles(video: File | null, cover: File | null) {
  if (!video) throw new Error("请选择视频文件")
  if (!cover) throw new Error("请选择封面图片")
  if (video.size > maxVideoBytes) throw new Error("视频不能超过 512 MB")
  if (cover.size > maxCoverBytes) throw new Error("封面不能超过 20 MB")
}

function formatBytes(value: number) {
  return value >= 1024 * 1024
    ? `${(value / 1024 / 1024).toFixed(1)} MB`
    : `${Math.ceil(value / 1024)} KB`
}

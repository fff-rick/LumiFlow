import VideoDetail from "@/components/VideoDetail"

export default async function VideoPage({
  params,
}: {
  params: Promise<{ videoId: string }>
}) {
  const { videoId } = await params
  return <VideoDetail videoId={videoId} />
}

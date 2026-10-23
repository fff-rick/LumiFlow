import CreatorProfile from "@/components/CreatorProfile"

export default async function UserPage({
  params,
}: {
  params: Promise<{ userId: string }>
}) {
  const { userId } = await params
  return <CreatorProfile userId={userId} />
}

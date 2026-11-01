import { ChannelVideos } from "@/features/channels/ChannelVideos";

export default async function Page({ params }: { params: Promise<{ channelId: string }> }) {
  const { channelId } = await params;
  return <ChannelVideos channelId={channelId} kind="videos" />;
}

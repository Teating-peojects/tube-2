"use client";
import { useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { VideoPlayer } from "@/components/video/VideoPlayer";
import { CommentSection } from "@/features/comments/CommentSection";
import { getVideo, getVideos } from "@/services/videos";
import { useUIStore } from "@/store/ui";
import type { VideoDetail } from "@/types";
import { ActionBar } from "./ActionBar";
import { CreatorPanel } from "./CreatorPanel";
import { RelatedVideos } from "./RelatedVideos";
import { usePlaybackReporting } from "./usePlaybackReporting";

export function WatchView({ videoId, initial }: { videoId: string; initial: VideoDetail }) {
  const router = useRouter();
  const autoplayNext = useUIStore((s) => s.autoplayNext);
  const { data: video } = useQuery({ queryKey: ["video", videoId], queryFn: () => getVideo(videoId), initialData: initial });
  // Shares the cache entry with <RelatedVideos/>, so no extra request.
  const related = useQuery({ queryKey: ["related", videoId], queryFn: () => getVideos({}) });
  const next = related.data?.items.find((v) => v.id !== videoId);
  // Views and resume position (the hook resets its own state when the video id changes).
  const playback = usePlaybackReporting(video.id);

  return (
    <div className="mx-auto grid max-w-[1700px] gap-6 lg:grid-cols-[minmax(0,1fr)_380px]">
      <div className="min-w-0 space-y-4">
        {/* key resets the player when navigating to another video */}
        <VideoPlayer key={video.id} src={video.streamUrl} poster={video.posterUrl} title={video.title} captions={video.captions}
          onPlay={playback.onPlay} onTime={playback.onTime} onPause={playback.onPause}
          onEnded={() => { if (autoplayNext && next) router.push(`/watch/${next.id}`); }} />
        <h1 className="text-lg font-bold leading-snug md:text-xl">{video.title}</h1>
        <ActionBar video={video} />
        <CreatorPanel video={video} />
        <CommentSection key={video.id} videoId={video.id} total={video.commentCount} />
      </div>
      <RelatedVideos currentId={video.id} />
    </div>
  );
}

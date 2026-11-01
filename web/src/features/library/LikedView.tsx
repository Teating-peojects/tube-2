"use client";
import { Heart } from "@phosphor-icons/react";
import { EmptyState } from "@/components/ui/EmptyState";
import { VideoRow } from "@/features/videos/VideoRow";
import { getLiked } from "@/services/library";
import type { Video } from "@/types";
import { PagedList } from "./PagedList";

export function LikedView() {
  return (
    <div className="mx-auto max-w-5xl">
      <h1 className="mb-6 text-2xl font-extrabold">Liked videos</h1>
      <PagedList<Video>
        queryKey={["liked"]} fetchPage={getLiked} getKey={(v) => v.id} errorLabel="Couldn’t load liked videos"
        empty={<EmptyState icon={<Heart size={28} />} title="No liked videos" text="Videos you like will appear here." action={{ label: "Browse videos", href: "/" }} />}
        render={(v) => <VideoRow video={v} />} />
    </div>
  );
}

"use client";
import { Broadcast } from "@phosphor-icons/react";
import { EmptyState } from "@/components/ui/EmptyState";
import { PagedList } from "@/features/library/PagedList";
import { VideoCard } from "@/features/videos/VideoCard";
import { getLive } from "@/services/videos";
import type { Video } from "@/types";

export function LiveView() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-extrabold">Live now</h1>
      <PagedList<Video> layout="grid" queryKey={["live"]} fetchPage={getLive} getKey={(v) => v.id} errorLabel="Couldn’t load live streams"
        empty={<EmptyState icon={<Broadcast size={28} />} title="No one is live right now" text="Check back soon, or browse trending videos." action={{ label: "See trending", href: "/trending" }} />}
        render={(v, i) => <VideoCard video={v} priority={i < 4} />} />
    </div>
  );
}

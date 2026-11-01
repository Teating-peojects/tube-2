"use client";
import { Clock, X } from "@phosphor-icons/react";
import { EmptyState } from "@/components/ui/EmptyState";
import { VideoRow } from "@/features/videos/VideoRow";
import { getWatchLater } from "@/services/library";
import type { Video } from "@/types";
import { PagedList } from "./PagedList";
import { useToggleWatchLater, wlListKey } from "./useWatchLater";

function Row({ video }: { video: Video }) {
  const { saved, loaded, toggle, pending } = useToggleWatchLater(video.id);
  if (loaded && !saved) return null; // removed (optimistic)
  return (
    <div className="relative">
      <VideoRow video={video} />
      <button type="button" onClick={toggle} disabled={pending} aria-label={`Remove ${video.title} from Watch Later`}
        className="absolute right-0 top-0 grid size-9 place-items-center rounded-full text-fg-2 hover:bg-surface hover:text-white sm:top-1"><X size={16} /></button>
    </div>
  );
}

export function WatchLaterView() {
  return (
    <div className="mx-auto max-w-5xl">
      <h1 className="mb-6 text-2xl font-extrabold">Watch later</h1>
      <PagedList<Video>
        queryKey={wlListKey} fetchPage={getWatchLater} getKey={(v) => v.id} errorLabel="Couldn’t load Watch Later"
        empty={<EmptyState icon={<Clock size={28} />} title="Nothing saved yet" text="Use the clock icon on any video to save it for later." action={{ label: "Browse videos", href: "/" }} />}
        render={(v) => <Row video={v} />} />
    </div>
  );
}

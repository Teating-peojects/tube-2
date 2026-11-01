"use client";
import { Clock } from "@phosphor-icons/react";
import { useToggleWatchLater } from "@/features/library/useWatchLater";
import { cn } from "@/utils/format";

/** Overlay button for thumbnails. Visible on hover/focus, always when saved, and always on touch devices. */
export function WatchLaterButton({ videoId }: { videoId: string }) {
  const { saved, toggle, pending } = useToggleWatchLater(videoId);
  return (
    <button type="button" onClick={toggle} disabled={pending} aria-pressed={saved}
      aria-label={saved ? "Remove from Watch Later" : "Add to Watch Later"}
      className={cn("absolute right-2 top-2 grid size-9 place-items-center rounded-full bg-bg/80 backdrop-blur transition-opacity hover:bg-surface focus-visible:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-100",
        saved ? "text-accent opacity-100" : "opacity-0")}>
      <Clock size={18} weight={saved ? "fill" : "regular"} aria-hidden="true" />
    </button>
  );
}

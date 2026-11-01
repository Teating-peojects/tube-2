"use client";
import { Play, SpinnerGap, WarningCircle } from "@phosphor-icons/react";
import dynamic from "next/dynamic";
import Image from "next/image";
import { useState } from "react";
import type { Caption } from "@/types";
import { cn } from "@/utils/format";

// Heavy player code (and hls.js inside it) loads only after the user presses play.
const PlayerCore = dynamic(() => import("./PlayerCore"), {
  ssr: false,
  loading: () => (
    <div className="absolute inset-0 grid place-items-center" role="status" aria-label="Loading player">
      <SpinnerGap size={36} className="animate-spin text-accent" />
    </div>
  ),
});

export type VideoPlayerProps = {
  src: string | null;
  poster?: string | null;
  title: string;
  captions?: Caption[];
  onEnded?: () => void;
  onPlay?: () => void;
  onTime?: (seconds: number) => void;
  onPause?: () => void;
  aspectClass?: string;
};

export function VideoPlayer({ src, poster, title, captions = [], onEnded, onPlay, onTime, onPause, aspectClass = "aspect-video" }: VideoPlayerProps) {
  const [started, setStarted] = useState(false);

  return (
    <div className={cn("relative overflow-hidden rounded-card border border-line bg-black", aspectClass)}>
      {!src ? (
        <div role="alert" className="absolute inset-0 grid place-items-center gap-2 p-6 text-center">
          <div className="grid justify-items-center gap-2">
            <WarningCircle size={40} className="text-accent" aria-hidden="true" />
            <p className="font-semibold">Video unavailable</p>
            <p className="text-sm text-fg-2">This video can’t be played right now.</p>
          </div>
        </div>
      ) : started ? (
        <PlayerCore src={src} title={title} captions={captions} onEnded={onEnded} onPlay={onPlay} onTime={onTime} onPause={onPause} />
      ) : (
        <button type="button" onClick={() => setStarted(true)} aria-label={`Play ${title}`}
          className="group absolute inset-0 grid place-items-center bg-gradient-to-br from-[#3a0a14] to-card">
          {poster && <Image src={poster} alt="" fill priority sizes="(min-width:1024px) 70vw, 100vw" className="object-cover" />}
          <span className="relative grid size-16 place-items-center rounded-full bg-accent shadow-[0_8px_30px_rgba(255,0,60,.4)] transition-transform group-hover:scale-105">
            <Play size={28} weight="fill" aria-hidden="true" />
          </span>
        </button>
      )}
    </div>
  );
}

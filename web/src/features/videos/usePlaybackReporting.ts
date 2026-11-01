"use client";
import { useCallback, useEffect, useRef } from "react";
import { useSession } from "@/features/auth/useSession";
import { recordView, saveProgress } from "@/services/playback";

const FLUSH_EVERY_SEC = 15; // the API allows 30 progress calls a minute and caps watchedSec at 120 per call

/**
 * Reports playback to the API: one view per play session, and (signed in) the resume position and
 * watch time every 15 s of real playback, on pause and when the tab is hidden or closed.
 * Seeking does not count as watch time: only small forward steps of the clock are added up.
 */
export function usePlaybackReporting(videoId: string) {
  const { user } = useSession();
  const signedIn = !!user;
  const viewed = useRef(false);
  const lastT = useRef<number | null>(null);
  const pos = useRef(0);
  const watched = useRef(0);

  // The same component instance can be reused when the user moves to another video: start clean for each id.
  useEffect(() => {
    viewed.current = false;
    lastT.current = null;
    pos.current = 0;
    watched.current = 0;
  }, [videoId]);

  const flush = useCallback(() => {
    if (!signedIn || watched.current <= 0) return;
    const w = watched.current;
    watched.current = 0;
    void saveProgress(videoId, pos.current, w);
  }, [signedIn, videoId]);

  const onPlay = useCallback(() => {
    lastT.current = null; // do not count the time between a pause and the next play
    if (viewed.current) return;
    viewed.current = true;
    void recordView(videoId);
  }, [videoId]);

  const onTime = useCallback((t: number) => {
    pos.current = t;
    const prev = lastT.current;
    lastT.current = t;
    if (prev !== null) {
      const d = t - prev;
      if (d > 0 && d < 2) watched.current += d; // timeupdate ticks ~4x per second; bigger jumps are seeks
    }
    if (watched.current >= FLUSH_EVERY_SEC) flush();
  }, [flush]);

  const onPause = useCallback(() => { lastT.current = null; flush(); }, [flush]);

  useEffect(() => {
    const hide = () => { if (document.visibilityState === "hidden") flush(); };
    document.addEventListener("visibilitychange", hide);
    window.addEventListener("pagehide", flush);
    return () => {
      document.removeEventListener("visibilitychange", hide);
      window.removeEventListener("pagehide", flush);
      flush();
    };
  }, [flush]);

  return { onPlay, onTime, onPause };
}

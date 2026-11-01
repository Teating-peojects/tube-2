"use client";
import {
  ArrowsIn, ArrowsOut, Gear, Pause, PictureInPicture, Play, SpeakerHigh, SpeakerSlash, SpinnerGap, Subtitles, WarningCircle,
} from "@phosphor-icons/react";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type HlsType from "hls.js";
import type { Caption } from "@/types";
import { cn, formatDuration } from "@/utils/format";

type Props = {
  src: string; title: string; captions: Caption[]; onEnded?: () => void;
  /** Playback reporting hooks (views, resume position). All optional. */
  onPlay?: () => void; onTime?: (seconds: number) => void; onPause?: () => void;
};
type Level = { index: number; label: string };
type FsVideo = HTMLVideoElement & { webkitEnterFullscreen?: () => void };

const RATES = [0.5, 0.75, 1, 1.25, 1.5, 2];
const btn = "grid size-11 place-items-center rounded-full transition-colors hover:bg-white/15";

export default function PlayerCore({ src, title, captions, onEnded, onPlay, onTime, onPause }: Props) {
  const vref = useRef<FsVideo>(null);
  const box = useRef<HTMLDivElement>(null);
  const hlsRef = useRef<HlsType | null>(null);

  const [playing, setPlaying] = useState(false);
  const [buffering, setBuffering] = useState(true);
  const [t, setT] = useState(0);
  const [dur, setDur] = useState(0);
  const [vol, setVol] = useState(1);
  const [muted, setMuted] = useState(false);
  const [rate, setRate] = useState(1);
  const [levels, setLevels] = useState<Level[]>([]);
  const [level, setLevel] = useState(-1);
  const [menu, setMenu] = useState(false);
  const [cc, setCc] = useState(false);
  const [fs, setFs] = useState(false);
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);

  // Attach source: native HLS (Safari) / progressive file, otherwise hls.js loaded on demand.
  useEffect(() => {
    const v = vref.current;
    if (!v) return;
    let cancelled = false;
    setError(false);
    setBuffering(true);
    const isHls = /\.m3u8($|\?)/.test(src);

    if (isHls && !v.canPlayType("application/vnd.apple.mpegurl")) {
      import("hls.js")
        .then(({ default: Hls }) => {
          if (cancelled) return;
          if (!Hls.isSupported()) return setError(true);
          const hls = new Hls({ capLevelToPlayerSize: true });
          hlsRef.current = hls;
          hls.loadSource(src);
          hls.attachMedia(v);
          hls.on(Hls.Events.MANIFEST_PARSED, (_e, d) => {
            setLevels(d.levels.map((l, index) => ({ index, label: l.height ? `${l.height}p` : `${Math.round(l.bitrate / 1000)}k` })));
            v.play().catch(() => {});
          });
          hls.on(Hls.Events.ERROR, (_e, d) => { if (d.fatal) setError(true); });
        })
        .catch(() => !cancelled && setError(true));
    } else {
      v.src = src;
      v.play().catch(() => {});
    }
    return () => {
      cancelled = true;
      hlsRef.current?.destroy();
      hlsRef.current = null;
    };
  }, [src, attempt]);

  useEffect(() => {
    const onFs = () => setFs(Boolean(document.fullscreenElement));
    document.addEventListener("fullscreenchange", onFs);
    return () => document.removeEventListener("fullscreenchange", onFs);
  }, []);

  const toggle = () => { const v = vref.current; if (v) void (v.paused ? v.play().catch(() => {}) : v.pause()); };
  const seekBy = (d: number) => { const v = vref.current; if (v) v.currentTime = Math.max(0, Math.min(v.duration || 0, v.currentTime + d)); };
  const setVolume = (n: number) => { const v = vref.current; if (!v) return; v.volume = Math.max(0, Math.min(1, n)); v.muted = n === 0; };
  const toggleMute = () => { const v = vref.current; if (v) v.muted = !v.muted; };
  const changeRate = (r: number) => { const v = vref.current; if (v) v.playbackRate = r; setRate(r); };
  const changeLevel = (i: number) => { if (hlsRef.current) hlsRef.current.currentLevel = i; setLevel(i); };
  const toggleCc = () => {
    const v = vref.current; if (!v) return;
    const on = !cc;
    for (const tr of Array.from(v.textTracks)) tr.mode = on ? "showing" : "disabled";
    setCc(on);
  };
  const toggleFs = () => {
    const el = box.current, v = vref.current;
    if (document.fullscreenElement) void document.exitFullscreen();
    else if (el?.requestFullscreen) void el.requestFullscreen();
    else v?.webkitEnterFullscreen?.(); // iOS Safari
  };
  const togglePip = () => {
    const v = vref.current; if (!v) return;
    if (document.pictureInPictureElement) void document.exitPictureInPicture().catch(() => {});
    else void v.requestPictureInPicture?.().catch(() => {});
  };

  function onKey(e: KeyboardEvent<HTMLDivElement>) {
    if (e.target !== e.currentTarget) return; // don't hijack keys from inner controls
    const map: Record<string, () => void> = {
      " ": toggle, k: toggle, ArrowLeft: () => seekBy(-5), ArrowRight: () => seekBy(5),
      ArrowUp: () => setVolume(vol + 0.1), ArrowDown: () => setVolume(vol - 0.1),
      m: toggleMute, f: toggleFs, c: toggleCc,
    };
    const fn = map[e.key];
    if (fn) { e.preventDefault(); fn(); }
  }

  const pip = typeof document !== "undefined" && document.pictureInPictureEnabled;

  return (
    <div ref={box} tabIndex={0} onKeyDown={onKey} role="group" aria-label={`${title} video player`}
      className="group/p absolute inset-0 bg-black focus-visible:outline-offset-[-2px]">
      <video ref={vref} className="size-full" playsInline preload="metadata" aria-label={title}
        crossOrigin={captions.length ? "anonymous" : undefined}
        onClick={toggle}
        onPlay={() => { setPlaying(true); onPlay?.(); }} onPause={() => { setPlaying(false); onPause?.(); }}
        onWaiting={() => setBuffering(true)} onPlaying={() => setBuffering(false)} onCanPlay={() => setBuffering(false)}
        onTimeUpdate={(e) => { setT(e.currentTarget.currentTime); onTime?.(e.currentTarget.currentTime); }}
        onDurationChange={(e) => setDur(e.currentTarget.duration || 0)}
        onVolumeChange={(e) => { setVol(e.currentTarget.volume); setMuted(e.currentTarget.muted); }}
        onEnded={onEnded}
        onError={() => setError(true)}>
        {captions.map((c) => <track key={c.lang} kind="captions" src={c.url} srcLang={c.lang} label={c.label} />)}
      </video>

      {buffering && !error && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center" role="status" aria-label="Buffering">
          <SpinnerGap size={40} className="animate-spin text-accent" />
        </div>
      )}

      {error && (
        <div role="alert" className="absolute inset-0 grid place-items-center bg-black/80 p-6 text-center">
          <div className="grid justify-items-center gap-3">
            <WarningCircle size={40} className="text-accent" aria-hidden="true" />
            <p className="font-semibold">This video couldn’t be played</p>
            <button type="button" onClick={() => setAttempt((a) => a + 1)} className="h-10 rounded-full bg-accent px-5 text-sm font-semibold hover:bg-accent-hover">Retry</button>
          </div>
        </div>
      )}

      <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 to-transparent px-2 pb-1 pt-8 opacity-100 transition-opacity md:opacity-0 md:group-hover/p:opacity-100 md:group-focus-within/p:opacity-100">
        <input type="range" min={0} max={dur || 0} step={0.1} value={Math.min(t, dur || 0)}
          onChange={(e) => { const v = vref.current; if (v) v.currentTime = Number(e.target.value); }}
          aria-label="Seek" aria-valuetext={`${formatDuration(Math.floor(t))} of ${formatDuration(Math.floor(dur))}`}
          className="h-4 w-full cursor-pointer accent-accent" />
        <div className="flex items-center gap-0.5 text-white">
          <button type="button" onClick={toggle} aria-label={playing ? "Pause" : "Play"} className={btn}>
            {playing ? <Pause size={22} weight="fill" /> : <Play size={22} weight="fill" />}
          </button>
          <button type="button" onClick={toggleMute} aria-label={muted || vol === 0 ? "Unmute" : "Mute"} className={btn}>
            {muted || vol === 0 ? <SpeakerSlash size={22} /> : <SpeakerHigh size={22} />}
          </button>
          <input type="range" min={0} max={1} step={0.05} value={muted ? 0 : vol} onChange={(e) => setVolume(Number(e.target.value))}
            aria-label="Volume" className="hidden h-4 w-20 cursor-pointer accent-accent sm:block" />
          <span className="ml-2 text-xs tabular-nums text-fg-2">{formatDuration(Math.floor(t))} / {formatDuration(Math.floor(dur))}</span>
          <span className="flex-1" />
          {captions.length > 0 && (
            <button type="button" onClick={toggleCc} aria-pressed={cc} aria-label="Captions" className={cn(btn, cc && "text-accent")}><Subtitles size={22} /></button>
          )}
          <div className="relative">
            <button type="button" onClick={() => setMenu((m) => !m)} aria-expanded={menu} aria-haspopup="true" aria-label="Settings" className={btn}><Gear size={22} /></button>
            {menu && (
              <div className="absolute bottom-12 right-0 z-10 w-44 rounded-xl border border-line bg-card p-2 text-sm shadow-soft">
                <p className="px-2 py-1 text-[11px] font-semibold tracking-wider text-fg-3">SPEED</p>
                <div className="grid grid-cols-3 gap-1">
                  {RATES.map((r) => (
                    <button key={r} type="button" aria-pressed={rate === r} onClick={() => changeRate(r)}
                      className={cn("h-8 rounded-md text-xs hover:bg-surface", rate === r && "bg-surface text-accent")}>{r}×</button>
                  ))}
                </div>
                {levels.length > 0 && (
                  <>
                    <p className="px-2 pb-1 pt-3 text-[11px] font-semibold tracking-wider text-fg-3">QUALITY</p>
                    <div className="grid max-h-36 gap-0.5 overflow-y-auto">
                      {[{ index: -1, label: "Auto" }, ...[...levels].reverse()].map((l) => (
                        <button key={l.index} type="button" aria-pressed={level === l.index} onClick={() => changeLevel(l.index)}
                          className={cn("h-8 rounded-md px-2 text-left text-xs hover:bg-surface", level === l.index && "bg-surface text-accent")}>{l.label}</button>
                      ))}
                    </div>
                  </>
                )}
              </div>
            )}
          </div>
          {pip && <button type="button" onClick={togglePip} aria-label="Picture in picture" className={cn(btn, "hidden sm:grid")}><PictureInPicture size={22} /></button>}
          <button type="button" onClick={toggleFs} aria-label={fs ? "Exit fullscreen" : "Fullscreen"} className={btn}>
            {fs ? <ArrowsIn size={22} /> : <ArrowsOut size={22} />}
          </button>
        </div>
      </div>
    </div>
  );
}

"use client";
import { UploadSimple } from "@phosphor-icons/react";
import { useRef, useState } from "react";
import { Button } from "@/components/ui/Button";
import { uploadLimits, validateVideoFile } from "@/config/upload";
import { cn, formatBytes } from "@/utils/format";

export function DropZone({ onFile, notice }: { onFile: (f: File) => void; notice?: string | null }) {
  const input = useRef<HTMLInputElement>(null);
  const [drag, setDrag] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  function pick(f?: File | null) {
    if (!f) return;
    const e = validateVideoFile(f);
    setErr(e);
    if (!e) onFile(f);
  }

  return (
    <div onDragOver={(e) => { e.preventDefault(); setDrag(true); }} onDragLeave={() => setDrag(false)}
      onDrop={(e) => { e.preventDefault(); setDrag(false); pick(e.dataTransfer.files[0]); }}
      className={cn("grid place-items-center gap-4 rounded-card border-2 border-dashed px-6 py-16 text-center transition-colors sm:py-24",
        drag ? "border-accent bg-accent/5" : "border-line bg-card")}>
      <span aria-hidden="true" className="grid size-16 place-items-center rounded-full border border-line bg-surface text-fg-2"><UploadSimple size={28} /></span>
      <div>
        <p className="text-lg font-bold">Drag and drop a video to upload</p>
        <p className="mt-1 text-sm text-fg-2">Your video stays private until you publish it.</p>
      </div>
      <input ref={input} type="file" className="sr-only" tabIndex={-1} aria-hidden="true" accept={uploadLimits.extensions.join(",") + ",video/*"} onChange={(e) => { pick(e.target.files?.[0]); e.target.value = ""; }} />
      <Button onClick={() => input.current?.click()}>Select file</Button>
      <p className="text-xs text-fg-3">{uploadLimits.extensions.join(" ")} • up to {formatBytes(uploadLimits.maxBytes)} • Shorts (60 s or less): {formatBytes(uploadLimits.maxShortBytes)}</p>
      {(err || notice) && <p role="alert" className="text-sm text-accent">{err ?? notice}</p>}
    </div>
  );
}

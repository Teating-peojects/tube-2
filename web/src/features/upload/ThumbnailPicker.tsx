"use client";
import { ImageSquare } from "@phosphor-icons/react";
import Image from "next/image";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/Button";
import { validateImage } from "@/config/upload";
import { cn } from "@/utils/format";

type Props = {
  candidates: string[]; selected: string | null; onSelect: (url: string | null) => void;
  customFile: File | null; onCustomFile: (f: File | null) => void;
};

export function ThumbnailPicker({ candidates, selected, onSelect, customFile, onCustomFile }: Props) {
  const input = useRef<HTMLInputElement>(null);
  const [err, setErr] = useState<string | null>(null);
  const preview = useMemo(() => (customFile ? URL.createObjectURL(customFile) : null), [customFile]);
  useEffect(() => () => { if (preview) URL.revokeObjectURL(preview); }, [preview]);

  function pick(f?: File | null) {
    if (!f) return;
    const e = validateImage(f);
    setErr(e);
    if (!e) { onCustomFile(f); onSelect(null); }
  }

  return (
    <fieldset className="space-y-3">
      <legend className="text-sm font-medium">Thumbnail</legend>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-2">
        {candidates.map((u, i) => (
          <label key={u} className={cn("relative block aspect-video cursor-pointer overflow-hidden rounded-xl border-2", selected === u && !customFile ? "border-accent" : "border-line")}>
            <input type="radio" name="thumb" className="sr-only" checked={selected === u && !customFile} onChange={() => { onCustomFile(null); onSelect(u); }} />
            <Image src={u} alt={`Suggested thumbnail ${i + 1}`} fill sizes="160px" loading="lazy" className="object-cover" />
          </label>
        ))}
        {preview && (
          <div className="relative aspect-video overflow-hidden rounded-xl border-2 border-accent">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={preview} alt="Your custom thumbnail" className="size-full object-cover" />
          </div>
        )}
        {candidates.length === 0 && !preview && (
          <div className="col-span-full grid aspect-video place-items-center rounded-xl border border-dashed border-line bg-card text-center text-xs text-fg-3">
            <span className="flex items-center gap-2 px-4"><ImageSquare size={18} aria-hidden="true" />Suggestions appear after processing</span>
          </div>
        )}
      </div>
      <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" className="sr-only" tabIndex={-1} aria-hidden="true" onChange={(e) => { pick(e.target.files?.[0]); e.target.value = ""; }} />
      <div className="flex items-center gap-2">
        <Button variant="secondary" className="h-10" onClick={() => input.current?.click()}>Upload custom</Button>
        {customFile && <Button variant="ghost" className="h-10" onClick={() => onCustomFile(null)}>Remove</Button>}
      </div>
      {err && <p role="alert" className="text-xs text-accent">{err}</p>}
    </fieldset>
  );
}

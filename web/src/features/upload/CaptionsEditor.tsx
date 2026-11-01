"use client";
import { X } from "@phosphor-icons/react";
import { useRef, useState } from "react";
import { Button } from "@/components/ui/Button";
import { languages, validateCaption } from "@/config/upload";

export type CaptionDraft = { lang: string; file: File };

export function CaptionsEditor({ value, onChange }: { value: CaptionDraft[]; onChange: (v: CaptionDraft[]) => void }) {
  const input = useRef<HTMLInputElement>(null);
  const [lang, setLang] = useState("en");
  const [err, setErr] = useState<string | null>(null);

  function pick(f?: File | null) {
    if (!f) return;
    const e = validateCaption(f);
    setErr(e);
    if (e) return;
    onChange([...value.filter((c) => c.lang !== lang), { lang, file: f }]); // one file per language
  }
  const label = (code: string) => languages.find((l) => l.code === code)?.label ?? code;

  return (
    <fieldset className="space-y-3">
      <legend className="text-sm font-medium">Subtitles / captions</legend>
      {value.length > 0 && (
        <ul className="space-y-1.5">
          {value.map((c) => (
            <li key={c.lang} className="flex items-center justify-between gap-2 rounded-xl border border-line bg-card px-3 py-2 text-sm">
              <span className="min-w-0 truncate"><strong className="font-semibold">{label(c.lang)}</strong> <span className="text-fg-2">• {c.file.name}</span></span>
              <button type="button" onClick={() => onChange(value.filter((x) => x.lang !== c.lang))} aria-label={`Remove ${label(c.lang)} captions`} className="grid size-8 shrink-0 place-items-center rounded-full hover:bg-surface"><X size={14} /></button>
            </li>
          ))}
        </ul>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <label className="sr-only" htmlFor="cap-lang">Caption language</label>
        <select id="cap-lang" value={lang} onChange={(e) => setLang(e.target.value)} className="h-10 rounded-lg border border-line bg-card px-2.5 text-sm">
          {languages.map((l) => <option key={l.code} value={l.code}>{l.label}</option>)}
        </select>
        <input ref={input} type="file" accept=".vtt,.srt" className="sr-only" tabIndex={-1} aria-hidden="true" onChange={(e) => { pick(e.target.files?.[0]); e.target.value = ""; }} />
        <Button variant="secondary" className="h-10" onClick={() => input.current?.click()}>Add file (.vtt / .srt)</Button>
      </div>
      {err && <p role="alert" className="text-xs text-accent">{err}</p>}
    </fieldset>
  );
}

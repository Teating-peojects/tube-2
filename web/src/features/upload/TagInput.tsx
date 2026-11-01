"use client";
import { X } from "@phosphor-icons/react";
import { useId, useState, type KeyboardEvent } from "react";

const MAX_TAGS = 15;
const clean = (s: string) => s.replace(/^#+/, "").trim().toLowerCase().slice(0, 30);

export function TagInput({ value, onChange, error }: { value: string[]; onChange: (v: string[]) => void; error?: string }) {
  const [text, setText] = useState("");
  const id = useId();

  function add(raw: string) {
    const t = clean(raw);
    if (t && !value.includes(t) && value.length < MAX_TAGS) onChange([...value, t]);
    setText("");
  }
  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter" || e.key === ",") { e.preventDefault(); add(text); }
    else if (e.key === "Backspace" && !text && value.length) onChange(value.slice(0, -1));
  }

  return (
    <div>
      <label htmlFor={id} className="mb-1.5 block text-sm font-medium">Tags</label>
      <div className="flex min-h-12 flex-wrap items-center gap-2 rounded-xl border border-line bg-card px-3 py-2 focus-within:border-white">
        {value.map((t) => (
          <span key={t} className="inline-flex h-8 items-center gap-1 rounded-full bg-surface pl-3 pr-1 text-xs">
            {t}
            <button type="button" onClick={() => onChange(value.filter((x) => x !== t))} aria-label={`Remove tag ${t}`} className="grid size-7 place-items-center rounded-full hover:bg-line"><X size={12} /></button>
          </span>
        ))}
        <input id={id} value={text} onChange={(e) => setText(e.target.value)} onKeyDown={onKey} onBlur={() => add(text)} maxLength={30}
          disabled={value.length >= MAX_TAGS} placeholder={value.length ? "" : "Type a tag and press Enter"} aria-describedby={`${id}-hint`}
          className="min-w-[8rem] flex-1 bg-transparent text-sm outline-none placeholder:text-fg-3" />
      </div>
      <p id={`${id}-hint`} className="mt-1.5 text-xs text-fg-3">{value.length}/{MAX_TAGS} tags • press Enter or comma to add</p>
      {error && <p role="alert" className="mt-1 text-xs text-accent">{error}</p>}
    </div>
  );
}

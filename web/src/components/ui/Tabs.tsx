"use client";
import { useRef, type KeyboardEvent } from "react";
import { cn } from "@/utils/format";

type Props<T extends string> = {
  tabs: { id: T; label: string }[];
  value: T;
  onChange: (id: T) => void;
  label: string;
  idPrefix: string; // panel must use id=`${idPrefix}-panel` and aria-labelledby=`${idPrefix}-tab-${value}`
};

export function Tabs<T extends string>({ tabs, value, onChange, label, idPrefix }: Props<T>) {
  const refs = useRef<Record<string, HTMLButtonElement | null>>({});

  function onKey(e: KeyboardEvent<HTMLDivElement>) {
    const i = tabs.findIndex((t) => t.id === value);
    let n = i;
    if (e.key === "ArrowRight") n = (i + 1) % tabs.length;
    else if (e.key === "ArrowLeft") n = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === "Home") n = 0;
    else if (e.key === "End") n = tabs.length - 1;
    else return;
    e.preventDefault();
    const t = tabs[n];
    if (t) { onChange(t.id); refs.current[t.id]?.focus(); }
  }

  return (
    <div role="tablist" aria-label={label} onKeyDown={onKey} className="flex gap-1 overflow-x-auto border-b border-line">
      {tabs.map((t) => {
        const sel = t.id === value;
        return (
          <button key={t.id} type="button" role="tab" id={`${idPrefix}-tab-${t.id}`} aria-selected={sel} aria-controls={`${idPrefix}-panel`}
            tabIndex={sel ? 0 : -1} ref={(el) => { refs.current[t.id] = el; }} onClick={() => onChange(t.id)}
            className={cn("relative h-11 shrink-0 px-4 text-sm font-semibold transition-colors",
              sel ? "text-white after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:rounded-full after:bg-accent" : "text-fg-2 hover:text-white")}>
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

"use client";
import { useId } from "react";
import { cn } from "@/utils/format";

export function Toggle({ label, description, checked, onChange, disabled }: { label: string; description?: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  const id = useId();
  return (
    <div className="flex items-start justify-between gap-4 py-4">
      <div className="min-w-0">
        <label htmlFor={id} className="text-sm font-medium">{label}</label>
        {description && <p id={`${id}-d`} className="mt-0.5 text-xs text-fg-2">{description}</p>}
      </div>
      <button id={id} type="button" role="switch" aria-checked={checked} aria-describedby={description ? `${id}-d` : undefined} disabled={disabled} onClick={() => onChange(!checked)}
        className={cn("relative h-7 w-12 shrink-0 rounded-full border transition-colors before:absolute before:-inset-2 before:content-[''] disabled:opacity-60", checked ? "border-accent bg-accent" : "border-line bg-surface")}>
        <span aria-hidden="true" className={cn("absolute top-0.5 size-5 rounded-full bg-white transition-transform", checked ? "translate-x-[1.375rem]" : "translate-x-0.5")} />
      </button>
    </div>
  );
}

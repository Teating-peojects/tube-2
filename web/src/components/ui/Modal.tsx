"use client";
import { X } from "@phosphor-icons/react";
import { useEffect, useId, useRef, type ReactNode } from "react";
import { cn } from "@/utils/format";

/**
 * Accessible modal built on the native <dialog>: focus trap, Escape, inert background and focus restore
 * come from the browser. Outside click closes it. On phones it becomes a bottom sheet.
 */
export function Modal({ open, onClose, title, children, size = "md" }: { open: boolean; onClose: () => void; title: string; children: ReactNode; size?: "sm" | "md" }) {
  const ref = useRef<HTMLDialogElement>(null);
  const id = useId();

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    else if (!open && d.open) d.close();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const el = document.documentElement;
    const prev = el.style.overflow;
    el.style.overflow = "hidden"; // lock page scroll
    return () => { el.style.overflow = prev; };
  }, [open]);

  return (
    <dialog ref={ref} aria-labelledby={id} onClose={onClose} onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
      className={cn("m-auto w-[calc(100%-1.5rem)] rounded-2xl border border-line bg-card p-0 text-white shadow-soft backdrop:bg-black/70 max-sm:mb-0 max-sm:mt-auto max-sm:w-full max-sm:max-w-none max-sm:rounded-b-none",
        size === "sm" ? "max-w-sm" : "max-w-md")}>
      <div className="max-h-[85dvh] overflow-y-auto p-5 pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:p-6">
        <div className="mb-4 flex items-start justify-between gap-4">
          <h2 id={id} className="text-lg font-bold">{title}</h2>
          <button type="button" onClick={onClose} aria-label="Close" className="-mr-2 -mt-1 grid size-10 place-items-center rounded-full text-fg-2 hover:bg-surface hover:text-white"><X size={18} /></button>
        </div>
        {children}
      </div>
    </dialog>
  );
}

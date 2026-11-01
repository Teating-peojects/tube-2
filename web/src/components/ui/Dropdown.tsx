"use client";
import { DotsThreeVertical } from "@phosphor-icons/react";
import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "@/utils/format";

export type MenuItem = { label: string; icon?: ReactNode; onSelect: () => void; danger?: boolean };

/** Menu-button pattern: arrow keys, Home/End, Escape (returns focus), outside click. Fixed-positioned so scroll containers never clip it. */
export function Dropdown({ label, items, className }: { label: string; items: MenuItem[]; className?: string }) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; right: number } | null>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const id = useId();

  const close = useCallback((refocus = false) => { setOpen(false); if (refocus) trigger.current?.focus(); }, []);

  function toggle() {
    if (open) return close();
    const r = trigger.current!.getBoundingClientRect();
    setPos({ top: r.bottom + 4, right: Math.max(8, window.innerWidth - r.right) });
    setOpen(true);
  }

  useEffect(() => {
    if (!open) return;
    menu.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus();
    const down = (e: MouseEvent) => { if (!menu.current?.contains(e.target as Node) && !trigger.current?.contains(e.target as Node)) close(); };
    const dismiss = () => close();
    document.addEventListener("mousedown", down);
    window.addEventListener("scroll", dismiss, true);
    window.addEventListener("resize", dismiss);
    return () => { document.removeEventListener("mousedown", down); window.removeEventListener("scroll", dismiss, true); window.removeEventListener("resize", dismiss); };
  }, [open, close]);

  function onKey(e: KeyboardEvent<HTMLDivElement>) {
    const els = [...(menu.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])];
    const i = els.indexOf(document.activeElement as HTMLElement);
    const go = (n: number) => { e.preventDefault(); els[(n + els.length) % els.length]?.focus(); };
    if (e.key === "ArrowDown") go(i + 1);
    else if (e.key === "ArrowUp") go(i - 1);
    else if (e.key === "Home") go(0);
    else if (e.key === "End") go(els.length - 1);
    else if (e.key === "Escape") { e.preventDefault(); close(true); }
    else if (e.key === "Tab") close();
  }

  return (
    <>
      <button ref={trigger} type="button" onClick={toggle} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? id : undefined} aria-label={label}
        className={cn("grid size-9 shrink-0 place-items-center rounded-full text-fg-2 transition-colors hover:bg-surface hover:text-white", className)}>
        <DotsThreeVertical size={20} weight="bold" aria-hidden="true" />
      </button>
      {open && pos && (
        <div ref={menu} id={id} role="menu" aria-label={label} onKeyDown={onKey} style={{ top: pos.top, right: pos.right }}
          className="fixed z-50 min-w-52 rounded-xl border border-line bg-card p-1.5 shadow-soft">
          {items.map((it) => (
            <button key={it.label} type="button" role="menuitem" tabIndex={-1} onClick={() => { close(); it.onSelect(); }}
              className={cn("flex min-h-11 w-full items-center gap-3 rounded-lg px-3 text-left text-sm transition-colors hover:bg-surface focus:bg-surface focus:outline-none", it.danger && "text-accent")}>
              {it.icon}{it.label}
            </button>
          ))}
        </div>
      )}
    </>
  );
}

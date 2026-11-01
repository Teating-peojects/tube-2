"use client";
import { ClockCounterClockwise, MagnifyingGlass, X } from "@phosphor-icons/react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useId, useState, type FocusEvent, type FormEvent, type KeyboardEvent } from "react";
import { useDebounce } from "@/hooks/useDebounce";
import { getSuggestions } from "@/services/search";
import { useRecentSearches } from "@/store/recent";

type Option = { label: string; kind: "suggestion" | "recent" };

export function SearchBox({ autoFocus = false }: { autoFocus?: boolean }) {
  const router = useRouter();
  const pathname = usePathname();
  const sp = useSearchParams();
  const urlQ = pathname === "/search" ? (sp.get("q") ?? "") : "";

  const [value, setValue] = useState(urlQ);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const listId = useId();
  const recent = useRecentSearches();

  useEffect(() => setValue(urlQ), [urlQ]);

  const typing = value.trim().length >= 2;
  const debounced = useDebounce(value.trim(), 250); // no request per keystroke
  const sugg = useQuery({
    queryKey: ["suggest", debounced],
    queryFn: () => getSuggestions(debounced),
    enabled: open && debounced.length >= 2,
    staleTime: 5 * 60_000,
    placeholderData: keepPreviousData,
  });

  const options: Option[] = typing
    ? (sugg.data ?? []).map((label) => ({ label, kind: "suggestion" }))
    : recent.items.map((label) => ({ label, kind: "recent" }));
  const expanded = open && options.length > 0;

  function go(term: string) {
    const t = term.trim().slice(0, 100);
    if (!t) return;
    recent.add(t);
    setValue(t);
    setOpen(false);
    setActive(-1);
    router.push(`/search?q=${encodeURIComponent(t)}`);
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    go(active >= 0 ? (options[active]?.label ?? value) : value);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    const n = options.length;
    if (e.key === "ArrowDown" && n) { e.preventDefault(); setOpen(true); setActive((a) => (a + 1) % n); }
    else if (e.key === "ArrowUp" && n) { e.preventDefault(); setOpen(true); setActive((a) => (a <= 0 ? n - 1 : a - 1)); }
    else if (e.key === "Escape") { setOpen(false); setActive(-1); }
    else if (e.key === "Delete" && active >= 0 && options[active]?.kind === "recent") { e.preventDefault(); recent.remove(options[active]!.label); setActive(-1); }
  }

  function onBlur(e: FocusEvent<HTMLDivElement>) {
    if (!e.currentTarget.contains(e.relatedTarget)) { setOpen(false); setActive(-1); }
  }

  return (
    <div className="relative w-full" onBlur={onBlur}>
      <form role="search" onSubmit={onSubmit} className="flex h-11 overflow-hidden rounded-full border border-line bg-card focus-within:border-[#3a3a40]">
        <label htmlFor={`${listId}-input`} className="sr-only">Search REDXTUBE</label>
        <input id={`${listId}-input`} type="text" role="combobox" autoComplete="off" enterKeyHint="search" maxLength={100} autoFocus={autoFocus}
          aria-expanded={expanded} aria-controls={listId} aria-autocomplete="list"
          aria-activedescendant={active >= 0 ? `${listId}-${active}` : undefined}
          value={value} placeholder="Search videos, creators, playlists"
          onChange={(e) => { setValue(e.target.value); setOpen(true); setActive(-1); }}
          onFocus={() => setOpen(true)} onKeyDown={onKeyDown}
          className="min-w-0 flex-1 bg-transparent px-5 text-sm outline-none placeholder:text-fg-3 focus-visible:outline-none" />
        <button type="submit" aria-label="Search" className="grid w-14 place-items-center border-l border-line bg-surface hover:bg-line"><MagnifyingGlass size={18} /></button>
      </form>

      {expanded && (
        <div className="absolute inset-x-0 top-full z-40 mt-2 overflow-hidden rounded-xl border border-line bg-card shadow-soft">
          {!typing && (
            <div className="flex items-center justify-between px-4 pb-1 pt-3 text-[11px] font-semibold tracking-wider text-fg-3">
              RECENT SEARCHES
              <button type="button" onMouseDown={(e) => e.preventDefault()} onClick={() => recent.clear()} className="rounded px-1 text-xs font-medium normal-case tracking-normal text-fg-2 hover:text-white">Clear</button>
            </div>
          )}
          <ul id={listId} role="listbox" aria-label={typing ? "Search suggestions" : "Recent searches"} className="py-1">
            {options.map((o, i) => (
              <li key={`${o.kind}-${o.label}`} role="presentation" className="flex items-center">
                <div id={`${listId}-${i}`} role="option" aria-selected={i === active}
                  onMouseDown={(e) => e.preventDefault()} onMouseEnter={() => setActive(i)} onClick={() => go(o.label)}
                  className={`flex min-h-11 flex-1 cursor-pointer items-center gap-3 px-4 text-sm ${i === active ? "bg-surface" : ""}`}>
                  {o.kind === "recent" ? <ClockCounterClockwise size={16} className="text-fg-3" aria-hidden="true" /> : <MagnifyingGlass size={16} className="text-fg-3" aria-hidden="true" />}
                  <span className="truncate">{o.label}</span>
                </div>
                {o.kind === "recent" && (
                  <button type="button" tabIndex={-1} aria-label={`Remove ${o.label} from recent searches`}
                    onMouseDown={(e) => e.preventDefault()} onClick={() => recent.remove(o.label)}
                    className="mr-2 grid size-9 place-items-center rounded-full text-fg-3 hover:bg-surface hover:text-white"><X size={14} /></button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
      <p className="sr-only" role="status" aria-live="polite">{expanded && typing ? `${options.length} suggestions available` : ""}</p>
    </div>
  );
}

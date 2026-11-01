"use client";
import { DATES, DURATIONS, SORTS, TYPES } from "@/lib/search-params";
import type { SearchParams } from "@/types";
import { cn } from "@/utils/format";

type Change = <K extends keyof Omit<SearchParams, "q">>(key: K, value: SearchParams[K]) => void;

const typeLabels = { all: "All", videos: "Videos", channels: "Channels", playlists: "Playlists", live: "Live" } as const;
const sortLabels = { relevance: "Relevance", latest: "Latest", views: "Most viewed" } as const;
const durationLabels = { any: "Any", short: "Short (< 4 min)", medium: "Medium (4–20 min)", long: "Long (> 20 min)" } as const;
const dateLabels = { any: "Any time", today: "Today", week: "This week", month: "This month", year: "This year" } as const;

function Select<T extends string>({ label, value, options, labels, onChange }: { label: string; value: T; options: readonly T[]; labels: Record<T, string>; onChange: (v: T) => void }) {
  return (
    <label className="flex items-center gap-2 text-sm text-fg-2">
      {label}
      <select value={value} onChange={(e) => onChange(e.target.value as T)} className="h-10 rounded-lg border border-line bg-card px-2.5 text-sm text-white">
        {options.map((o) => <option key={o} value={o}>{labels[o]}</option>)}
      </select>
    </label>
  );
}

export function SearchFilters({ params, onChange }: { params: SearchParams; onChange: Change }) {
  return (
    <div className="space-y-3">
      <div role="group" aria-label="Content type" className="-mx-3 flex gap-2 overflow-x-auto px-3 md:mx-0 md:px-0">
        {TYPES.map((t) => (
          <button key={t} type="button" aria-pressed={params.type === t} onClick={() => onChange("type", t)}
            className={cn("h-9 shrink-0 rounded-full border px-4 text-[13px] font-medium transition-colors",
              params.type === t ? "border-white bg-white text-bg" : "border-line bg-card text-fg-2 hover:border-[#3a3a40] hover:text-white")}>
            {typeLabels[t]}
          </button>
        ))}
      </div>
      <div className="flex flex-wrap gap-x-5 gap-y-2">
        <Select label="Sort" value={params.sort} options={SORTS} labels={sortLabels} onChange={(v) => onChange("sort", v)} />
        <Select label="Duration" value={params.duration} options={DURATIONS} labels={durationLabels} onChange={(v) => onChange("duration", v)} />
        <Select label="Uploaded" value={params.date} options={DATES} labels={dateLabels} onChange={(v) => onChange("date", v)} />
      </div>
    </div>
  );
}

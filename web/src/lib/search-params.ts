import type { SearchDate, SearchDuration, SearchParams, SearchSort, SearchType } from "@/types";

export const SORTS = ["relevance", "latest", "views"] as const satisfies readonly SearchSort[];
export const TYPES = ["all", "videos", "channels", "playlists", "live"] as const satisfies readonly SearchType[];
export const DURATIONS = ["any", "short", "medium", "long"] as const satisfies readonly SearchDuration[];
export const DATES = ["any", "today", "week", "month", "year"] as const satisfies readonly SearchDate[];

export const DEFAULTS: Omit<SearchParams, "q"> = { sort: "relevance", type: "all", duration: "any", date: "any" };

// URL input is untrusted: every value is checked against an allow-list.
function pick<T extends string>(v: string | null, allowed: readonly T[], fallback: T): T {
  return (allowed as readonly string[]).includes(v ?? "") ? (v as T) : fallback;
}

export function parseSearchParams(sp: { get(key: string): string | null }): SearchParams {
  return {
    q: (sp.get("q") ?? "").trim().slice(0, 100),
    sort: pick(sp.get("sort"), SORTS, DEFAULTS.sort),
    type: pick(sp.get("type"), TYPES, DEFAULTS.type),
    duration: pick(sp.get("duration"), DURATIONS, DEFAULTS.duration),
    date: pick(sp.get("date"), DATES, DEFAULTS.date),
  };
}

/** Builds a query string, omitting values that equal the default. */
export function toQueryString(p: SearchParams): string {
  const qs = new URLSearchParams();
  if (p.q) qs.set("q", p.q);
  (["sort", "type", "duration", "date"] as const).forEach((k) => { if (p[k] !== DEFAULTS[k]) qs.set(k, p[k]); });
  return qs.toString();
}

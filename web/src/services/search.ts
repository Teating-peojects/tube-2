import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import { toQueryString } from "@/lib/search-params";
import type { SearchPage, SearchParams } from "@/types";
import { api } from "./api";
import { mockSearch, mockSuggest } from "./mock/discover";

export async function searchContent(p: SearchParams, cursor?: string): Promise<SearchPage> {
  if (env.useMock) return mockSearch({ ...p, cursor });
  const qs = new URLSearchParams(toQueryString(p));
  if (cursor) qs.set("cursor", cursor);
  return api<SearchPage>(`${endpoints.search}?${qs}`);
}

export async function getSuggestions(q: string): Promise<string[]> {
  if (env.useMock) return mockSuggest(q);
  const res = await api<{ suggestions: string[] }>(`${endpoints.searchSuggestions}?q=${encodeURIComponent(q)}`);
  return res.suggestions;
}

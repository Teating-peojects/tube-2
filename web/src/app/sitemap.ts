import type { MetadataRoute } from "next";
import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import { site } from "@/config/site";
import { api } from "@/services/api";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const pages = ["", "/explore", "/trending", "/live", "/shorts", "/terms", "/privacy", "/community-guidelines", "/dmca"];
  const entries: MetadataRoute.Sitemap = pages.map((p) => ({ url: `${site.url}${p}`, changeFrequency: p === "" ? "daily" : "weekly" }));

  // Public videos come from the backend (only public, non-restricted ones should be returned).
  if (!env.useMock && env.apiUrl) {
    try {
      const videos = await api<{ id: string; updatedAt?: string }[]>(`${endpoints.sitemapVideos}?limit=5000`);
      for (const v of videos) entries.push({ url: `${site.url}/watch/${encodeURIComponent(v.id)}`, lastModified: v.updatedAt, changeFrequency: "weekly" });
    } catch { /* sitemap still works without videos */ }
  }
  return entries;
}

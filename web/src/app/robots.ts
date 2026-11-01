import type { MetadataRoute } from "next";
import { site } from "@/config/site";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [{
      userAgent: "*",
      allow: "/",
      // Private, personalised or low-value pages are never indexed.
      disallow: ["/api/", "/studio", "/settings", "/upload", "/profile", "/history", "/watch-later", "/liked", "/subscriptions", "/notifications", "/search", "/report", "/login", "/register", "/verify", "/forgot-password", "/reset-password", "/age-verification"],
    }],
    sitemap: `${site.url}/sitemap.xml`,
  };
}

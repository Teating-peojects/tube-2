import type { Metadata } from "next";
import { Suspense } from "react";
import { SearchPageSkeleton, SearchView } from "@/features/search/SearchView";

type Props = { searchParams: Promise<{ q?: string | string[] }> };

export async function generateMetadata({ searchParams }: Props): Promise<Metadata> {
  const { q } = await searchParams;
  const term = (typeof q === "string" ? q : "").trim().slice(0, 60);
  // Search result pages are not indexed.
  return { title: term ? `Search: ${term}` : "Search", robots: { index: false, follow: true } };
}

export default function SearchPage() {
  // useSearchParams() in the view requires a Suspense boundary.
  return (
    <Suspense fallback={<SearchPageSkeleton />}>
      <SearchView />
    </Suspense>
  );
}

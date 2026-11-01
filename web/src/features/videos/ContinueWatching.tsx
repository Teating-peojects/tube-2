"use client";
import { useQuery } from "@tanstack/react-query";
import { useSession } from "@/features/auth/useSession";
import { getHistory } from "@/services/library";
import { VideoCard } from "./VideoCard";

export function ContinueWatching() {
  const { status } = useSession();
  const q = useQuery({
    queryKey: ["continue-watching"],
    queryFn: async () => (await getHistory()).items.filter((i) => i.progressSec > 0 && i.progressSec < i.video.durationSec * 0.95).slice(0, 6),
    enabled: status === "authenticated",
    staleTime: 60_000,
  });
  if (!q.data || q.data.length === 0) return null;
  return (
    <section aria-labelledby="cw-h" className="mb-8">
      <h2 id="cw-h" className="mb-3 text-lg font-bold">Continue watching</h2>
      <ul className="-mx-3 flex gap-4 overflow-x-auto px-3 pb-1 md:mx-0 md:px-0">
        {q.data.map((i) => (
          <li key={i.video.id} className="w-64 shrink-0 sm:w-72"><VideoCard video={i.video} progress={i.progressSec / Math.max(1, i.video.durationSec)} /></li>
        ))}
      </ul>
    </section>
  );
}

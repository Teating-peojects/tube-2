"use client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { getOverview } from "@/services/studio";
import type { Period } from "@/types";
import { formatViews } from "@/utils/format";
import { MiniChart, StatCard } from "./charts";
import { PeriodSelect } from "./PeriodSelect";

export function OverviewView() {
  const [period, setPeriod] = useState<Period>("28d");
  const q = useQuery({ queryKey: ["studio", "overview", period], queryFn: () => getOverview(period) });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-lg font-bold">Channel overview</h2>
        <PeriodSelect value={period} onChange={setPeriod} />
      </div>

      {q.isPending && <div className="space-y-4" aria-hidden="true"><div className="grid grid-cols-2 gap-3 lg:grid-cols-4">{Array.from({ length: 4 }, (_, i) => <div key={i} className="skeleton h-24 rounded-card" />)}</div><div className="skeleton h-56 rounded-card" /></div>}
      {q.isError && (
        <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
          <p className="font-semibold">Couldn’t load your overview</p><Button onClick={() => q.refetch()}>Retry</Button>
        </div>
      )}
      {q.data && (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatCard label="Views" value={formatViews(q.data.totals.views)} />
            <StatCard label="Watch time (hours)" value={formatViews(q.data.totals.watchTimeHours)} />
            <StatCard label="Subscribers" value={q.data.totals.subscribers.toLocaleString("en")} note={`${q.data.totals.subscribersDelta >= 0 ? "+" : ""}${q.data.totals.subscribersDelta} this period`} />
            {q.data.totals.revenue !== null && <StatCard label="Revenue" value={`$${q.data.totals.revenue.toLocaleString("en")}`} />}
          </div>
          <section aria-labelledby="views-h" className="rounded-card border border-line bg-card p-5">
            <h3 id="views-h" className="mb-3 text-sm font-semibold">Views</h3>
            <MiniChart data={q.data.views} label="Views" />
          </section>
          <section aria-labelledby="top-h" className="rounded-card border border-line bg-card p-5">
            <h3 id="top-h" className="mb-3 text-sm font-semibold">Top videos</h3>
            <ol className="divide-y divide-line">
              {q.data.topVideos.map((v, i) => (
                <li key={v.id} className="flex items-center gap-4 py-3 text-sm">
                  <span aria-hidden="true" className="w-5 text-fg-3 tabular-nums">{i + 1}</span>
                  <Link href={`/watch/${v.id}`} className="min-w-0 flex-1 truncate hover:underline">{v.title}</Link>
                  <span className="tabular-nums text-fg-2">{formatViews(v.views)} views</span>
                </li>
              ))}
            </ol>
          </section>
        </>
      )}
    </div>
  );
}

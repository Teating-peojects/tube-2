"use client";
import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { getAnalytics } from "@/services/studio";
import type { Period } from "@/types";
import { formatDuration, formatViews } from "@/utils/format";
import { BarList, MiniChart, StatCard } from "./charts";
import { PeriodSelect } from "./PeriodSelect";

function Panel({ title, children }: { title: string; children: ReactNode }) {
  const id = title.toLowerCase().replace(/\W+/g, "-");
  return (
    <section aria-labelledby={id} className="rounded-card border border-line bg-card p-5">
      <h3 id={id} className="mb-4 text-sm font-semibold">{title}</h3>
      {children}
    </section>
  );
}

export function AnalyticsView() {
  const [period, setPeriod] = useState<Period>("28d");
  const q = useQuery({ queryKey: ["studio", "analytics", period], queryFn: () => getAnalytics(period) });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-lg font-bold">Analytics</h2>
        <PeriodSelect value={period} onChange={setPeriod} />
      </div>
      {q.isPending && <div className="grid gap-4 md:grid-cols-2" aria-hidden="true">{Array.from({ length: 4 }, (_, i) => <div key={i} className="skeleton h-56 rounded-card" />)}</div>}
      {q.isError && (
        <div role="alert" className="grid place-items-center gap-3 rounded-card border border-line bg-card py-16 text-center">
          <p className="font-semibold">Couldn’t load analytics</p><Button onClick={() => q.refetch()}>Retry</Button>
        </div>
      )}
      {q.data && (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <Panel title="Views"><MiniChart data={q.data.views} label="Views" /></Panel>
            <Panel title="Watch time (hours)"><MiniChart data={q.data.watchTime} label="Watch time" unit=" h" /></Panel>
          </div>
          <Panel title="Engagement">
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <StatCard label="Likes" value={formatViews(q.data.engagement.likes)} />
              <StatCard label="Comments" value={formatViews(q.data.engagement.comments)} />
              <StatCard label="Shares" value={formatViews(q.data.engagement.shares)} />
              <StatCard label="Avg. view duration" value={formatDuration(q.data.engagement.avgViewDurationSec)} />
            </div>
          </Panel>
          <div className="grid gap-4 md:grid-cols-2">
            <Panel title="Traffic sources"><BarList items={q.data.traffic} /></Panel>
            <Panel title="Top countries"><BarList items={q.data.audience.countries} /></Panel>
            <Panel title="Devices"><BarList items={q.data.audience.devices} /></Panel>
          </div>
        </>
      )}
    </div>
  );
}

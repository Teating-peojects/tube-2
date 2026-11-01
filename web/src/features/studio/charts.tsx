import type { SeriesPoint, Stat } from "@/types";

/** Dependency-free bar chart (SVG) with a screen-reader data table. */
export function MiniChart({ data, label, unit = "" }: { data: SeriesPoint[]; label: string; unit?: string }) {
  if (data.length === 0) return null;
  const W = 600, H = 160, gap = 2;
  const max = Math.max(1, ...data.map((d) => d.value));
  const bw = (W - gap * (data.length - 1)) / data.length;
  const total = data.reduce((a, b) => a + b.value, 0);
  return (
    <figure>
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`${label}: ${total.toLocaleString("en")}${unit} over ${data.length} days`} className="h-40 w-full">
        {data.map((d, i) => {
          const h = (d.value / max) * H;
          return (
            <rect key={d.date} x={i * (bw + gap)} y={H - h} width={bw} height={h} rx={2} className="fill-fg-3/60 transition-colors hover:fill-accent">
              <title>{`${d.date}: ${d.value.toLocaleString("en")}${unit}`}</title>
            </rect>
          );
        })}
      </svg>
      <figcaption className="mt-1 flex justify-between text-[11px] text-fg-3"><span>{data[0]!.date}</span><span>{data[data.length - 1]!.date}</span></figcaption>
      <table className="sr-only">
        <caption>{label}</caption>
        <thead><tr><th>Date</th><th>Value</th></tr></thead>
        <tbody>{data.map((d) => <tr key={d.date}><td>{d.date}</td><td>{d.value}</td></tr>)}</tbody>
      </table>
    </figure>
  );
}

export function BarList({ items, suffix = "%" }: { items: Stat[]; suffix?: string }) {
  const max = Math.max(1, ...items.map((i) => i.value));
  return (
    <ul className="space-y-3">
      {items.map((i) => (
        <li key={i.label}>
          <div className="mb-1 flex justify-between text-sm"><span>{i.label}</span><span className="tabular-nums text-fg-2">{i.value}{suffix}</span></div>
          <div className="h-1.5 overflow-hidden rounded-full bg-surface" aria-hidden="true"><div className="h-full rounded-full bg-fg-2" style={{ width: `${(i.value / max) * 100}%` }} /></div>
        </li>
      ))}
    </ul>
  );
}

export function StatCard({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="rounded-card border border-line bg-card p-4">
      <p className="text-xs text-fg-2">{label}</p>
      <p className="mt-1 text-2xl font-extrabold tabular-nums">{value}</p>
      {note && <p className="mt-0.5 text-xs text-fg-3">{note}</p>}
    </div>
  );
}

import type { Period } from "@/types";

const options: { id: Period; label: string }[] = [{ id: "7d", label: "Last 7 days" }, { id: "28d", label: "Last 28 days" }, { id: "90d", label: "Last 90 days" }];

export function PeriodSelect({ value, onChange }: { value: Period; onChange: (p: Period) => void }) {
  return (
    <label className="flex items-center gap-2 text-sm text-fg-2">
      <span className="sr-only sm:not-sr-only">Period</span>
      <select value={value} onChange={(e) => onChange(e.target.value as Period)} className="h-10 rounded-lg border border-line bg-card px-2.5 text-sm text-white">
        {options.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
      </select>
    </label>
  );
}

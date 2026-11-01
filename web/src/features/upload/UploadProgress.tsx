import { CheckCircle, WarningCircle } from "@phosphor-icons/react/dist/ssr";
import { Button } from "@/components/ui/Button";
import type { Progress } from "@/lib/uploader";
import type { UploadPhase } from "@/types";
import { cn, formatBytes, formatEta } from "@/utils/format";

const steps = ["Preparing", "Uploading", "Processing", "Encoding", "Publishing", "Completed"] as const;
const index: Partial<Record<UploadPhase, number>> = { preparing: 0, uploading: 1, processing: 2, encoding: 3, ready: 4, publishing: 4, completed: 5 };

type Props = {
  phase: UploadPhase; progress: Progress; serverProgress: number; fileName: string; error: string | null;
  failedAtProcessing: boolean; onCancel: () => void; onRetry: () => void; onReset: () => void;
};

export function UploadProgress({ phase, progress, serverProgress, fileName, error, failedAtProcessing, onCancel, onRetry, onReset }: Props) {
  const idx = index[phase] ?? -1;
  const uploading = phase === "uploading" || phase === "preparing";
  const pct = phase === "uploading" ? Math.floor((progress.loaded / Math.max(1, progress.total)) * 100)
    : phase === "processing" || phase === "encoding" ? serverProgress
      : phase === "ready" || phase === "publishing" || phase === "completed" ? 100 : 0;
  const failed = phase === "failed";

  return (
    <section aria-label="Upload status" className="space-y-4 rounded-card border border-line bg-card p-5">
      <div className="min-w-0">
        <p className="truncate text-sm font-semibold" title={fileName}>{fileName}</p>
        <p className="mt-0.5 flex items-center gap-1.5 text-xs text-fg-2" role="status" aria-live="polite">
          {failed ? <><WarningCircle size={14} className="text-accent" aria-hidden="true" />{failedAtProcessing ? "Processing failed" : "Upload failed"}</>
            : phase === "completed" ? <><CheckCircle size={14} weight="fill" aria-hidden="true" />Published</>
              : phase === "ready" ? "Ready to publish"
                : phase === "cancelled" ? "Cancelled" : steps[Math.max(0, idx)] + "…"}
        </p>
      </div>

      <div role="progressbar" aria-label="Progress" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}
        className="h-2 overflow-hidden rounded-full bg-surface">
        <div className={cn("h-full rounded-full transition-[width] duration-300", failed ? "bg-fg-3" : "bg-accent")} style={{ width: `${pct}%` }} />
      </div>
      <div className="flex justify-between text-xs tabular-nums text-fg-2">
        <span>{pct}%</span>
        {phase === "uploading" && <span>{formatBytes(progress.loaded)} / {formatBytes(progress.total)}</span>}
      </div>
      {phase === "uploading" && (
        <p className="text-xs text-fg-3">{progress.speed > 0 ? `${formatBytes(progress.speed)}/s • ${formatEta(progress.eta)}` : "Starting…"}</p>
      )}

      <ol className="grid grid-cols-3 gap-x-2 gap-y-1.5 text-[11px] sm:grid-cols-6 lg:grid-cols-2" aria-label="Steps">
        {steps.map((s, i) => (
          <li key={s} aria-current={i === idx && !failed ? "step" : undefined}
            className={cn("flex items-center gap-1.5", i < idx || phase === "completed" ? "text-white" : i === idx && !failed ? "font-semibold text-white" : "text-fg-3")}>
            <span aria-hidden="true" className={cn("size-1.5 rounded-full", i < idx || phase === "completed" ? "bg-white" : i === idx && !failed ? "bg-accent" : "bg-line")} />{s}
          </li>
        ))}
      </ol>

      {failed && error && <p role="alert" className="rounded-xl border border-accent/40 bg-accent/10 px-3 py-2 text-sm">{error}</p>}
      <div className="flex flex-wrap gap-2">
        {failed && !failedAtProcessing && <Button className="h-10" onClick={onRetry}>Retry upload</Button>}
        {failed && failedAtProcessing && <Button className="h-10" onClick={onReset}>Upload again</Button>}
        {(uploading || phase === "failed") && !failedAtProcessing && <Button variant="secondary" className="h-10" onClick={onCancel}>Cancel</Button>}
      </div>
    </section>
  );
}

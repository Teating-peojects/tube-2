import { useState } from "react";
import { CheckCircle, WarningCircle, WifiSlash } from "@phosphor-icons/react/dist/ssr";
import { Button } from "@/components/ui/Button";
import type { Progress } from "@/lib/uploader";
import type { UploadPhase } from "@/types";
import { cn, formatBytes, formatEta } from "@/utils/format";

const steps = ["Preparing", "Uploading", "Processing", "Encoding", "Publishing", "Completed"] as const;
const index: Partial<Record<UploadPhase, number>> = { preparing: 0, uploading: 1, processing: 2, encoding: 3, ready: 4, publishing: 4, completed: 5 };

type Props = {
  phase: UploadPhase; progress: Progress; serverProgress: number; fileName: string; error: string | null;
  failedAtProcessing: boolean; onCancel: () => void; onRetry: () => void; onReset: () => void;
  /** The browser is offline: the upload is paused and continues by itself. */
  offline?: boolean;
  /** False when retrying the same file cannot help (the server refused it): offer to choose another file instead. */
  canRetry?: boolean;
  /** Stops server-side processing while the video is being processed or encoded (hidden when not provided). */
  onCancelProcessing?: () => void | Promise<void>;
};

export function UploadProgress({ phase, progress, serverProgress, fileName, error, failedAtProcessing, onCancel, onRetry, onReset, offline = false, canRetry = true, onCancelProcessing }: Props) {
  const [cancelling, setCancelling] = useState(false);
  const [cancelError, setCancelError] = useState<string | null>(null);
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
            : offline && uploading ? <><WifiSlash size={14} aria-hidden="true" />Waiting for connection…</>
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
      {phase === "uploading" && !offline && (
        <p className="text-xs text-fg-3">{progress.speed > 0 ? `${formatBytes(progress.speed)}/s • ${formatEta(progress.eta)}` : "Starting…"}</p>
      )}
      {phase === "uploading" && offline && (
        <p className="text-xs text-fg-2">You’re offline. The upload continues by itself when the connection is back; nothing is lost.</p>
      )}

      <ol className="grid grid-cols-3 gap-x-2 gap-y-1.5 text-[11px] sm:grid-cols-6 lg:grid-cols-2" aria-label="Steps">
        {steps.map((s, i) => (
          <li key={s} aria-current={i === idx && !failed ? "step" : undefined}
            className={cn("flex items-center gap-1.5", i < idx || phase === "completed" ? "text-white" : i === idx && !failed ? "font-semibold text-white" : "text-fg-3")}>
            <span aria-hidden="true" className={cn("size-1.5 rounded-full", i < idx || phase === "completed" ? "bg-white" : i === idx && !failed ? "bg-accent" : "bg-line")} />{s}
          </li>
        ))}
      </ol>

      {cancelError && <p role="alert" className="rounded-xl border border-accent/40 bg-accent/10 px-3 py-2 text-sm">{cancelError}</p>}
      {failed && error && <p role="alert" className="rounded-xl border border-accent/40 bg-accent/10 px-3 py-2 text-sm">{error}</p>}
      <div className="flex flex-wrap gap-2">
        {failed && !failedAtProcessing && canRetry && <Button className="h-11" onClick={onRetry}>Retry upload</Button>}
        {failed && !failedAtProcessing && !canRetry && <Button className="h-11" onClick={onReset}>Choose another file</Button>}
        {failed && failedAtProcessing && <Button className="h-11" onClick={onReset}>Upload again</Button>}
        {(phase === "processing" || phase === "encoding") && onCancelProcessing && (
          <Button variant="secondary" className="h-11" disabled={cancelling}
            onClick={async () => {
              setCancelling(true);
              setCancelError(null);
              try { await onCancelProcessing(); } catch { setCancelError("Couldn't cancel processing. Please try again."); } finally { setCancelling(false); }
            }}>
            {cancelling ? "Cancelling…" : "Cancel processing"}
          </Button>
        )}
        {(uploading || (phase === "failed" && canRetry)) && !failedAtProcessing && <Button variant="secondary" className="h-11" onClick={onCancel}>Cancel</Button>}
      </div>
    </section>
  );
}

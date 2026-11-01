"use client";
import { CheckCircle, Info, WarningCircle, X } from "@phosphor-icons/react";
import { useToastStore } from "@/store/toast";

const icon = { success: CheckCircle, error: WarningCircle, info: Info } as const;

export function ToastViewport() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);
  return (
    // Always-mounted live region so screen readers announce new toasts; errors use role="alert".
    <div aria-live="polite" className="pointer-events-none fixed inset-x-0 bottom-24 z-[70] flex flex-col items-center gap-2 px-3 lg:bottom-6">
      {toasts.map((t) => {
        const I = icon[t.kind];
        return (
          <div key={t.id} role={t.kind === "error" ? "alert" : undefined}
            className="pointer-events-auto flex w-full max-w-sm animate-[toast-in_0.15s_ease-out] items-center gap-3 rounded-xl border border-line bg-card px-4 py-3 text-sm shadow-soft">
            <I size={20} weight="fill" aria-hidden="true" className={t.kind === "error" ? "text-accent" : "text-fg-2"} />
            <span className="min-w-0 flex-1">{t.message}</span>
            <button type="button" onClick={() => dismiss(t.id)} aria-label="Dismiss notification" className="-mr-2 grid size-9 shrink-0 place-items-center rounded-full text-fg-2 hover:bg-surface hover:text-white"><X size={14} /></button>
          </div>
        );
      })}
    </div>
  );
}

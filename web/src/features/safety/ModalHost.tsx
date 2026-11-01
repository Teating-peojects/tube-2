"use client";
import { Copy, Link as LinkIcon, ShareNetwork } from "@phosphor-icons/react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { useModalStore } from "@/store/modal";
import { toast } from "@/store/toast";
import { ReportForm } from "./ReportForm";

function LoginRequired({ reason }: { reason?: string }) {
  const pathname = usePathname();
  const close = useModalStore((s) => s.close);
  const next = encodeURIComponent(pathname);
  return (
    <div className="space-y-4">
      <p className="text-sm text-fg-2">{reason ?? "Log in or create an account to do that."}</p>
      <div className="flex flex-col gap-2">
        <Link href={`/login?next=${next}`} onClick={close} className="inline-flex h-11 items-center justify-center rounded-full bg-accent text-sm font-semibold hover:bg-accent-hover">Log in</Link>
        <Link href={`/register?next=${next}`} onClick={close} className="inline-flex h-11 items-center justify-center rounded-full border border-line bg-surface text-sm font-semibold hover:bg-line">Create account</Link>
      </div>
    </div>
  );
}

function Share({ url, title }: { url: string; title: string }) {
  const u = encodeURIComponent(url);
  const t = encodeURIComponent(title);
  const targets = [
    { label: "X", href: `https://twitter.com/intent/tweet?url=${u}&text=${t}` },
    { label: "Reddit", href: `https://www.reddit.com/submit?url=${u}&title=${t}` },
    { label: "Telegram", href: `https://t.me/share/url?url=${u}&text=${t}` },
    { label: "WhatsApp", href: `https://wa.me/?text=${t}%20${u}` },
    { label: "Email", href: `mailto:?subject=${t}&body=${u}` },
  ];
  const canNative = typeof navigator !== "undefined" && typeof navigator.share === "function";

  async function copy() {
    try { await navigator.clipboard.writeText(url); toast.success("Link copied"); } catch { toast.error("Couldn’t copy. Select the link and copy it manually."); }
  }
  return (
    <div className="space-y-4">
      <div className="flex gap-2">
        <label className="sr-only" htmlFor="share-url">Link</label>
        <input id="share-url" readOnly value={url} onFocus={(e) => e.currentTarget.select()} className="h-11 min-w-0 flex-1 rounded-xl border border-line bg-bg px-3 text-sm" />
        <Button onClick={copy} className="h-11 px-4"><Copy size={16} aria-hidden="true" />Copy</Button>
      </div>
      {canNative && <Button variant="secondary" full onClick={() => navigator.share({ title, url }).catch(() => {})}><ShareNetwork size={16} aria-hidden="true" />Share via…</Button>}
      <ul className="grid grid-cols-3 gap-2 sm:grid-cols-5">
        {targets.map((s) => (
          <li key={s.label}><a href={s.href} target="_blank" rel="noopener noreferrer" className="flex h-11 items-center justify-center gap-1 rounded-xl border border-line bg-surface text-xs font-semibold hover:bg-line"><LinkIcon size={12} aria-hidden="true" />{s.label}</a></li>
        ))}
      </ul>
    </div>
  );
}

/** Single dialog host mounted once in the root layout; open via `modals.*` / useModalStore. */
export function ModalHost() {
  const modal = useModalStore((s) => s.modal);
  const close = useModalStore((s) => s.close);
  const answer = useModalStore((s) => s.answer);

  const title = !modal ? "" : modal.kind === "confirm" ? modal.options.title : modal.kind === "login-required" ? "Log in required"
    : modal.kind === "share" ? "Share" : "Report";

  return (
    <Modal open={modal !== null} onClose={close} title={title} size={modal?.kind === "report" ? "md" : "sm"}>
      {modal?.kind === "confirm" && (
        <div className="space-y-5">
          <p className="text-sm text-fg-2">{modal.options.message}</p>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => answer(false)}>{modal.options.cancelLabel ?? "Cancel"}</Button>
            <Button onClick={() => answer(true)}>{modal.options.confirmLabel ?? "Confirm"}</Button>
          </div>
        </div>
      )}
      {modal?.kind === "login-required" && <LoginRequired reason={modal.reason} />}
      {modal?.kind === "share" && <Share url={modal.url} title={modal.title} />}
      {modal?.kind === "report" && <ReportForm target={modal.target} onDone={close} />}
    </Modal>
  );
}

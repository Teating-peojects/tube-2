import Link from "next/link";
import type { ReactNode } from "react";

export function EmptyState({ icon, title, text, action }: { icon: ReactNode; title: string; text?: string; action?: { label: string; href: string } }) {
  return (
    <div className="grid place-items-center gap-3 rounded-card border border-line bg-card px-6 py-16 text-center">
      <span aria-hidden="true" className="grid size-16 place-items-center rounded-full border border-line bg-surface text-fg-2">{icon}</span>
      <p className="text-lg font-bold">{title}</p>
      {text && <p className="max-w-sm text-sm text-fg-2">{text}</p>}
      {action && <Link href={action.href} className="mt-1 inline-flex h-11 items-center rounded-full bg-accent px-6 text-sm font-semibold transition-colors hover:bg-accent-hover">{action.label}</Link>}
    </div>
  );
}

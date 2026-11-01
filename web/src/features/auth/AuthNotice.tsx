import type { ReactNode } from "react";

export function AuthError({ children }: { children: ReactNode }) {
  return <div role="alert" className="rounded-xl border border-accent/40 bg-accent/10 px-4 py-3 text-sm">{children}</div>;
}
export function AuthInfo({ children }: { children: ReactNode }) {
  return <div role="status" className="rounded-xl border border-line bg-surface px-4 py-3 text-sm text-fg-2">{children}</div>;
}
export function AuthTitle({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div className="mb-6 text-center">
      <h1 className="text-2xl font-extrabold">{title}</h1>
      {subtitle && <p className="mt-1.5 text-sm text-fg-2">{subtitle}</p>}
    </div>
  );
}

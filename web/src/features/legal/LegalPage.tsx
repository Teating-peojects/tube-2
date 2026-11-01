import type { ReactNode } from "react";
import { site } from "@/config/site";

export function LegalPage({ title, children }: { title: string; children: ReactNode }) {
  return (
    <article className="space-y-4 text-sm leading-relaxed text-fg-2">
      <header className="mb-6">
        <h1 className="text-3xl font-extrabold text-white">{title}</h1>
        <p className="mt-2 text-xs">Last updated: {site.updated}</p>
        {process.env.NODE_ENV !== "production" && (
          <p role="note" className="mt-4 rounded-xl border border-line bg-surface p-3 text-xs">
            Draft template: replace the [bracketed] items and have qualified legal counsel review this page before launch.
          </p>
        )}
      </header>
      {children}
    </article>
  );
}
export const H = ({ children }: { children: ReactNode }) => <h2 className="pt-4 text-lg font-bold text-white">{children}</h2>;
export const P = ({ children }: { children: ReactNode }) => <p>{children}</p>;
export const UL = ({ children }: { children: ReactNode }) => <ul className="list-disc space-y-1.5 pl-5">{children}</ul>;

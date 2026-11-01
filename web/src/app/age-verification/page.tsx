import type { Metadata } from "next";
import Link from "next/link";
import { safeNext } from "@/lib/auth";
import { AgeGateActions } from "./AgeGateActions";

export const metadata: Metadata = { title: "Age Verification", robots: { index: false } };

export default async function AgeVerificationPage({ searchParams }: { searchParams: Promise<{ next?: string | string[] }> }) {
  const raw = (await searchParams).next;
  const next = safeNext(Array.isArray(raw) ? raw[0] : raw);
  return (
    <div className="grid min-h-dvh place-items-center bg-[radial-gradient(400px_260px_at_50%_0,rgba(255,0,60,.16),transparent_70%)] p-4">
      <section role="dialog" aria-modal="true" aria-labelledby="age-title" className="w-full max-w-[400px] rounded-[18px] border border-line bg-card p-8 text-center shadow-soft">
        <span className="inline-grid size-11 place-items-center rounded-[11px] bg-accent text-2xl font-extrabold text-black">X</span>
        <p className="mt-4 text-4xl font-extrabold">18+</p>
        <h1 id="age-title" className="mb-2.5 mt-1.5 text-lg font-bold">Age Restricted Content</h1>
        <p className="mb-6 text-sm leading-relaxed text-fg-2">You must be 18 years or older to access this platform.</p>
        <AgeGateActions next={next} />
        <nav aria-label="Legal" className="mt-5 flex justify-center gap-4 text-xs text-fg-2">
          <Link href="/privacy" className="hover:text-white">Privacy</Link>
          <Link href="/terms" className="hover:text-white">Terms</Link>
          <Link href="/community-guidelines" className="hover:text-white">Safety</Link>
        </nav>
      </section>
    </div>
  );
}

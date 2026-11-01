import Link from "next/link";
import type { ReactNode } from "react";
import { Logo } from "@/components/ui/Logo";

export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div className="grid min-h-dvh place-items-center bg-[radial-gradient(500px_300px_at_50%_0,rgba(255,0,60,.14),transparent_70%)] px-4 py-10">
      <div className="w-full max-w-[420px]">
        <div className="mb-6 flex justify-center"><Logo /></div>
        <main id="main" className="rounded-[18px] border border-line bg-card p-6 shadow-soft sm:p-8">{children}</main>
        <nav aria-label="Legal" className="mt-5 flex justify-center gap-4 text-xs text-fg-2">
          <Link href="/privacy" className="hover:text-white">Privacy</Link>
          <Link href="/terms" className="hover:text-white">Terms</Link>
          <Link href="/community-guidelines" className="hover:text-white">Safety</Link>
        </nav>
      </div>
    </div>
  );
}

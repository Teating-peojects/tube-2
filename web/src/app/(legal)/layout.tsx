import Link from "next/link";
import type { ReactNode } from "react";
import { Logo } from "@/components/ui/Logo";

export default function LegalLayout({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-dvh">
      <header className="flex h-16 items-center justify-between border-b border-line px-4 md:px-8">
        <Logo />
        <Link href="/" className="text-sm text-fg-2 hover:text-white">Back to REDXTUBE</Link>
      </header>
      <main id="main" className="mx-auto max-w-3xl px-4 py-10 md:px-0">{children}</main>
      <nav aria-label="Legal" className="mx-auto flex max-w-3xl flex-wrap gap-x-5 gap-y-2 border-t border-line px-4 py-6 text-xs text-fg-2 md:px-0">
        <Link href="/terms" className="hover:text-white">Terms</Link>
        <Link href="/privacy" className="hover:text-white">Privacy</Link>
        <Link href="/community-guidelines" className="hover:text-white">Community Guidelines</Link>
        <Link href="/dmca" className="hover:text-white">Copyright / DMCA</Link>
      </nav>
    </div>
  );
}

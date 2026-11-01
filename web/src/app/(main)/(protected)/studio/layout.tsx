import Link from "next/link";
import type { ReactNode } from "react";
import { StudioNav } from "@/features/studio/StudioNav";

export default function StudioLayout({ children }: { children: ReactNode }) {
  return (
    <div className="mx-auto max-w-6xl">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h1 className="text-2xl font-extrabold">Creator Studio</h1>
        <Link href="/upload" className="inline-flex h-10 items-center rounded-full bg-accent px-5 text-sm font-semibold transition-colors hover:bg-accent-hover">Upload</Link>
      </div>
      <StudioNav />
      <div className="pt-6">{children}</div>
    </div>
  );
}

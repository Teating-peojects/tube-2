import Link from "next/link";

// TEMPORARY mark. Next task: swap for the official logo asset (public/logo.png).
export function Logo({ compact = false }: { compact?: boolean }) {
  return (
    <Link href="/" aria-label="REDXTUBE home" className="flex items-center gap-2.5 font-extrabold tracking-wide">
      <span className="grid size-8 place-items-center rounded-lg bg-accent text-lg text-black">R</span>
      {!compact && (
        <span className="text-lg text-white">
          <span className="text-accent">RED</span>XTUBE
        </span>
      )}
    </Link>
  );
}

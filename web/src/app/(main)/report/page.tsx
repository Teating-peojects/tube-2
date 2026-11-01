import type { Metadata } from "next";
import Link from "next/link";
import { ReportForm } from "@/features/safety/ReportForm";
import type { ReportTarget } from "@/types";

type Props = { searchParams: Promise<{ type?: string | string[]; id?: string | string[] }> };
export const metadata: Metadata = { title: "Report content", robots: { index: false, follow: false } };

const TYPES = ["video", "comment", "channel"] as const;

export default async function ReportPage({ searchParams }: Props) {
  const sp = await searchParams;
  const type = typeof sp.type === "string" && (TYPES as readonly string[]).includes(sp.type) ? (sp.type as ReportTarget["type"]) : null;
  const id = typeof sp.id === "string" && /^[A-Za-z0-9_-]{1,64}$/.test(sp.id) ? sp.id : null; // validated, never echoed as HTML
  return (
    <div className="mx-auto max-w-lg">
      <h1 className="mb-1 text-2xl font-extrabold">Report content</h1>
      <p className="mb-6 text-sm text-fg-2">Help us keep REDXTUBE safe. Reports are confidential.</p>
      {type && id ? (
        <div className="rounded-card border border-line bg-card p-5"><ReportForm target={{ type, id }} /></div>
      ) : (
        <div role="alert" className="rounded-card border border-line bg-card p-6 text-sm">
          This report link is incomplete. Open the menu on a video, comment or channel and choose <strong>Report</strong>, or go back to <Link href="/" className="underline">Home</Link>.
        </div>
      )}
    </div>
  );
}

"use client";
import { CheckCircle } from "@phosphor-icons/react";
import { useMutation, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { Textarea } from "@/components/ui/Input";
import { ApiError } from "@/services/api";
import { getReportReasons, submitReport } from "@/services/safety";
import { toast } from "@/store/toast";
import type { ReportTarget } from "@/types";

function message(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === "already_reported" || e.status === 409) return "You’ve already reported this. Our team will review it.";
    if (e.status === 429) return "You’re sending reports too quickly. Please try again shortly.";
    if (e.status === 401) return "Please log in to submit a report.";
  }
  return "Couldn’t submit your report. Please try again.";
}

/** Shared by the report modal and the /report page. Shows no internal moderation data. */
export function ReportForm({ target, onDone }: { target: ReportTarget; onDone?: () => void }) {
  const [reason, setReason] = useState("");
  const [details, setDetails] = useState("");
  const reasons = useQuery({ queryKey: ["report-reasons"], queryFn: getReportReasons, staleTime: 30 * 60_000 });
  const send = useMutation({
    mutationFn: () => submitReport({ target, reason, details: details.trim() || undefined }),
    onSuccess: () => toast.success("Report submitted"),
  });
  const needsDetails = reason === "other";
  const valid = reason !== "" && (!needsDetails || details.trim().length > 0);

  if (send.isSuccess) {
    return (
      <div className="grid place-items-center gap-3 py-6 text-center">
        <CheckCircle size={40} weight="fill" aria-hidden="true" />
        <p className="font-semibold">Thanks, your report was submitted</p>
        <p className="text-sm text-fg-2">Our moderators will review it. We may not be able to share the outcome.</p>
        {onDone && <Button variant="secondary" onClick={onDone}>Close</Button>}
      </div>
    );
  }

  return (
    <form onSubmit={(e) => { e.preventDefault(); if (valid) send.mutate(); }} className="space-y-4">
      <fieldset className="space-y-2">
        <legend className="mb-1 text-sm font-medium">Why are you reporting this {target.type}?</legend>
        {reasons.isPending && <div className="space-y-2" aria-hidden="true">{Array.from({ length: 5 }, (_, i) => <div key={i} className="skeleton h-12 rounded-xl" />)}</div>}
        {reasons.data?.map((r) => (
          <label key={r.id} className="flex cursor-pointer items-start gap-3 rounded-xl border border-line bg-bg p-3 text-sm has-[:checked]:border-white">
            <input type="radio" name="reason" value={r.id} checked={reason === r.id} onChange={() => setReason(r.id)} className="mt-0.5 size-5 shrink-0 accent-accent" />
            <span><span className="block font-semibold">{r.label}</span>{r.description && <span className="text-xs text-fg-2">{r.description}</span>}</span>
          </label>
        ))}
      </fieldset>
      {reason === "underage" && (
        <p role="note" className="rounded-xl border border-line bg-surface p-3 text-xs text-fg-2">
          If a child may be in immediate danger, contact your local emergency services. See our <Link href="/community-guidelines" className="underline">Community Guidelines</Link>.
        </p>
      )}
      <Textarea label={needsDetails ? "Details (required)" : "Details (optional)"} rows={3} maxLength={1000} value={details} onChange={(e) => setDetails(e.target.value)} hint="Don’t include personal information about yourself." />
      {send.isError && <p role="alert" className="text-sm text-accent">{message(send.error)}</p>}
      <Button type="submit" full disabled={!valid} loading={send.isPending}>Submit report</Button>
    </form>
  );
}

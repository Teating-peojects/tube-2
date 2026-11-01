"use client";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import { ApiError, api } from "@/services/api";

const THIRTY_DAYS = 60 * 60 * 24 * 30;

export function AgeGateActions({ next = "/" }: { next?: string }) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function confirm() {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      if (env.useMock) {
        // Mock mode has no backend: a plain UI flag is enough for demos and the e2e tests.
        document.cookie = `xh_age_ok=1; Path=/; Max-Age=${THIRTY_DAYS}; SameSite=Lax${location.protocol === "https:" ? "; Secure" : ""}`;
      } else {
        // The API sets the signed, httpOnly `xh_age` cookie (the only proof it trusts) and the `xh_age_ok` marker.
        await api(endpoints.age.verify, { method: "POST", body: JSON.stringify({ confirmed: true }) });
      }
      router.replace(next);
      router.refresh();
    } catch (e) {
      setBusy(false);
      if (e instanceof ApiError && e.code === "rate_limited") {
        setError(`Too many attempts. Try again${e.retryAfter ? ` in ${e.retryAfter} seconds` : " later"}.`);
      } else if (e instanceof ApiError && e.code === "network") {
        setError("Can’t reach the server. Check your connection and try again.");
      } else {
        setError("Something went wrong. Please try again.");
      }
    }
  }

  return (
    <div className="flex flex-col gap-2.5">
      <button type="button" onClick={confirm} disabled={busy} aria-busy={busy}
        className="h-[46px] rounded-full bg-accent text-[15px] font-semibold transition-colors hover:bg-accent-hover disabled:opacity-60">I am 18+</button>
      {error && <p role="alert" className="text-sm text-accent">{error}</p>}
      <a href="https://www.google.com" rel="noopener noreferrer" className="grid h-[46px] place-items-center rounded-full border border-line bg-surface text-[15px] font-semibold transition-colors hover:bg-line">Exit</a>
    </div>
  );
}

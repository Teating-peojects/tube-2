"use client";
import { useQueryClient } from "@tanstack/react-query";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { meKey } from "./useSession";

/** Reacts to a 401 from any API call while signed in: clear the session and send the user to log in. */
export function SessionWatcher() {
  const qc = useQueryClient();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    const onUnauthorized = () => {
      if (!qc.getQueryData(meKey)) return; // anonymous users hitting a 401 are not "expired"
      qc.clear();
      qc.setQueryData(meKey, null);
      router.replace(`/login?reason=expired&next=${encodeURIComponent(pathname)}`);
    };
    window.addEventListener("xh:unauthorized", onUnauthorized);
    return () => window.removeEventListener("xh:unauthorized", onUnauthorized);
  }, [qc, router, pathname]);

  return null;
}

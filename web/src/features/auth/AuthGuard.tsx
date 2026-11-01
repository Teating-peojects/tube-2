"use client";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { useSession } from "./useSession";

/** Client-side guard for protected pages. The backend must still authorize every API call. */
export function AuthGuard({ children }: { children: ReactNode }) {
  const { user, status, refetch } = useSession();
  const router = useRouter();
  const pathname = usePathname();
  const next = encodeURIComponent(pathname);

  useEffect(() => {
    if (status === "anonymous") router.replace(`/login?next=${next}`);
    else if (status === "authenticated" && user && !user.emailVerified) router.replace(`/verify?next=${next}`);
  }, [status, user, router, next]);

  if (status === "error") {
    return (
      <div role="alert" className="grid place-items-center gap-3 py-24 text-center">
        <p className="font-semibold">Couldn’t check your session</p>
        <p className="text-sm text-fg-2">Check your connection and try again.</p>
        <Button onClick={() => refetch()}>Retry</Button>
      </div>
    );
  }
  if (status !== "authenticated" || !user?.emailVerified) {
    return <div className="grid place-items-center py-24" role="status" aria-label="Checking session"><div className="skeleton size-10 rounded-full" /></div>;
  }
  return <>{children}</>;
}

"use client";
import { useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { getMe } from "@/services/auth";

export const meKey = ["me"] as const;

/** Auth is server state: it lives in TanStack Query, never in Zustand/localStorage. */
export function useSession() {
  const q = useQuery({
    queryKey: meKey,
    queryFn: getMe,
    staleTime: 5 * 60_000,
    retry: false,
    refetchOnWindowFocus: true, // detects expired sessions when the user returns to the tab
  });
  const status = q.isPending ? "loading" : q.isError ? "error" : q.data ? "authenticated" : "anonymous";
  return { user: q.data ?? null, status, refetch: q.refetch } as const;
}

/** For login/register pages: send signed-in users onward. */
export function useRedirectIfAuthenticated(next: string) {
  const { user } = useSession();
  const router = useRouter();
  useEffect(() => {
    if (user) router.replace(user.emailVerified ? next : `/verify?next=${encodeURIComponent(next)}`);
  }, [user, next, router]);
}

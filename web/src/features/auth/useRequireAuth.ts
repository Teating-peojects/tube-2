"use client";
import { useCallback } from "react";
import { useModalStore } from "@/store/modal";
import { useSession } from "./useSession";

/** Returns a guard: `if (!requireAuth("Log in to subscribe")) return;` opens the "Login required" modal for anonymous users. */
export function useRequireAuth() {
  const { status } = useSession();
  return useCallback((reason?: string) => {
    if (status === "authenticated") return true;
    useModalStore.getState().openLoginRequired(reason);
    return false;
  }, [status]);
}

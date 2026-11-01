"use client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getSettings, patchSettings } from "@/services/settings";
import { toast } from "@/store/toast";
import type { Settings, SettingsPatch } from "@/types";

export const settingsKey = ["settings"] as const;

export function useSettings() {
  return useQuery({ queryKey: settingsKey, queryFn: getSettings });
}

/** Optimistic toggle: UI flips immediately, rolls back and shows an error toast if saving fails. */
export function usePatchSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: SettingsPatch) => patchSettings(p),
    onMutate: async (p) => {
      await qc.cancelQueries({ queryKey: settingsKey });
      const prev = qc.getQueryData<Settings>(settingsKey);
      qc.setQueryData<Settings>(settingsKey, (s) => s && { privacy: { ...s.privacy, ...p.privacy }, notifications: { ...s.notifications, ...p.notifications } });
      return { prev };
    },
    onSuccess: () => toast.success("Settings saved"),
    onError: (_e, _p, ctx) => { if (ctx?.prev) qc.setQueryData(settingsKey, ctx.prev); toast.error("Couldn’t save your settings"); },
  });
}

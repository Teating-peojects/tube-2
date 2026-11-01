import { create } from "zustand";

export type ToastKind = "success" | "error" | "info";
export type ToastItem = { id: number; kind: ToastKind; message: string };
type ToastState = { toasts: ToastItem[]; push: (kind: ToastKind, message: string) => number; dismiss: (id: number) => void };

let nextId = 1;
export const useToastStore = create<ToastState>()((set, get) => ({
  toasts: [],
  push: (kind, message) => {
    const id = nextId++;
    set((s) => ({ toasts: [...s.toasts, { id, kind, message }].slice(-3) })); // never stack more than 3
    setTimeout(() => get().dismiss(id), kind === "error" ? 6000 : 4000);
    return id;
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));

/** Imperative helper usable from any mutation callback. */
export const toast = {
  success: (m: string) => useToastStore.getState().push("success", m),
  error: (m: string) => useToastStore.getState().push("error", m),
  info: (m: string) => useToastStore.getState().push("info", m),
};

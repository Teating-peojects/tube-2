import { create } from "zustand";
import type { ReportTarget } from "@/types";

export type ConfirmOptions = { title: string; message: string; confirmLabel?: string; cancelLabel?: string; danger?: boolean };
export type ActiveModal =
  | { kind: "login-required"; reason?: string }
  | { kind: "share"; url: string; title: string }
  | { kind: "report"; target: ReportTarget }
  | { kind: "confirm"; options: ConfirmOptions; resolve: (v: boolean) => void };

type ModalState = {
  modal: ActiveModal | null;
  openLoginRequired: (reason?: string) => void;
  openShare: (p: { url: string; title: string }) => void;
  openReport: (target: ReportTarget) => void;
  confirm: (o: ConfirmOptions) => Promise<boolean>;
  answer: (v: boolean) => void;
  close: () => void;
};

export const useModalStore = create<ModalState>()((set, get) => ({
  modal: null,
  openLoginRequired: (reason) => set({ modal: { kind: "login-required", reason } }),
  openShare: (p) => set({ modal: { kind: "share", ...p } }),
  openReport: (target) => set({ modal: { kind: "report", target } }),
  confirm: (options) => new Promise<boolean>((resolve) => set({ modal: { kind: "confirm", options, resolve } })),
  answer: (v) => {
    const m = get().modal;
    if (m?.kind === "confirm") m.resolve(v);
    set({ modal: null });
  },
  close: () => {
    const m = get().modal;
    if (m?.kind === "confirm") m.resolve(false); // dismissing counts as "cancel"
    set({ modal: null });
  },
}));

export const modals = {
  confirm: (o: ConfirmOptions) => useModalStore.getState().confirm(o),
  share: (p: { url: string; title: string }) => useModalStore.getState().openShare(p),
  report: (t: ReportTarget) => useModalStore.getState().openReport(t),
};

import { create } from "zustand";
import { persist } from "zustand/middleware";

type UIState = {
  sidebarCollapsed: boolean;
  autoplayNext: boolean;
  toggleSidebar: () => void;
  setAutoplayNext: (v: boolean) => void;
};

// Client UI state only. Server data lives in TanStack Query.
export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      autoplayNext: true,
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setAutoplayNext: (autoplayNext) => set({ autoplayNext }),
    }),
    { name: "xh-ui" },
  ),
);

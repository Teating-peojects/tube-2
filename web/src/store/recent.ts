import { create } from "zustand";
import { persist } from "zustand/middleware";

type RecentState = {
  items: string[];
  add: (q: string) => void;
  remove: (q: string) => void;
  clear: () => void;
};

// Stored on this device only (never sent to the server).
export const useRecentSearches = create<RecentState>()(
  persist(
    (set) => ({
      items: [],
      add: (q) => {
        const t = q.trim().slice(0, 100);
        if (!t) return;
        set((s) => ({ items: [t, ...s.items.filter((i) => i.toLowerCase() !== t.toLowerCase())].slice(0, 8) }));
      },
      remove: (q) => set((s) => ({ items: s.items.filter((i) => i !== q) })),
      clear: () => set({ items: [] }),
    }),
    { name: "xh-recent" },
  ),
);

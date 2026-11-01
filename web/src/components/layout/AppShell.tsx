import type { ReactNode } from "react";
import { BottomNav } from "./BottomNav";
import { Header } from "./Header";
import { Sidebar } from "./Sidebar";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <>
      <Header />
      <div className="flex">
        <Sidebar />
        {/* bottom padding keeps content clear of the mobile bottom nav */}
        <main id="main" className="min-w-0 flex-1 px-3 pb-24 pt-4 md:px-7 lg:pb-12 lg:pt-6">{children}</main>
      </div>
      <BottomNav />
    </>
  );
}

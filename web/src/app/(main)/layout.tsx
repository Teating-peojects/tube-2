import type { ReactNode } from "react";
import { AppShell } from "@/components/layout/AppShell";
import { RealtimeBridge } from "@/features/notifications/RealtimeBridge";

export default function MainLayout({ children }: { children: ReactNode }) {
  return (
    <AppShell>
      <RealtimeBridge />
      {children}
    </AppShell>
  );
}

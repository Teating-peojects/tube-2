import type { ReactNode } from "react";
import { SettingsNav } from "@/features/settings/SettingsNav";

export default function SettingsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="mx-auto max-w-4xl">
      <h1 className="mb-5 text-2xl font-extrabold">Settings</h1>
      <div className="grid gap-6 md:grid-cols-[200px_minmax(0,1fr)]">
        <SettingsNav />
        <div>{children}</div>
      </div>
    </div>
  );
}

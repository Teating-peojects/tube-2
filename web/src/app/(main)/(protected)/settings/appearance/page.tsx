import type { Metadata } from "next";
import { AppearancePanel } from "@/features/settings/panels";

export const metadata: Metadata = { title: "Appearance settings", robots: { index: false, follow: false } };

export default function Page() {
  return <AppearancePanel />;
}

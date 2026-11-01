import type { Metadata } from "next";
import { SecurityPanel } from "@/features/settings/panels";

export const metadata: Metadata = { title: "Security settings", robots: { index: false, follow: false } };

export default function Page() {
  return <SecurityPanel />;
}

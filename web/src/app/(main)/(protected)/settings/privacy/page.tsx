import type { Metadata } from "next";
import { PrivacyPanel } from "@/features/settings/panels";

export const metadata: Metadata = { title: "Privacy settings", robots: { index: false, follow: false } };

export default function Page() {
  return <PrivacyPanel />;
}

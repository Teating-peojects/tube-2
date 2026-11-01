import type { Metadata } from "next";
import { AccountPanel } from "@/features/settings/panels";

export const metadata: Metadata = { title: "Account settings", robots: { index: false, follow: false } };

export default function Page() {
  return <AccountPanel />;
}

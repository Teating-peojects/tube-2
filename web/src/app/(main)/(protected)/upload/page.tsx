import type { Metadata } from "next";
import { UploadView } from "@/features/upload/UploadView";

export const metadata: Metadata = { title: "Upload", robots: { index: false, follow: false } };

export default function Page() {
  return <UploadView />;
}

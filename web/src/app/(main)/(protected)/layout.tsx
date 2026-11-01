import type { ReactNode } from "react";
import { AuthGuard } from "@/features/auth/AuthGuard";

// Every page placed in this group (profile, history, upload, studio, ...) requires a verified session.
export default function ProtectedLayout({ children }: { children: ReactNode }) {
  return <AuthGuard>{children}</AuthGuard>;
}

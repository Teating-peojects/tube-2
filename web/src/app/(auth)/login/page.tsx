import type { Metadata } from "next";
import { Suspense } from "react";
import { LoginForm } from "@/features/auth/LoginForm";

export const metadata: Metadata = { title: "Log in", robots: { index: false, follow: false } };

export default function Page() {
  // useSearchParams() inside the form requires a Suspense boundary.
  return (
    <Suspense fallback={<div className="skeleton h-72 rounded-card" aria-hidden="true" />}>
      <LoginForm />
    </Suspense>
  );
}

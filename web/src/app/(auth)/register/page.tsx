import type { Metadata } from "next";
import { Suspense } from "react";
import { RegisterForm } from "@/features/auth/RegisterForm";

export const metadata: Metadata = { title: "Create account", robots: { index: false, follow: false } };

export default function Page() {
  // useSearchParams() inside the form requires a Suspense boundary.
  return (
    <Suspense fallback={<div className="skeleton h-72 rounded-card" aria-hidden="true" />}>
      <RegisterForm />
    </Suspense>
  );
}

"use client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { PasswordInput } from "@/components/ui/Input";
import { resetPassword } from "@/services/auth";
import { AuthError, AuthTitle } from "./AuthNotice";
import { authErrorMessage } from "./errors";
import { resetSchema, type ResetValues } from "./schemas";

export function ResetPasswordForm() {
  const token = useSearchParams().get("token") ?? "";
  const router = useRouter();
  const { register, handleSubmit, formState: { errors } } = useForm<ResetValues>({ resolver: zodResolver(resetSchema), defaultValues: { password: "", confirm: "" } });
  const m = useMutation({
    mutationFn: (v: ResetValues) => resetPassword(token, v.password),
    onSuccess: () => router.replace("/login?reason=password-reset"),
  });

  if (!token) {
    return (
      <div className="space-y-4 text-center">
        <AuthTitle title="Link not valid" subtitle="This password reset link is missing or has expired." />
        <Link href="/forgot-password" className="inline-flex h-11 items-center rounded-full bg-accent px-6 text-sm font-semibold hover:bg-accent-hover">Request a new link</Link>
      </div>
    );
  }
  return (
    <>
      <AuthTitle title="Choose a new password" />
      <form onSubmit={handleSubmit((v) => m.mutate(v))} noValidate className="space-y-4">
        {m.isError && (
          <AuthError>
            {authErrorMessage(m.error)}{" "}
            <Link href="/forgot-password" className="font-semibold underline">Request a new link</Link>
          </AuthError>
        )}
        <PasswordInput label="New password" autoComplete="new-password" hint="At least 10 characters, with a letter and a number" error={errors.password?.message} {...register("password")} />
        <PasswordInput label="Confirm new password" autoComplete="new-password" error={errors.confirm?.message} {...register("confirm")} />
        <Button type="submit" full loading={m.isPending}>Update password</Button>
      </form>
    </>
  );
}

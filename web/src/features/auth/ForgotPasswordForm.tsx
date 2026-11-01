"use client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { forgotPassword } from "@/services/auth";
import { AuthError, AuthInfo, AuthTitle } from "./AuthNotice";
import { authErrorMessage } from "./errors";
import { forgotSchema, type ForgotValues } from "./schemas";

export function ForgotPasswordForm() {
  const { register, handleSubmit, formState: { errors } } = useForm<ForgotValues>({ resolver: zodResolver(forgotSchema), defaultValues: { email: "" } });
  const m = useMutation({ mutationFn: (v: ForgotValues) => forgotPassword(v.email) });

  return (
    <>
      <AuthTitle title="Reset your password" subtitle="Enter your email and we’ll send you a reset link" />
      {m.isSuccess ? (
        // Same message whether or not the account exists (prevents account enumeration).
        <div className="space-y-4">
          <AuthInfo>If an account exists for that email, we’ve sent instructions to reset your password.</AuthInfo>
          <Link href="/login" className="block text-center text-sm font-semibold hover:underline">Back to log in</Link>
        </div>
      ) : (
        <form onSubmit={handleSubmit((v) => m.mutate(v))} noValidate className="space-y-4">
          {m.isError && <AuthError>{authErrorMessage(m.error)}</AuthError>}
          <Input label="Email" type="email" autoComplete="email" error={errors.email?.message} {...register("email")} />
          <Button type="submit" full loading={m.isPending}>Send reset link</Button>
          <Link href="/login" className="block text-center text-sm text-fg-2 hover:text-white">Back to log in</Link>
        </form>
      )}
    </>
  );
}

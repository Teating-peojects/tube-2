"use client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { maskEmail, safeNext } from "@/lib/auth";
import { resendCode, verifyCode } from "@/services/auth";
import { AuthError, AuthInfo, AuthTitle } from "./AuthNotice";
import { authErrorMessage } from "./errors";
import { verifySchema, type VerifyValues } from "./schemas";
import { meKey, useSession } from "./useSession";

const COOLDOWN = 30;

export function VerifyForm() {
  const next = safeNext(useSearchParams().get("next"));
  const router = useRouter();
  const qc = useQueryClient();
  const { user, status, refetch } = useSession();
  const [cool, setCool] = useState(COOLDOWN);

  useEffect(() => {
    if (status === "anonymous") router.replace(`/login?next=${encodeURIComponent(next)}`);
    else if (user?.emailVerified) router.replace(next);
  }, [status, user, next, router]);

  useEffect(() => {
    if (cool <= 0) return;
    const id = setTimeout(() => setCool((c) => c - 1), 1000);
    return () => clearTimeout(id);
  }, [cool]);

  const { register, handleSubmit, formState: { errors } } = useForm<VerifyValues>({ resolver: zodResolver(verifySchema), defaultValues: { code: "" } });
  const verify = useMutation({
    mutationFn: (v: VerifyValues) => verifyCode(v.code),
    onSuccess: (u) => { qc.setQueryData(meKey, u); router.replace(next); },
  });
  const resend = useMutation({ mutationFn: resendCode, onSuccess: () => setCool(COOLDOWN) });

  if (status === "error") {
    return (
      <div role="alert" className="space-y-3 text-center">
        <p className="font-semibold">Couldn’t load your account</p>
        <Button onClick={() => refetch()}>Retry</Button>
      </div>
    );
  }
  if (!user) return <div className="skeleton mx-auto h-40 w-full rounded-card" role="status" aria-label="Loading" />;

  return (
    <>
      <AuthTitle title="Verify your account" subtitle={`Enter the 6-digit code sent to ${maskEmail(user.email)}`} />
      <form onSubmit={handleSubmit((v) => verify.mutate(v))} noValidate className="space-y-4">
        {verify.isError && <AuthError>{authErrorMessage(verify.error)}</AuthError>}
        {resend.isSuccess && cool > 0 && <AuthInfo>A new code is on its way.</AuthInfo>}
        {resend.isError && <AuthError>{authErrorMessage(resend.error)}</AuthError>}
        <Input label="Verification code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} pattern="[0-9]*"
          className="text-center text-lg tracking-[0.5em]" error={errors.code?.message} {...register("code")} />
        <Button type="submit" full loading={verify.isPending}>Verify</Button>
      </form>
      <div className="mt-5 text-center text-sm text-fg-2">
        Didn’t get a code?{" "}
        <button type="button" disabled={cool > 0 || resend.isPending} onClick={() => resend.mutate()}
          className="font-semibold text-white hover:underline disabled:cursor-not-allowed disabled:text-fg-3 disabled:no-underline">
          {cool > 0 ? `Resend in ${cool}s` : "Resend code"}
        </button>
      </div>
    </>
  );
}

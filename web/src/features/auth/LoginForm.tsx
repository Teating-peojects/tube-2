"use client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { Input, PasswordInput } from "@/components/ui/Input";
import { safeNext } from "@/lib/auth";
import { login } from "@/services/auth";
import { ApiError } from "@/services/api";
import { AuthError, AuthInfo, AuthTitle } from "./AuthNotice";
import { authErrorMessage } from "./errors";
import { loginSchema, type LoginValues } from "./schemas";
import { meKey, useRedirectIfAuthenticated } from "./useSession";

export function LoginForm() {
  const sp = useSearchParams();
  const router = useRouter();
  const qc = useQueryClient();
  const next = safeNext(sp.get("next"));
  const reason = sp.get("reason");
  useRedirectIfAuthenticated(next);

  const { register, handleSubmit, formState: { errors } } = useForm<LoginValues>({
    resolver: zodResolver(loginSchema), defaultValues: { identifier: "", password: "" },
  });

  const m = useMutation({
    mutationFn: (v: LoginValues) => login(v.identifier, v.password),
    onSuccess: (user) => { qc.setQueryData(meKey, user); router.replace(next); },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "verification_required") {
        void qc.invalidateQueries({ queryKey: meKey });
        router.push(`/verify?next=${encodeURIComponent(next)}`);
      }
    },
  });
  const verifying = m.error instanceof ApiError && m.error.code === "verification_required";

  return (
    <>
      <AuthTitle title="Welcome back" subtitle="Log in to continue to REDXTUBE" />
      <form onSubmit={handleSubmit((v) => m.mutate(v))} noValidate className="space-y-4">
        {reason === "expired" && <AuthInfo>Your session expired. Please log in again.</AuthInfo>}
        {reason === "password-reset" && <AuthInfo>Password updated. Log in with your new password.</AuthInfo>}
        {m.isError && !verifying && <AuthError>{authErrorMessage(m.error)}</AuthError>}
        <Input label="Email or username" autoComplete="username" autoCapitalize="none" spellCheck={false} error={errors.identifier?.message} {...register("identifier")} />
        <PasswordInput label="Password" autoComplete="current-password" error={errors.password?.message} {...register("password")} />
        <div className="text-right"><Link href="/forgot-password" className="text-sm text-fg-2 hover:text-white">Forgot password?</Link></div>
        <Button type="submit" full loading={m.isPending}>Log in</Button>
      </form>
      <p className="mt-6 text-center text-sm text-fg-2">
        New to REDXTUBE? <Link href={`/register${next !== "/" ? `?next=${encodeURIComponent(next)}` : ""}`} className="font-semibold text-white hover:underline">Create an account</Link>
      </p>
    </>
  );
}

"use client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { Input, PasswordInput } from "@/components/ui/Input";
import { safeNext } from "@/lib/auth";
import { register as registerUser } from "@/services/auth";
import { AuthError, AuthTitle } from "./AuthNotice";
import { authErrorMessage } from "./errors";
import { registerSchema, type RegisterValues } from "./schemas";
import { meKey, useRedirectIfAuthenticated } from "./useSession";

export function RegisterForm() {
  const sp = useSearchParams();
  const qc = useQueryClient();
  const next = safeNext(sp.get("next"));
  useRedirectIfAuthenticated(next); // sends the new (unverified) user to /verify

  const { register, handleSubmit, formState: { errors } } = useForm<RegisterValues>({
    resolver: zodResolver(registerSchema),
    defaultValues: { email: "", username: "", password: "", confirm: "" },
  });

  const m = useMutation({
    mutationFn: (v: RegisterValues) => registerUser({ email: v.email, username: v.username, password: v.password }),
    onSuccess: (user) => qc.setQueryData(meKey, user),
  });

  const check = "mt-0.5 size-5 shrink-0 accent-accent";
  return (
    <>
      <AuthTitle title="Create your account" subtitle="You must be 18 or older to join" />
      <form onSubmit={handleSubmit((v) => m.mutate(v))} noValidate className="space-y-4">
        {m.isError && <AuthError>{authErrorMessage(m.error)}</AuthError>}
        <Input label="Email" type="email" autoComplete="email" error={errors.email?.message} {...register("email")} />
        <Input label="Username" autoComplete="username" autoCapitalize="none" spellCheck={false} hint="3–24 characters: letters, numbers, underscores" error={errors.username?.message} {...register("username")} />
        <PasswordInput label="Password" autoComplete="new-password" hint="At least 10 characters, with a letter and a number" error={errors.password?.message} {...register("password")} />
        <PasswordInput label="Confirm password" autoComplete="new-password" error={errors.confirm?.message} {...register("confirm")} />
        <div className="space-y-3 pt-1 text-sm">
          <div>
            <label className="flex cursor-pointer gap-3"><input type="checkbox" className={check} {...register("adult")} /><span>I confirm that I am 18 years or older.</span></label>
            {errors.adult && <p role="alert" className="mt-1 text-xs text-accent">{errors.adult.message}</p>}
          </div>
          <div>
            <label className="flex cursor-pointer gap-3">
              <input type="checkbox" className={check} {...register("terms")} />
              <span>I agree to the <Link href="/terms" className="underline">Terms</Link> and <Link href="/privacy" className="underline">Privacy Policy</Link>.</span>
            </label>
            {errors.terms && <p role="alert" className="mt-1 text-xs text-accent">{errors.terms.message}</p>}
          </div>
        </div>
        <Button type="submit" full loading={m.isPending}>Create account</Button>
      </form>
      <p className="mt-6 text-center text-sm text-fg-2">
        Already have an account? <Link href="/login" className="font-semibold text-white hover:underline">Log in</Link>
      </p>
    </>
  );
}

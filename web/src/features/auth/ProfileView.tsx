"use client";
import { SealCheck } from "@phosphor-icons/react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/Button";
import { logout } from "@/services/auth";
import { modals } from "@/store/modal";
import { meKey, useSession } from "./useSession";

export function ProfileView() {
  const { user } = useSession();
  const qc = useQueryClient();
  const router = useRouter();
  const out = useMutation({
    mutationFn: logout,
    onSettled: () => {
      // Drop every cached user-specific query, then mark the session anonymous.
      qc.clear();
      qc.setQueryData(meKey, null);
      router.replace("/");
      router.refresh();
    },
  });
  if (!user) return null;

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <h1 className="sr-only">Profile</h1>
      <section className="flex items-center gap-5 rounded-card border border-line bg-card p-6">
        <span aria-hidden="true" className="grid size-20 shrink-0 place-items-center rounded-full border border-line bg-surface text-2xl font-extrabold">
          {user.displayName.charAt(0).toUpperCase()}
        </span>
        <div className="min-w-0">
          <p className="truncate text-xl font-bold">{user.displayName}</p>
          <p className="truncate text-sm text-fg-2">@{user.username}</p>
          <p className="mt-1 flex items-center gap-1.5 text-xs text-fg-2">
            <SealCheck size={14} weight="fill" aria-hidden="true" />Verified account
          </p>
        </div>
      </section>
      <dl className="divide-y divide-line rounded-card border border-line bg-card text-sm">
        <div className="flex justify-between gap-4 p-4"><dt className="text-fg-2">Email</dt><dd className="truncate">{user.email}</dd></div>
        <div className="flex justify-between gap-4 p-4"><dt className="text-fg-2">Joined</dt><dd>{new Date(user.joinedAt).toLocaleDateString("en", { year: "numeric", month: "long", day: "numeric" })}</dd></div>
      </dl>
      {out.isError && <p role="alert" className="text-sm text-accent">Couldn’t log out on the server. You were signed out on this device.</p>}
      <Button variant="secondary" onClick={async () => { if (await modals.confirm({ title: "Log out?", message: "You’ll need to log in again to access your account.", confirmLabel: "Log out" })) out.mutate(); }} loading={out.isPending}>Log out</Button>
    </div>
  );
}

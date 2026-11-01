"use client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { Input, PasswordInput, Textarea } from "@/components/ui/Input";
import { Toggle } from "@/components/ui/Toggle";
import { authErrorMessage } from "@/features/auth/errors";
import { accountSchema, changePasswordSchema, type AccountValues, type ChangePasswordValues } from "@/features/auth/schemas";
import { meKey, useSession } from "@/features/auth/useSession";
import { changePassword, deleteAccount, logoutAll, updateAccount } from "@/services/settings";
import { modals } from "@/store/modal";
import { useRecentSearches } from "@/store/recent";
import { toast } from "@/store/toast";
import { useUIStore } from "@/store/ui";
import { useSettings, usePatchSettings } from "./useSettings";

export function Section({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  return (
    <section className="rounded-card border border-line bg-card p-5 sm:p-6">
      <h2 className="text-base font-bold">{title}</h2>
      {description && <p className="mt-1 text-sm text-fg-2">{description}</p>}
      <div className="mt-4">{children}</div>
    </section>
  );
}

function ToggleSkeleton() {
  return <div className="space-y-4" aria-hidden="true">{Array.from({ length: 3 }, (_, i) => <div key={i} className="skeleton h-12 rounded-lg" />)}</div>;
}
function SettingsError({ onRetry }: { onRetry: () => void }) {
  return <div role="alert" className="text-sm">Couldn’t load your settings. <button type="button" onClick={onRetry} className="font-semibold text-accent hover:underline">Retry</button></div>;
}

export function PrivacyPanel() {
  const q = useSettings();
  const patch = usePatchSettings();
  const clearRecent = useRecentSearches((s) => s.clear);
  return (
    <div className="space-y-6">
      <Section title="Privacy" description="Control what other people can see about you.">
        {q.isPending ? <ToggleSkeleton /> : q.isError ? <SettingsError onRetry={() => q.refetch()} /> : (
          <div className="divide-y divide-line">
            <Toggle label="Show my subscriptions" description="Let others see which channels you subscribe to." checked={q.data.privacy.showSubscriptions} onChange={(v) => patch.mutate({ privacy: { showSubscriptions: v } })} />
            <Toggle label="Show my liked videos" description="Let others see videos you’ve liked." checked={q.data.privacy.showLikedVideos} onChange={(v) => patch.mutate({ privacy: { showLikedVideos: v } })} />
            <Toggle label="Save watch history" description="Used for Continue watching and recommendations." checked={q.data.privacy.saveWatchHistory} onChange={(v) => patch.mutate({ privacy: { saveWatchHistory: v } })} />
          </div>
        )}
      </Section>
      <Section title="On this device" description="Recent searches are stored only in this browser.">
        <Button variant="secondary" onClick={() => { clearRecent(); toast.success("Recent searches cleared"); }}>Clear recent searches</Button>
      </Section>
    </div>
  );
}

export function NotificationsPanel() {
  const q = useSettings();
  const patch = usePatchSettings();
  const n = q.data?.notifications;
  const set = (k: keyof NonNullable<typeof n>) => (v: boolean) => patch.mutate({ notifications: { [k]: v } });
  return (
    <Section title="Notifications" description="Choose what you want to hear about.">
      {q.isPending ? <ToggleSkeleton /> : q.isError || !n ? <SettingsError onRetry={() => q.refetch()} /> : (
        <div className="divide-y divide-line">
          <Toggle label="New videos from subscriptions" checked={n.newVideo} onChange={set("newVideo")} />
          <Toggle label="Replies to my comments" checked={n.replies} onChange={set("replies")} />
          <Toggle label="New subscribers" checked={n.newSubscriber} onChange={set("newSubscriber")} />
          <Toggle label="System announcements" checked={n.system} onChange={set("system")} />
          <Toggle label="Email notifications" description="Receive important updates by email." checked={n.email} onChange={set("email")} />
        </div>
      )}
    </Section>
  );
}

export function PlaybackPanel() {
  const autoplay = useUIStore((s) => s.autoplayNext);
  const setAutoplay = useUIStore((s) => s.setAutoplayNext);
  return (
    <Section title="Playback" description="Saved on this device.">
      <Toggle label="Autoplay next video" description="Play the next related video when one ends." checked={autoplay} onChange={setAutoplay} />
    </Section>
  );
}

export function AppearancePanel() {
  const collapsed = useUIStore((s) => s.sidebarCollapsed);
  const toggle = useUIStore((s) => s.toggleSidebar);
  return (
    <Section title="Appearance" description="REDXTUBE uses its dark theme. Motion follows your system’s “reduce motion” setting.">
      <Toggle label="Compact sidebar" description="Show icons only in the desktop sidebar." checked={collapsed} onChange={() => toggle()} />
    </Section>
  );
}

export function AccountPanel() {
  const { user } = useSession();
  const qc = useQueryClient();
  const router = useRouter();
  const { register, handleSubmit, formState: { errors, isDirty }, reset } = useForm<AccountValues>({
    resolver: zodResolver(accountSchema), values: { displayName: user?.displayName ?? "", bio: user?.bio ?? "" },
  });
  const save = useMutation({
    mutationFn: (v: AccountValues) => updateAccount({ displayName: v.displayName.trim(), bio: v.bio }),
    onSuccess: (u) => { qc.setQueryData(meKey, u); reset({ displayName: u.displayName, bio: u.bio ?? "" }); toast.success("Account updated"); },
  });
  const delForm = useForm<{ password: string }>({ defaultValues: { password: "" } });
  const del = useMutation({
    mutationFn: (v: { password: string }) => deleteAccount(v.password),
    onSuccess: () => { qc.clear(); qc.setQueryData(meKey, null); toast.success("Your account was deleted"); router.replace("/"); router.refresh(); },
  });

  async function onDelete(v: { password: string }) {
    const ok = await modals.confirm({ title: "Delete your account?", message: "This permanently deletes your account and content. This can’t be undone.", confirmLabel: "Delete account" });
    if (ok) del.mutate(v);
  }
  if (!user) return null;

  return (
    <div className="space-y-6">
      <Section title="Profile">
        <form onSubmit={handleSubmit((v) => save.mutate(v))} noValidate className="space-y-4">
          <Input label="Display name" maxLength={40} error={errors.displayName?.message} {...register("displayName")} />
          <Input label="Username" value={user.username} readOnly hint="Usernames can’t be changed." />
          <Input label="Email" value={user.email} readOnly />
          <Textarea label="Bio" rows={3} maxLength={300} error={errors.bio?.message} {...register("bio")} />
          {save.isError && <p role="alert" className="text-sm text-accent">Couldn’t save your changes. Try again.</p>}
          <Button type="submit" loading={save.isPending} disabled={!isDirty}>Save changes</Button>
        </form>
      </Section>
      <Section title="Delete account" description="Enter your password to permanently delete your account.">
        <form onSubmit={delForm.handleSubmit(onDelete)} className="space-y-4">
          <PasswordInput label="Password" autoComplete="current-password" {...delForm.register("password", { required: true })} />
          {del.isError && <p role="alert" className="text-sm text-accent">{authErrorMessage(del.error)}</p>}
          <Button type="submit" variant="secondary" loading={del.isPending}>Delete account…</Button>
        </form>
      </Section>
    </div>
  );
}

export function SecurityPanel() {
  const qc = useQueryClient();
  const router = useRouter();
  const { register, handleSubmit, reset, formState: { errors } } = useForm<ChangePasswordValues>({
    resolver: zodResolver(changePasswordSchema), defaultValues: { currentPassword: "", password: "", confirm: "" },
  });
  const change = useMutation({
    mutationFn: (v: ChangePasswordValues) => changePassword(v.currentPassword, v.password),
    onSuccess: () => { reset(); toast.success("Password updated"); },
  });
  const out = useMutation({
    mutationFn: logoutAll,
    onSettled: () => { qc.clear(); qc.setQueryData(meKey, null); router.replace("/login"); },
  });

  return (
    <div className="space-y-6">
      <Section title="Change password">
        <form onSubmit={handleSubmit((v) => change.mutate(v))} noValidate className="space-y-4">
          <PasswordInput label="Current password" autoComplete="current-password" error={errors.currentPassword?.message} {...register("currentPassword")} />
          <PasswordInput label="New password" autoComplete="new-password" hint="At least 10 characters, with a letter and a number" error={errors.password?.message} {...register("password")} />
          <PasswordInput label="Confirm new password" autoComplete="new-password" error={errors.confirm?.message} {...register("confirm")} />
          {change.isError && <p role="alert" className="text-sm text-accent">{authErrorMessage(change.error)}</p>}
          <Button type="submit" loading={change.isPending}>Update password</Button>
        </form>
      </Section>
      <Section title="Sessions" description="Sign out of REDXTUBE on every device, including this one.">
        <Button variant="secondary" loading={out.isPending}
          onClick={async () => { if (await modals.confirm({ title: "Log out everywhere?", message: "You’ll be signed out on all devices.", confirmLabel: "Log out everywhere" })) out.mutate(); }}>
          Log out of all devices
        </Button>
      </Section>
    </div>
  );
}

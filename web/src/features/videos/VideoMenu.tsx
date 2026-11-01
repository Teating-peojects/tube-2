"use client";
import { Clock, EyeSlash, Flag, ProhibitInset, ShareNetwork } from "@phosphor-icons/react";
import { Dropdown, type MenuItem } from "@/components/ui/Dropdown";
import { useRequireAuth } from "@/features/auth/useRequireAuth";
import { useToggleWatchLater } from "@/features/library/useWatchLater";
import { markNotInterested, setChannelBlocked } from "@/services/safety";
import { modals } from "@/store/modal";
import { toast } from "@/store/toast";
import type { Video } from "@/types";

export function VideoMenu({ video }: { video: Video }) {
  const requireAuth = useRequireAuth();
  const wl = useToggleWatchLater(video.id);

  const items: MenuItem[] = [
    { label: "Report", icon: <Flag size={18} aria-hidden="true" />, onSelect: () => modals.report({ type: "video", id: video.id }) },
    {
      label: "Not interested", icon: <EyeSlash size={18} aria-hidden="true" />,
      onSelect: async () => {
        if (!requireAuth("Log in to personalize your recommendations.")) return;
        try { await markNotInterested(video.id); toast.success("We’ll show fewer videos like this"); } catch { toast.error("Something went wrong. Try again."); }
      },
    },
    {
      label: `Block ${video.creator.name}`, icon: <ProhibitInset size={18} aria-hidden="true" />, danger: true,
      onSelect: async () => {
        if (!requireAuth("Log in to block creators.")) return;
        const ok = await modals.confirm({ title: `Block ${video.creator.name}?`, message: "You won’t see videos from this creator anymore.", confirmLabel: "Block" });
        if (!ok) return;
        try { await setChannelBlocked(video.creator.id, true); toast.success("Creator blocked"); } catch { toast.error("Couldn’t block this creator. Try again."); }
      },
    },
    { label: wl.saved ? "Remove from Watch Later" : "Save to Watch Later", icon: <Clock size={18} aria-hidden="true" />, onSelect: wl.toggle },
    { label: "Share", icon: <ShareNetwork size={18} aria-hidden="true" />, onSelect: () => modals.share({ url: `${location.origin}/watch/${video.id}`, title: video.title }) },
  ];
  return <Dropdown label={`More options for ${video.title}`} items={items} className="-mr-2" />;
}

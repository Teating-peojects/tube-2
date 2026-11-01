"use client";
import { SquaresFour } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { EmptyState } from "@/components/ui/EmptyState";
import { CreatorCard } from "@/features/creators/CreatorCard";
import { VideoCard } from "@/features/videos/VideoCard";
import { getSubscribedChannels, getSubscriptionFeed } from "@/services/library";
import type { Video } from "@/types";
import { PagedList } from "./PagedList";

export function SubscriptionsView() {
  const channels = useQuery({ queryKey: ["subscriptions", "channels"], queryFn: getSubscribedChannels });
  const none = channels.isSuccess && channels.data.length === 0;

  return (
    <div className="space-y-8">
      <h1 className="text-2xl font-extrabold">Subscriptions</h1>
      {none ? (
        <EmptyState icon={<SquaresFour size={28} />} title="No subscriptions yet" text="Subscribe to creators to see their latest videos here." action={{ label: "Explore creators", href: "/trending" }} />
      ) : (
        <>
          <section aria-label="Subscribed channels">
            <div className="-mx-3 flex gap-3 overflow-x-auto px-3 pb-1 md:mx-0 md:px-0">
              {channels.isPending && Array.from({ length: 4 }, (_, i) => <div key={i} className="skeleton h-36 w-36 shrink-0 rounded-card" aria-hidden="true" />)}
              {channels.isError && <p role="alert" className="text-sm text-fg-2">Couldn’t load channels. <button type="button" onClick={() => channels.refetch()} className="font-semibold text-accent hover:underline">Retry</button></p>}
              {channels.data?.map((c) => <CreatorCard key={c.id} channel={c} />)}
            </div>
          </section>
          <section aria-labelledby="latest-h">
            <h2 id="latest-h" className="mb-4 text-lg font-bold">Latest</h2>
            <PagedList<Video> layout="grid" queryKey={["subscriptions", "feed"]} fetchPage={getSubscriptionFeed} getKey={(v) => v.id}
              empty={<p className="rounded-card border border-line bg-card py-12 text-center text-sm text-fg-2">No new videos from your subscriptions yet.</p>}
              render={(v, i) => <VideoCard video={v} priority={i < 4} />} />
          </section>
        </>
      )}
    </div>
  );
}

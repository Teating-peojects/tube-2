import { notFound } from "next/navigation";
import { loadChannel } from "@/features/channels/load";
import { safeExternalUrl } from "@/lib/auth";
import { formatViews } from "@/utils/format";

export default async function AboutPage({ params }: { params: Promise<{ channelId: string }> }) {
  const { channelId } = await params;
  const c = await loadChannel(channelId);
  if (!c) notFound();
  const links = c.links.map((l) => ({ label: l.label, url: safeExternalUrl(l.url) })).filter((l): l is { label: string; url: string } => Boolean(l.url));
  return (
    <div className="max-w-3xl space-y-6">
      <section aria-labelledby="about-h">
        <h2 id="about-h" className="mb-2 text-lg font-bold">About</h2>
        <p className="whitespace-pre-line text-sm leading-relaxed text-fg-2">{c.description}</p>
      </section>
      <dl className="divide-y divide-line rounded-card border border-line bg-card text-sm">
        <div className="flex justify-between p-4"><dt className="text-fg-2">Joined</dt><dd>{new Date(c.joinedAt).toLocaleDateString("en", { year: "numeric", month: "long", day: "numeric" })}</dd></div>
        <div className="flex justify-between p-4"><dt className="text-fg-2">Total views</dt><dd>{formatViews(c.totalViews)}</dd></div>
        <div className="flex justify-between p-4"><dt className="text-fg-2">Videos</dt><dd>{c.videoCount}</dd></div>
      </dl>
      {links.length > 0 && (
        <ul className="space-y-2 text-sm" aria-label="Links">
          {links.map((l) => <li key={l.url}><a href={l.url} target="_blank" rel="noopener noreferrer nofollow ugc" className="underline hover:text-accent">{l.label}</a></li>)}
        </ul>
      )}
    </div>
  );
}

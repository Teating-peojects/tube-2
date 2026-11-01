import Link from "next/link";

export default function VideoNotFound() {
  return (
    <div className="grid place-items-center gap-3 py-24 text-center">
      <p className="text-5xl font-extrabold text-accent">Video unavailable</p>
      <p className="text-sm text-fg-2">This video was removed, made private, or never existed.</p>
      <Link href="/" className="mt-2 inline-flex h-11 items-center rounded-full bg-accent px-6 text-sm font-semibold hover:bg-accent-hover">Back to Home</Link>
    </div>
  );
}

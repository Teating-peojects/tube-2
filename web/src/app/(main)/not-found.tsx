import Link from "next/link";

export default function NotFound() {
  return (
    <div className="grid place-items-center gap-3 py-24 text-center">
      <p className="text-6xl font-extrabold text-accent">404</p>
      <h1 className="text-xl font-bold">Page not found</h1>
      <p className="text-sm text-fg-2">The page you’re looking for doesn’t exist or was moved.</p>
      <Link href="/" className="mt-2 inline-flex h-11 items-center rounded-full bg-accent px-6 text-sm font-semibold hover:bg-accent-hover">Back to Home</Link>
    </div>
  );
}

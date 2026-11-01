"use client";

export default function GlobalError({ reset }: { error: Error; reset: () => void }) {
  // Never render error.message / stack to users.
  return (
    <div className="grid min-h-dvh place-items-center p-6 text-center">
      <div className="grid gap-3">
        <p className="text-5xl font-extrabold text-accent">Oops</p>
        <h1 className="text-xl font-bold">Something went wrong</h1>
        <p className="text-sm text-fg-2">Please try again in a moment.</p>
        <button type="button" onClick={reset} className="mx-auto h-11 rounded-full bg-accent px-6 text-sm font-semibold hover:bg-accent-hover">Try again</button>
      </div>
    </div>
  );
}

export class UploadError extends Error {
  constructor(public code: "network" | "part_failed" | "aborted", public status?: number) {
    super(code);
    this.name = "UploadError";
  }
}
export type Progress = { loaded: number; total: number; speed: number; eta: number };
export type PutPart = (blob: Blob, url: string, onProgress: (loaded: number) => void, signal: AbortSignal) => Promise<string>;
export type PartDone = { partNumber: number; etag: string };

/** A part upload with no progress for this long is treated as a dead connection (a silent drop never fires `onerror`). */
export const STALL_MS = 60_000;

/**
 * PUT one part to a presigned URL. XHR is used because fetch() has no upload progress. No credentials are sent.
 * If no progress arrives for STALL_MS the request is aborted and reported as a network error, so it is retried.
 */
export const xhrPut: PutPart = (blob, url, onProgress, signal) =>
  new Promise((resolve, reject) => {
    const x = new XMLHttpRequest();
    let stalled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const arm = () => { clearTimeout(timer); timer = setTimeout(() => { stalled = true; x.abort(); }, STALL_MS); };
    x.open("PUT", url);
    x.upload.onprogress = (e) => { arm(); if (e.lengthComputable) onProgress(e.loaded); };
    x.onload = () => {
      clearTimeout(timer);
      if (x.status >= 200 && x.status < 300) resolve((x.getResponseHeader("ETag") ?? "").replace(/"/g, ""));
      else reject(new UploadError("part_failed", x.status));
    };
    x.onerror = () => { clearTimeout(timer); reject(new UploadError("network")); };
    x.onabort = () => { clearTimeout(timer); reject(new UploadError(stalled ? "network" : "aborted")); };
    if (signal.aborted) return reject(new UploadError("aborted"));
    signal.addEventListener("abort", () => x.abort(), { once: true });
    arm();
    x.send(blob);
  });

/** False only when the browser positively reports being offline (server-side rendering and old browsers count as online). */
export const browserOnline = () => typeof navigator === "undefined" || navigator.onLine !== false;

/** Resolves when the browser is back online (immediately if it already is); rejects with "aborted" if the signal fires first. */
export const waitForOnline = (signal: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (browserOnline()) return resolve();
    if (signal.aborted) return reject(new UploadError("aborted"));
    const cleanup = () => { window.removeEventListener("online", on); signal.removeEventListener("abort", off); };
    const on = () => { cleanup(); resolve(); };
    const off = () => { cleanup(); reject(new UploadError("aborted")); };
    window.addEventListener("online", on);
    signal.addEventListener("abort", off, { once: true });
  });

/**
 * A 4xx answer from storage (an expired presigned URL answers 403) fails the same way every time, so retrying it
 * only delays the real recovery. Timeouts, throttling, 5xx and network errors are worth another attempt.
 */
export const isTransient = (e: unknown) =>
  !(e instanceof UploadError && e.code === "part_failed" && e.status !== undefined && e.status >= 400 && e.status < 500 && e.status !== 408 && e.status !== 429);

const sleep = (ms: number, signal: AbortSignal) =>
  new Promise<void>((res, rej) => {
    const t = setTimeout(res, ms);
    signal.addEventListener("abort", () => { clearTimeout(t); rej(new UploadError("aborted")); }, { once: true });
  });

/**
 * Chunked, resumable upload: parts already in `done` are skipped, so calling this again after a failure resumes.
 * Each part is retried with exponential backoff (not for 4xx answers, see `isTransient`). While the browser is
 * offline the upload waits and resumes by itself, without using up retry attempts. Progress, speed and ETA are
 * reported via `onProgress`.
 */
export async function uploadParts(o: {
  file: File; partSize: number; targets: { partNumber: number; url: string }[]; put: PutPart;
  done: Map<number, string>; onProgress: (p: Progress) => void; signal: AbortSignal; concurrency?: number; maxAttempts?: number; backoffMs?: number;
  /** Connectivity gate: `online` says whether to try now, `waitOnline` blocks until it is worth trying, `onWaiting` mirrors that to the UI. */
  online?: () => boolean; waitOnline?: (signal: AbortSignal) => Promise<void>; onWaiting?: (waiting: boolean) => void;
}): Promise<PartDone[]> {
  const { file, partSize, targets, put, done, onProgress, signal } = o;
  const total = file.size;
  const sizeOf = (n: number) => Math.min(partSize, total - (n - 1) * partSize);
  const inflight = new Map<number, number>();
  const samples: { t: number; loaded: number }[] = [];
  let doneBytes = 0;
  for (const [n] of done) doneBytes += sizeOf(n);

  function report() {
    const loaded = doneBytes + [...inflight.values()].reduce((a, b) => a + b, 0);
    const now = performance.now();
    samples.push({ t: now, loaded });
    while (samples.length > 2 && now - samples[0]!.t > 5000) samples.shift();
    const dt = (now - samples[0]!.t) / 1000;
    const speed = dt > 0.5 ? Math.max(0, (loaded - samples[0]!.loaded) / dt) : 0;
    onProgress({ loaded, total, speed, eta: speed > 0 ? (total - loaded) / speed : Infinity });
  }

  const maxAttempts = o.maxAttempts ?? 4;
  let waiting = 0;
  async function gate() {
    if (!o.online || !o.waitOnline || o.online()) return;
    if (waiting++ === 0) o.onWaiting?.(true);
    try { await o.waitOnline(signal); } finally {
      if (--waiting === 0) { o.onWaiting?.(false); samples.length = 0; } // the speed estimate restarts after a pause
    }
  }
  const queue = targets.filter((t) => !done.has(t.partNumber));
  let failed = false;

  async function worker() {
    while (queue.length && !failed) {
      if (signal.aborted) throw new UploadError("aborted");
      const t = queue.shift()!;
      const blob = file.slice((t.partNumber - 1) * partSize, t.partNumber * partSize);
      for (let attempt = 1; ; attempt++) {
        await gate();
        if (signal.aborted) throw new UploadError("aborted");
        try {
          inflight.set(t.partNumber, 0);
          const etag = await put(blob, t.url, (l) => { inflight.set(t.partNumber, l); report(); }, signal);
          inflight.delete(t.partNumber);
          done.set(t.partNumber, etag);
          doneBytes += blob.size;
          report();
          break;
        } catch (e) {
          inflight.delete(t.partNumber);
          if (signal.aborted || (e instanceof UploadError && e.code === "aborted")) throw new UploadError("aborted");
          if (!isTransient(e)) throw e;
          if (o.online && !o.online()) { attempt--; continue; } // the network is gone: wait for it instead of burning an attempt
          if (attempt >= maxAttempts) throw e;
          await sleep((o.backoffMs ?? 1000) * 2 ** (attempt - 1), signal);
        }
      }
    }
  }

  try {
    await Promise.all(Array.from({ length: Math.min(o.concurrency ?? 3, queue.length) }, worker));
  } catch (e) {
    failed = true;
    throw e;
  }
  report();
  return [...done].map(([partNumber, etag]) => ({ partNumber, etag })).sort((a, b) => a.partNumber - b.partNumber);
}

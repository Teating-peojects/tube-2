export class UploadError extends Error {
  constructor(public code: "network" | "part_failed" | "aborted", public status?: number) {
    super(code);
    this.name = "UploadError";
  }
}
export type Progress = { loaded: number; total: number; speed: number; eta: number };
export type PutPart = (blob: Blob, url: string, onProgress: (loaded: number) => void, signal: AbortSignal) => Promise<string>;
export type PartDone = { partNumber: number; etag: string };

/** PUT one part to a presigned URL. XHR is used because fetch() has no upload progress. No credentials are sent. */
export const xhrPut: PutPart = (blob, url, onProgress, signal) =>
  new Promise((resolve, reject) => {
    const x = new XMLHttpRequest();
    x.open("PUT", url);
    x.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded); };
    x.onload = () => (x.status >= 200 && x.status < 300 ? resolve((x.getResponseHeader("ETag") ?? "").replace(/"/g, "")) : reject(new UploadError("part_failed", x.status)));
    x.onerror = () => reject(new UploadError("network"));
    x.onabort = () => reject(new UploadError("aborted"));
    if (signal.aborted) return reject(new UploadError("aborted"));
    signal.addEventListener("abort", () => x.abort(), { once: true });
    x.send(blob);
  });

const sleep = (ms: number, signal: AbortSignal) =>
  new Promise<void>((res, rej) => {
    const t = setTimeout(res, ms);
    signal.addEventListener("abort", () => { clearTimeout(t); rej(new UploadError("aborted")); }, { once: true });
  });

/**
 * Chunked, resumable upload: parts already in `done` are skipped, so calling this again after a failure resumes.
 * Each part is retried with exponential backoff. Progress, speed and ETA are reported via `onProgress`.
 */
export async function uploadParts(o: {
  file: File; partSize: number; targets: { partNumber: number; url: string }[]; put: PutPart;
  done: Map<number, string>; onProgress: (p: Progress) => void; signal: AbortSignal; concurrency?: number; maxAttempts?: number; backoffMs?: number;
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
  const queue = targets.filter((t) => !done.has(t.partNumber));
  let failed = false;

  async function worker() {
    while (queue.length && !failed) {
      if (signal.aborted) throw new UploadError("aborted");
      const t = queue.shift()!;
      const blob = file.slice((t.partNumber - 1) * partSize, t.partNumber * partSize);
      for (let attempt = 1; ; attempt++) {
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

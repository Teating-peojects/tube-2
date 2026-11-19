import { afterEach, describe, expect, it, vi } from "vitest";
import { isTransient, STALL_MS, UploadError, uploadParts, waitForOnline, xhrPut, type PutPart } from "./uploader";

const file = () => new File([new Uint8Array(25)], "v.mp4"); // 3 parts of 10/10/5 bytes
const targets = [1, 2, 3].map((n) => ({ partNumber: n, url: `u${n}` }));
const base = { file: file(), partSize: 10, targets, onProgress: () => {}, backoffMs: 0 };
const ok: PutPart = async (_b, url) => `etag-${url}`;

describe("uploadParts", () => {
  it("uploads every part and returns them in order", async () => {
    const parts = await uploadParts({ ...base, put: ok, done: new Map(), signal: new AbortController().signal });
    expect(parts).toEqual([{ partNumber: 1, etag: "etag-u1" }, { partNumber: 2, etag: "etag-u2" }, { partNumber: 3, etag: "etag-u3" }]);
  });

  it("reports progress up to the full size", async () => {
    const seen: number[] = [];
    await uploadParts({ ...base, put: ok, done: new Map(), signal: new AbortController().signal, onProgress: (p) => seen.push(p.loaded) });
    expect(seen.at(-1)).toBe(25);
  });

  it("resumes: parts that are already done are not uploaded again", async () => {
    const put = vi.fn(ok);
    const done = new Map([[1, "old1"], [3, "old3"]]);
    const parts = await uploadParts({ ...base, put, done, signal: new AbortController().signal });
    expect(put).toHaveBeenCalledTimes(1);
    expect(parts.map((p) => p.etag)).toEqual(["old1", "etag-u2", "old3"]);
  });

  it("retries a failing part and then succeeds", async () => {
    let calls = 0;
    const flaky: PutPart = async (_b, url) => { if (url === "u2" && ++calls < 3) throw new UploadError("network"); return `etag-${url}`; };
    const parts = await uploadParts({ ...base, put: flaky, done: new Map(), signal: new AbortController().signal, maxAttempts: 4 });
    expect(calls).toBe(3);
    expect(parts).toHaveLength(3);
  });

  it("gives up after maxAttempts and keeps finished parts for a later resume", async () => {
    const done = new Map<number, string>();
    const bad: PutPart = async (_b, url) => { if (url === "u2") throw new UploadError("part_failed", 500); return `etag-${url}`; };
    await expect(uploadParts({ ...base, put: bad, done, signal: new AbortController().signal, maxAttempts: 2, concurrency: 1 })).rejects.toBeInstanceOf(UploadError);
    expect(done.has(1)).toBe(true);
  });

  it("stops when aborted", async () => {
    const ac = new AbortController();
    const slow: PutPart = (_b, _u, _p, signal) => new Promise((_res, rej) => signal.addEventListener("abort", () => rej(new UploadError("aborted"))));
    const run = uploadParts({ ...base, put: slow, done: new Map(), signal: ac.signal });
    ac.abort();
    await expect(run).rejects.toMatchObject({ code: "aborted" });
  });
});

describe("uploadParts: which failures are retried", () => {
  const failWith = (status: number, calls: { n: number }): PutPart => async (_b, url) => { if (url === "u2") { calls.n++; throw new UploadError("part_failed", status); } return `etag-${url}`; };
  const run = (put: PutPart, maxAttempts = 4) => uploadParts({ ...base, put, done: new Map(), signal: new AbortController().signal, maxAttempts, concurrency: 1 });

  it("does not retry a 4xx answer: an expired presigned URL (403) fails the same way every time", async () => {
    for (const status of [400, 403, 404]) {
      const calls = { n: 0 };
      await expect(run(failWith(status, calls))).rejects.toMatchObject({ code: "part_failed", status });
      expect(calls.n, `status ${status}`).toBe(1);
    }
  });
  it("retries timeouts, throttling and server errors", async () => {
    for (const status of [408, 429, 500, 503]) {
      const calls = { n: 0 };
      await expect(run(failWith(status, calls), 3)).rejects.toMatchObject({ status });
      expect(calls.n, `status ${status}`).toBe(3);
    }
  });
  it("does not retry a missing ETag part by part", async () => {
    let calls = 0;
    const put: PutPart = async () => { calls++; throw new UploadError("no_etag", 200); };
    await expect(uploadParts({ ...base, put, done: new Map(), signal: new AbortController().signal, maxAttempts: 4, concurrency: 1 })).rejects.toMatchObject({ code: "no_etag" });
    expect(calls).toBe(1);
  });
  it("isTransient", () => {
    expect(isTransient(new UploadError("network"))).toBe(true);
    expect(isTransient(new UploadError("part_failed", 500))).toBe(true);
    expect(isTransient(new UploadError("part_failed", 403))).toBe(false);
    expect(isTransient(new Error("x"))).toBe(true);
  });
});

describe("uploadParts: offline handling", () => {
  it("waits while the browser is offline and resumes by itself, without using retry attempts", async () => {
    const net = { online: false };
    let wake!: () => void;
    const comeBack = new Promise<void>((res) => { wake = res; }); // one event wakes every waiting worker
    const waiting: boolean[] = [];
    const put = vi.fn(ok);
    const run = uploadParts({
      ...base, put, done: new Map(), signal: new AbortController().signal, maxAttempts: 1,
      online: () => net.online, waitOnline: () => comeBack, onWaiting: (w) => waiting.push(w),
    });
    await Promise.resolve();
    expect(put).not.toHaveBeenCalled(); // nothing is sent while offline
    expect(waiting).toEqual([true]);
    net.online = true;
    wake();
    const parts = await run;
    expect(parts).toHaveLength(3);
    expect(waiting).toEqual([true, false]);
  });

  it("a connection lost mid-part does not use up the attempt: the part is sent again once online", async () => {
    const net = { online: true };
    let calls = 0;
    const put: PutPart = async (_b, url) => {
      if (url === "u2" && ++calls === 1) { net.online = false; throw new UploadError("network"); }
      return `etag-${url}`;
    };
    const parts = await uploadParts({
      ...base, put, done: new Map(), signal: new AbortController().signal, maxAttempts: 1, concurrency: 1,
      online: () => net.online, waitOnline: async () => { net.online = true; },
    });
    expect(calls).toBe(2);
    expect(parts.map((p) => p.etag)).toEqual(["etag-u1", "etag-u2", "etag-u3"]);
  });

  it("a network error while online still counts as an attempt", async () => {
    let calls = 0;
    const put: PutPart = async () => { calls++; throw new UploadError("network"); };
    await expect(uploadParts({ ...base, put, done: new Map(), signal: new AbortController().signal, maxAttempts: 2, concurrency: 1, online: () => true, waitOnline: async () => {} }))
      .rejects.toMatchObject({ code: "network" });
    expect(calls).toBe(2);
  });

  it("stops waiting when the upload is cancelled", async () => {
    const ac = new AbortController();
    const off = vi.spyOn(window.navigator, "onLine", "get").mockReturnValue(false);
    const run = uploadParts({ ...base, put: ok, done: new Map(), signal: ac.signal, online: () => false, waitOnline: waitForOnline });
    await Promise.resolve();
    ac.abort();
    await expect(run).rejects.toMatchObject({ code: "aborted" });
    off.mockRestore();
  });
});

describe("waitForOnline", () => {
  afterEach(() => vi.restoreAllMocks());
  it("resolves immediately when online", async () => {
    vi.spyOn(window.navigator, "onLine", "get").mockReturnValue(true);
    await expect(waitForOnline(new AbortController().signal)).resolves.toBeUndefined();
  });
  it("waits for the online event", async () => {
    vi.spyOn(window.navigator, "onLine", "get").mockReturnValue(false);
    let resolved = false;
    const p = waitForOnline(new AbortController().signal).then(() => { resolved = true; });
    await Promise.resolve();
    expect(resolved).toBe(false);
    window.dispatchEvent(new Event("online"));
    await p;
    expect(resolved).toBe(true);
  });
  it("rejects with aborted when cancelled, and leaves no listener behind", async () => {
    vi.spyOn(window.navigator, "onLine", "get").mockReturnValue(false);
    const add = vi.spyOn(window, "addEventListener");
    const remove = vi.spyOn(window, "removeEventListener");
    const ac = new AbortController();
    const p = waitForOnline(ac.signal);
    ac.abort();
    await expect(p).rejects.toMatchObject({ code: "aborted" });
    expect(remove).toHaveBeenCalledWith("online", add.mock.calls.find((c) => c[0] === "online")![1]);
  });
});

class FakeXHR {
  static last: FakeXHR;
  upload: { onprogress: ((e: { loaded: number; lengthComputable: boolean }) => void) | null } = { onprogress: null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  status = 0;
  headers: Record<string, string> = {};
  method = "";
  url = "";
  sent: Blob | undefined;
  aborted = false;
  open(m: string, u: string) { this.method = m; this.url = u; }
  send(b: Blob) { this.sent = b; FakeXHR.last = this; }
  abort() { this.aborted = true; this.onabort?.(); }
  getResponseHeader(n: string) { return this.headers[n.toLowerCase()] ?? null; }
}

describe("xhrPut", () => {
  const put = (signal = new AbortController().signal, onProgress = vi.fn()) => xhrPut(new Blob(["abc"]), "https://s3.test/part?sig=1", onProgress, signal);
  const setup = () => { vi.useFakeTimers(); vi.stubGlobal("XMLHttpRequest", FakeXHR); };
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

  it("PUTs the blob and resolves with the ETag without its quotes", async () => {
    setup();
    const p = put();
    const x = FakeXHR.last;
    expect(x.method).toBe("PUT");
    expect(x.url).toBe("https://s3.test/part?sig=1");
    x.status = 200;
    x.headers.etag = '"abc123"';
    x.onload!();
    await expect(p).resolves.toBe("abc123");
  });
  it("fails clearly when the browser cannot read the ETag (bucket CORS does not expose it)", async () => {
    setup();
    const p = put();
    FakeXHR.last.status = 200; // the part arrived, but no ETag header is readable
    FakeXHR.last.onload!();
    await expect(p).rejects.toMatchObject({ code: "no_etag", status: 200 });
    expect(isTransient(new UploadError("no_etag", 200))).toBe(false);
  });
  it("reports progress", async () => {
    setup();
    const onProgress = vi.fn();
    const p = put(undefined, onProgress);
    FakeXHR.last.upload.onprogress!({ loaded: 2, lengthComputable: true });
    FakeXHR.last.status = 200;
    FakeXHR.last.headers.etag = '"e1"';
    FakeXHR.last.onload!();
    await p;
    expect(onProgress).toHaveBeenCalledWith(2);
  });
  it("rejects with the status on a non-2xx answer and with network on a transport error", async () => {
    setup();
    let p = put();
    FakeXHR.last.status = 403;
    FakeXHR.last.onload!();
    await expect(p).rejects.toMatchObject({ code: "part_failed", status: 403 });
    p = put();
    FakeXHR.last.onerror!();
    await expect(p).rejects.toMatchObject({ code: "network" });
  });
  it("treats a connection that stops making progress as a network error", async () => {
    setup();
    const p = put();
    const assertion = expect(p).rejects.toMatchObject({ code: "network" });
    await vi.advanceTimersByTimeAsync(STALL_MS - 1);
    expect(FakeXHR.last.aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(2);
    await assertion;
    expect(FakeXHR.last.aborted).toBe(true);
  });
  it("progress keeps the watchdog from firing", async () => {
    setup();
    const p = put();
    const assertion = expect(p).rejects.toMatchObject({ code: "network" });
    await vi.advanceTimersByTimeAsync(STALL_MS - 10_000);
    FakeXHR.last.upload.onprogress!({ loaded: 1, lengthComputable: true });
    await vi.advanceTimersByTimeAsync(STALL_MS - 10_000);
    expect(FakeXHR.last.aborted).toBe(false); // alive: only STALL_MS - 10 s since the last progress
    await vi.advanceTimersByTimeAsync(11_000);
    await assertion;
    expect(FakeXHR.last.aborted).toBe(true);
  });
  it("a cancel is reported as aborted, not as a network error, and stops the watchdog", async () => {
    setup();
    const ac = new AbortController();
    const p = put(ac.signal);
    ac.abort();
    await expect(p).rejects.toMatchObject({ code: "aborted" });
    expect(vi.getTimerCount()).toBe(0);
  });
  it("does not send anything when already aborted", async () => {
    setup();
    FakeXHR.last = undefined as unknown as FakeXHR;
    const ac = new AbortController();
    ac.abort();
    await expect(put(ac.signal)).rejects.toMatchObject({ code: "aborted" });
    expect(FakeXHR.last).toBeUndefined();
  });
});

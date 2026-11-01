import { describe, expect, it, vi } from "vitest";
import { UploadError, uploadParts, type PutPart } from "./uploader";

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

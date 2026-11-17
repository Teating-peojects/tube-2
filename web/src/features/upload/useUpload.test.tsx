import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UploadError } from "@/lib/uploader";
import { ApiError } from "@/services/api";
import * as svc from "@/services/upload";
import type { UploadInit } from "@/types";
import { classify, useUpload } from "./useUpload";

vi.mock("@/services/upload", () => ({
  initUpload: vi.fn(), completeUpload: vi.fn(), abortUpload: vi.fn(), retryUpload: vi.fn(), getUploadStatus: vi.fn(), putPart: vi.fn(),
}));
vi.mock("@/lib/realtime", () => ({ useRealtime: () => {} }));

const m = vi.mocked(svc);
const file = () => new File([new Uint8Array(25)], "v.mp4", { type: "video/mp4" }); // 3 parts of 10/10/5 bytes
const session = (id: string, prefix: string): UploadInit => ({ uploadId: id, videoId: `video-${id}`, partSize: 10, parts: [1, 2, 3].map((n) => ({ partNumber: n, url: `${prefix}${n}` })) });
const deferred = <T,>() => {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>
);
const setOnline = (online: boolean) => vi.spyOn(window.navigator, "onLine", "get").mockReturnValue(online);
const okPut = async (_b: Blob, url: string) => `etag-${url}`;

beforeEach(() => {
  vi.resetAllMocks();
  m.initUpload.mockResolvedValue(session("u1", "u"));
  m.putPart.mockImplementation(okPut);
  m.completeUpload.mockResolvedValue(undefined);
  m.abortUpload.mockResolvedValue(undefined);
  m.getUploadStatus.mockResolvedValue({ status: "processing", progress: 10, thumbnails: [] });
  setOnline(true);
});
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

const phase = (r: { current: ReturnType<typeof useUpload> }) => r.current.phase;

describe("useUpload", () => {
  it("uploads every part, completes the upload and follows the processing status", async () => {
    const { result } = renderHook(() => useUpload(), { wrapper });
    act(() => result.current.start(file()));
    await waitFor(() => expect(phase(result)).toBe("processing"));
    expect(m.initUpload).toHaveBeenCalledWith({ filename: "v.mp4", size: 25, contentType: "video/mp4" });
    expect(m.completeUpload).toHaveBeenCalledWith("u1", [{ partNumber: 1, etag: "etag-u1" }, { partNumber: 2, etag: "etag-u2" }, { partNumber: 3, etag: "etag-u3" }]);
    expect(result.current.session).toEqual({ uploadId: "u1", videoId: "video-u1" });
    expect(result.current.progress.loaded).toBe(25);
  });

  it("cancelling while the session is being created closes that session and stays cancelled", async () => {
    const init = deferred<UploadInit>();
    m.initUpload.mockReturnValue(init.promise);
    const { result } = renderHook(() => useUpload(), { wrapper });
    act(() => result.current.start(file()));
    expect(phase(result)).toBe("preparing");
    act(() => result.current.cancel());
    expect(phase(result)).toBe("cancelled");
    await act(async () => { init.resolve(session("u1", "u")); await Promise.resolve(); });
    expect(m.abortUpload).toHaveBeenCalledWith("u1"); // the session that arrived too late must not stay open
    expect(m.putPart).not.toHaveBeenCalled();
    expect(phase(result)).toBe("cancelled"); // and the UI must not jump back to "uploading"
  });

  it("cancelling an upload in progress aborts the session", async () => {
    const hang = deferred<string>();
    m.putPart.mockReturnValue(hang.promise);
    const { result } = renderHook(() => useUpload(), { wrapper });
    act(() => result.current.start(file()));
    await waitFor(() => expect(phase(result)).toBe("uploading"));
    act(() => result.current.cancel());
    expect(m.abortUpload).toHaveBeenCalledWith("u1");
    expect(phase(result)).toBe("cancelled");
  });

  describe("expired presigned URLs / session", () => {
    const expiredUrls = (prefix: string) => async (_b: Blob, url: string) => {
      if (url.startsWith(prefix)) throw new UploadError("part_failed", 403);
      return `etag-${url}`;
    };

    it("reopens the same upload through the retry endpoint and uploads again from the start", async () => {
      m.putPart.mockImplementation(expiredUrls("u"));
      m.retryUpload.mockResolvedValue(session("u1", "r"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("processing"));
      expect(m.retryUpload).toHaveBeenCalledTimes(1);
      expect(m.retryUpload).toHaveBeenCalledWith("u1");
      expect(m.initUpload).toHaveBeenCalledTimes(1); // no second session
      expect(m.abortUpload).not.toHaveBeenCalled();
      expect(m.completeUpload).toHaveBeenCalledWith("u1", [{ partNumber: 1, etag: "etag-r1" }, { partNumber: 2, etag: "etag-r2" }, { partNumber: 3, etag: "etag-r3" }]);
      expect(result.current.session).toEqual({ uploadId: "u1", videoId: "video-u1" });
    });

    it("a 403 is not retried part by part: recovery starts at once", async () => {
      m.putPart.mockImplementation(expiredUrls("u"));
      m.retryUpload.mockResolvedValue(session("u1", "r"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("processing"));
      const failed = m.putPart.mock.calls.filter(([, url]) => String(url).startsWith("u"));
      expect(failed.length).toBeLessThanOrEqual(3); // at most one try per concurrent part, no backoff rounds
    });

    it("starts a new session when the old one is still open or gone, after closing the old one", async () => {
      m.putPart.mockImplementation(expiredUrls("u"));
      m.retryUpload.mockRejectedValue(new ApiError(409, "", "upload_active"));
      m.initUpload.mockResolvedValueOnce(session("u1", "u")).mockResolvedValueOnce(session("u2", "n"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("processing"));
      expect(m.abortUpload).toHaveBeenCalledWith("u1");
      expect(m.abortUpload.mock.invocationCallOrder[0]).toBeLessThan(m.initUpload.mock.invocationCallOrder[1]!); // frees the open-upload slot first
      expect(m.completeUpload).toHaveBeenCalledWith("u2", expect.any(Array));
      expect(result.current.session).toEqual({ uploadId: "u2", videoId: "video-u2" });
    });

    it("keeps the old session when the recovery itself fails for a transient reason (offline, server error)", async () => {
      m.putPart.mockImplementation(expiredUrls("u"));
      m.retryUpload.mockRejectedValue(new ApiError(0, "", "network"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      expect(m.abortUpload).not.toHaveBeenCalled();
      expect(m.initUpload).toHaveBeenCalledTimes(1);
      expect(result.current.error).toMatch(/Connection lost/);
    });

    it("recovers when the session closed between the last part and complete", async () => {
      m.completeUpload.mockRejectedValueOnce(new ApiError(409, "", "upload_closed")).mockResolvedValue(undefined);
      m.retryUpload.mockResolvedValue(session("u1", "r"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("processing"));
      expect(m.retryUpload).toHaveBeenCalledWith("u1");
      expect(m.completeUpload).toHaveBeenCalledTimes(2);
    });

    it("recovers only once per attempt: a second expiry is shown to the user", async () => {
      m.putPart.mockRejectedValue(new UploadError("part_failed", 403));
      m.retryUpload.mockResolvedValue(session("u1", "r"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      expect(m.retryUpload).toHaveBeenCalledTimes(1);
      expect(result.current.error).toMatch(/link expired/);
    });

    it("cancelling during the recovery closes whatever the recovery opened", async () => {
      const retry = deferred<UploadInit>();
      m.putPart.mockImplementation(expiredUrls("u"));
      m.retryUpload.mockReturnValue(retry.promise);
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(m.retryUpload).toHaveBeenCalled());
      act(() => result.current.cancel());
      m.abortUpload.mockClear();
      await act(async () => { retry.resolve(session("u1", "r")); await Promise.resolve(); await Promise.resolve(); });
      expect(m.abortUpload).toHaveBeenCalledWith("u1");
      expect(phase(result)).toBe("cancelled");
    });
  });

  describe("failures the server decides", () => {
    it("a file the server refuses cannot be retried: choose another file", async () => {
      m.completeUpload.mockRejectedValue(new ApiError(422, "", "invalid_file"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      expect(result.current.canRetry).toBe(false);
      expect(result.current.error).toMatch(/couldn’t be read as a video/);
      act(() => result.current.reset());
      expect(phase(result)).toBe("idle");
      expect(result.current.canRetry).toBe(true);
    });

    it("an incomplete upload is retried with a fresh session (the server closed the old one)", async () => {
      m.completeUpload.mockRejectedValueOnce(new ApiError(422, "", "size_mismatch")).mockResolvedValue(undefined);
      m.initUpload.mockResolvedValueOnce(session("u1", "u")).mockResolvedValueOnce(session("u2", "n"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      expect(result.current.canRetry).toBe(true);
      act(() => { void result.current.retry(); });
      await waitFor(() => expect(phase(result)).toBe("processing"));
      expect(m.initUpload).toHaveBeenCalledTimes(2);
      expect(m.completeUpload).toHaveBeenLastCalledWith("u2", expect.any(Array));
    });

    it("mismatching parts are uploaded again in the same session", async () => {
      m.completeUpload.mockRejectedValueOnce(new ApiError(422, "", "parts_mismatch")).mockResolvedValue(undefined);
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      m.putPart.mockClear();
      act(() => { void result.current.retry(); });
      await waitFor(() => expect(phase(result)).toBe("processing"));
      expect(m.initUpload).toHaveBeenCalledTimes(1);
      expect(m.putPart).toHaveBeenCalledTimes(3); // all parts again, none skipped
    });

    it("too many unfinished uploads: a clear message, and Retry asks for a session again", async () => {
      m.initUpload.mockRejectedValueOnce(new ApiError(422, "", "limit_reached")).mockResolvedValue(session("u1", "u"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      expect(result.current.error).toMatch(/too many unfinished uploads/);
      act(() => { void result.current.retry(); });
      await waitFor(() => expect(phase(result)).toBe("processing"));
    });

    it("a missing ETag (storage CORS) is reported as a server setup problem, once, without part-by-part retries", async () => {
      m.putPart.mockRejectedValue(new UploadError("no_etag", 200));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      expect(result.current.error).toMatch(/not set up correctly on the server/);
      expect(m.putPart.mock.calls.length).toBeLessThanOrEqual(3); // one try per concurrent part, no backoff rounds
      expect(m.retryUpload).not.toHaveBeenCalled(); // not an expired link, so no recovery attempt either
    });

    it("never shows the server's own text", () => {
      const e = new ApiError(500, "Internal: pq: relation does not exist", "boom");
      expect(classify(e).message).toBe("The upload failed. Retry to continue where you left off.");
    });
  });

  describe("connection loss", () => {
    it("pauses while offline and continues by itself when the connection is back", async () => {
      setOnline(false);
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(result.current.offline).toBe(true));
      expect(phase(result)).toBe("uploading");
      expect(m.putPart).not.toHaveBeenCalled();
      setOnline(true);
      act(() => { window.dispatchEvent(new Event("online")); });
      await waitFor(() => expect(phase(result)).toBe("processing"));
      expect(result.current.offline).toBe(false);
      expect(m.initUpload).toHaveBeenCalledTimes(1);
    });

    it("after a lost connection that exhausted the retries, coming back online resumes and keeps finished parts", async () => {
      vi.useFakeTimers();
      let healthy = false;
      m.putPart.mockImplementation(async (b, url) => {
        if (url === "u3" && !healthy) throw new UploadError("network");
        return okPut(b, url);
      });
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await act(async () => { await vi.advanceTimersByTimeAsync(10_000); }); // 1s + 2s + 4s of backoff
      expect(phase(result)).toBe("failed");
      expect(result.current.error).toMatch(/Connection lost/);
      expect(result.current.canRetry).toBe(true);
      healthy = true;
      m.putPart.mockClear();
      await act(async () => { window.dispatchEvent(new Event("online")); await vi.advanceTimersByTimeAsync(50); });
      expect(phase(result)).toBe("processing");
      expect(m.initUpload).toHaveBeenCalledTimes(1); // same session
      expect(m.putPart.mock.calls.map(([, url]) => url)).toEqual(["u3"]); // only the missing part
    });

    it("a failure that is not a lost connection does not resume by itself", async () => {
      m.completeUpload.mockRejectedValue(new ApiError(503, "", "storage_unavailable"));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("failed"));
      m.completeUpload.mockClear();
      act(() => { window.dispatchEvent(new Event("online")); });
      await new Promise((r) => setTimeout(r, 20));
      expect(m.completeUpload).not.toHaveBeenCalled();
    });
  });

  describe("leaving the page", () => {
    it("asks for confirmation while uploading and stops asking once cancelled", async () => {
      m.putPart.mockReturnValue(new Promise(() => {}));
      const { result } = renderHook(() => useUpload(), { wrapper });
      act(() => result.current.start(file()));
      await waitFor(() => expect(phase(result)).toBe("uploading"));
      const ev = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(ev);
      expect(ev.defaultPrevented).toBe(true);
      act(() => result.current.cancel());
      const after = new Event("beforeunload", { cancelable: true });
      window.dispatchEvent(after);
      expect(after.defaultPrevented).toBe(false);
    });
  });

  it("starting over does not abort an upload the server already owns", async () => {
    const { result } = renderHook(() => useUpload(), { wrapper });
    act(() => result.current.start(file()));
    await waitFor(() => expect(phase(result)).toBe("processing"));
    act(() => result.current.reset());
    expect(m.abortUpload).not.toHaveBeenCalled();
    expect(phase(result)).toBe("idle");
  });
});

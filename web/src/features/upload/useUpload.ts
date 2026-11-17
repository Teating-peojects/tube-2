"use client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { useRealtime } from "@/lib/realtime";
import { browserOnline, UploadError, uploadParts, waitForOnline, type Progress } from "@/lib/uploader";
import { ApiError } from "@/services/api";
import { abortUpload, completeUpload, getUploadStatus, initUpload, putPart, retryUpload } from "@/services/upload";
import type { UploadInit, UploadPhase } from "@/types";

type ClientPhase = "idle" | "preparing" | "uploading" | "uploaded" | "failed" | "cancelled";
const EMPTY: Progress = { loaded: 0, total: 0, speed: 0, eta: Infinity };

/** How the next "Retry" must behave after a failure. */
type Next = "resume" | "restart" | "reupload" | "never";
type Failure = { message: string; next: Next; network: boolean };

/** Maps a failure to a user-facing message (never the server's text) and to what Retry has to do. */
export function classify(e: unknown): Failure {
  const code = e instanceof ApiError ? e.code : undefined;
  const status = e instanceof ApiError ? e.status : undefined;
  if (e instanceof UploadError && e.code === "network") return { message: "Connection lost. Retry to continue where you left off.", next: "resume", network: true };
  if (e instanceof ApiError && (e.status === 0 || code === "network")) return { message: "Connection lost. Retry to continue where you left off.", next: "resume", network: true };
  if (e instanceof UploadError && e.code === "no_etag") return { message: "Uploads are not set up correctly on the server (storage CORS). Please try again later.", next: "resume", network: false };
  // The server looked at the finished upload and refused it; the session is closed, so the file has to change.
  if (code === "invalid_file") return { message: "This file couldn’t be read as a video. Choose another file.", next: "never", network: false };
  if (code === "size_mismatch") return { message: "The upload arrived incomplete. Retry to upload it again.", next: "restart", network: false };
  if (code === "upload_closed" || code === "upload_not_found") return { message: "This upload expired. Retry to start it again.", next: "restart", network: false };
  if (code === "parts_mismatch") return { message: "Some parts didn’t arrive intact. Retry to upload them again.", next: "reupload", network: false };
  if (code === "validation_failed" || status === 413 || code === "too_large") return { message: "This file isn’t accepted. Check its format and size, or choose another file.", next: "never", network: false };
  if (code === "limit_reached") return { message: "You have too many unfinished uploads. Finish or cancel one, then retry.", next: "resume", network: false };
  if (status === 429) return { message: "Too many uploads right now. Please try again shortly.", next: "resume", network: false };
  if (status === 401 || status === 403) return { message: "You’re not allowed to upload right now. Log in again and retry.", next: "resume", network: false };
  if (code === "storage_unavailable" || code === "uploads_disabled" || status === 502 || status === 503) return { message: "Uploads are temporarily unavailable. Try again in a few minutes.", next: "resume", network: false };
  if (e instanceof UploadError && e.code === "part_failed" && e.status === 403) return { message: "The upload link expired. Retry to continue.", next: "restart", network: false };
  return { message: "The upload failed. Retry to continue where you left off.", next: "resume", network: false };
}

/** An expired presigned URL answers 403; a session that closed meanwhile answers upload_closed at complete. */
const sessionExpired = (e: unknown) =>
  (e instanceof UploadError && e.code === "part_failed" && e.status === 403) || (e instanceof ApiError && e.code === "upload_closed");

/**
 * Brings an upload whose session or URLs expired back to life. The backend reopens an expired upload under the same
 * ids (retry); a session that is still open (only the URLs expired), cancelled or gone is replaced by a new one.
 * Anything else (offline, 5xx, rate limit) is rethrown so the old session is not thrown away for nothing.
 */
async function recover(old: UploadInit, f: File): Promise<UploadInit> {
  try {
    return await retryUpload(old.uploadId);
  } catch (e) {
    if (!(e instanceof ApiError) || (e.status !== 409 && e.status !== 404)) throw e;
    await abortUpload(old.uploadId).catch(() => {}); // free the open-upload slot before asking for a new one
    return initUpload({ filename: f.name, size: f.size, contentType: f.type || "video/mp4" });
  }
}

/** Chunked, resumable upload + server-side processing status. */
export function useUpload() {
  const qc = useQueryClient();
  const [client, setClient] = useState<ClientPhase>("idle");
  const [progress, setProgress] = useState<Progress>(EMPTY);
  const [error, setError] = useState<string | null>(null);
  const [canRetry, setCanRetry] = useState(true);
  const [offline, setOffline] = useState(false); // paused until the connection is back
  const [session, setSession] = useState<{ uploadId: string; videoId: string } | null>(null);
  const [fileInfo, setFileInfo] = useState<{ name: string; size: number } | null>(null);

  const file = useRef<File | null>(null);
  const abort = useRef<AbortController | null>(null);
  const init = useRef<UploadInit | null>(null);
  const done = useRef(new Map<number, string>()); // completed parts (kept for resume)
  const generation = useRef(0); // bumped by start/cancel/reset: results of an older run are ignored
  const running = useRef(false);
  const lastFailure = useRef<Failure | null>(null);
  const clientRef = useRef(client);
  clientRef.current = client;

  const run = useCallback(async () => {
    const f = file.current;
    if (!f || running.current) return;
    running.current = true;
    const ac = new AbortController();
    abort.current = ac;
    const gen = generation.current;
    const stale = () => ac.signal.aborted || gen !== generation.current;
    setError(null);
    lastFailure.current = null;
    try {
      if (!init.current) {
        setClient("preparing");
        const res = await initUpload({ filename: f.name, size: f.size, contentType: f.type || "video/mp4" });
        if (stale()) { // cancelled while the session was being created: do not leave it open on the server
          void abortUpload(res.uploadId).catch(() => {});
          return;
        }
        init.current = res;
        setSession({ uploadId: res.uploadId, videoId: res.videoId });
      }
      setClient("uploading");
      let recovered = false;
      for (;;) {
        const i = init.current!;
        try {
          const parts = await uploadParts({
            file: f, partSize: i.partSize, targets: i.parts, put: putPart, done: done.current, onProgress: setProgress, signal: ac.signal,
            online: browserOnline, waitOnline: waitForOnline, onWaiting: setOffline,
          });
          await completeUpload(i.uploadId, parts);
          break;
        } catch (e) {
          if (stale() || recovered || !sessionExpired(e)) throw e;
          recovered = true; // one automatic recovery per attempt; if it does not help, the user decides
          const next = await recover(i, f);
          if (stale()) { // cancelled while recovering: whatever recovery opened must not stay open
            void abortUpload(next.uploadId).catch(() => {});
            return;
          }
          init.current = next;
          done.current = new Map(); // parts of the old multipart upload are gone
          setSession({ uploadId: next.uploadId, videoId: next.videoId });
          setProgress({ ...EMPTY, total: f.size });
        }
      }
      setClient("uploaded");
    } catch (e) {
      if (stale()) return;
      const failure = classify(e);
      lastFailure.current = failure;
      if (failure.next === "restart" || failure.next === "never") { init.current = null; done.current = new Map(); }
      if (failure.next === "reupload") done.current = new Map();
      setCanRetry(failure.next !== "never");
      setClient("failed");
      setError(failure.message);
    } finally {
      running.current = false;
      if (gen === generation.current) setOffline(false);
    }
  }, []);

  const start = useCallback((f: File) => {
    generation.current++;
    abort.current?.abort();
    running.current = false;
    file.current = f;
    init.current = null;
    done.current = new Map();
    lastFailure.current = null;
    setFileInfo({ name: f.name, size: f.size });
    setProgress({ ...EMPTY, total: f.size });
    setSession(null);
    setCanRetry(true);
    setOffline(false);
    void run();
  }, [run]);

  const cancel = useCallback(() => {
    generation.current++;
    abort.current?.abort();
    running.current = false;
    if (init.current) void abortUpload(init.current.uploadId).catch(() => {});
    init.current = null;
    done.current = new Map();
    setOffline(false);
    setCanRetry(true);
    setClient("cancelled");
  }, []);

  const reset = useCallback(() => {
    generation.current++;
    abort.current?.abort();
    running.current = false;
    // An upload that never finished is cancelled here. A finished one (processing, failed, published) belongs to the
    // server now and is not touched.
    if (init.current && clientRef.current !== "uploaded") void abortUpload(init.current.uploadId).catch(() => {});
    file.current = null;
    init.current = null;
    done.current = new Map();
    lastFailure.current = null;
    setFileInfo(null);
    setSession(null);
    setProgress(EMPTY);
    setError(null);
    setCanRetry(true);
    setOffline(false);
    setClient("idle");
  }, []);

  // After the connection was lost and came back, continue by itself (finished parts are kept).
  useEffect(() => {
    if (client !== "failed" || !lastFailure.current?.network || !canRetry) return;
    const resume = () => void run();
    window.addEventListener("online", resume, { once: true });
    return () => window.removeEventListener("online", resume);
  }, [client, canRetry, run]);

  const statusKey = ["upload-status", session?.uploadId];
  const status = useQuery({
    queryKey: statusKey,
    queryFn: () => getUploadStatus(session!.uploadId),
    enabled: client === "uploaded" && !!session,
    refetchInterval: (q) => (["ready", "published", "failed"].includes(q.state.data?.status ?? "") ? false : 3000), // polling fallback
  });
  // Realtime push (when a WebSocket URL is configured) refreshes the status immediately.
  useRealtime(client === "uploaded" && session ? `video:${session.videoId}` : null, () => void qc.invalidateQueries({ queryKey: statusKey }));

  const s = status.data?.status;
  const phase: UploadPhase =
    client === "idle" ? "idle" : client === "cancelled" ? "cancelled" : client === "failed" ? "failed"
      : client === "preparing" ? "preparing" : client === "uploading" ? "uploading"
        : s === "failed" ? "failed" : s === "encoding" ? "encoding" : s === "ready" ? "ready" : s === "published" ? "completed" : "processing";

  // Closing the tab or navigating away would lose an upload in progress (the file handle cannot be restored).
  useEffect(() => {
    if (phase !== "preparing" && phase !== "uploading") return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [phase]);

  return {
    phase, progress, fileInfo, session, serverStatus: status.data, offline, canRetry,
    processingFailed: s === "failed", error: s === "failed" ? "Processing failed. Please upload the file again." : error,
    start, cancel, retry: run, reset,
  };
}

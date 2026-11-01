"use client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { useRealtime } from "@/lib/realtime";
import { UploadError, uploadParts, type Progress } from "@/lib/uploader";
import { ApiError } from "@/services/api";
import { abortUpload, completeUpload, getUploadStatus, initUpload, putPart } from "@/services/upload";
import type { UploadInit, UploadPhase } from "@/types";

type ClientPhase = "idle" | "preparing" | "uploading" | "uploaded" | "failed" | "cancelled";
const EMPTY: Progress = { loaded: 0, total: 0, speed: 0, eta: Infinity };

function describe(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 429) return "Too many uploads right now. Please try again shortly.";
    if (e.status === 413 || e.code === "too_large") return "This file is too large.";
    if (e.status === 401 || e.status === 403) return "You’re not allowed to upload right now. Log in again and retry.";
  }
  if (e instanceof UploadError && e.code === "network") return "Connection lost. Retry to continue where you left off.";
  return "The upload failed. Retry to continue where you left off.";
}

/** Chunked, resumable upload + server-side processing status. */
export function useUpload() {
  const qc = useQueryClient();
  const [client, setClient] = useState<ClientPhase>("idle");
  const [progress, setProgress] = useState<Progress>(EMPTY);
  const [error, setError] = useState<string | null>(null);
  const [session, setSession] = useState<{ uploadId: string; videoId: string } | null>(null);
  const [fileInfo, setFileInfo] = useState<{ name: string; size: number } | null>(null);

  const file = useRef<File | null>(null);
  const abort = useRef<AbortController | null>(null);
  const init = useRef<UploadInit | null>(null);
  const done = useRef(new Map<number, string>()); // completed parts (kept for resume)

  const run = useCallback(async () => {
    const f = file.current;
    if (!f) return;
    const ac = new AbortController();
    abort.current = ac;
    setError(null);
    try {
      if (!init.current) {
        setClient("preparing");
        const res = await initUpload({ filename: f.name, size: f.size, contentType: f.type || "video/mp4" });
        init.current = res;
        setSession({ uploadId: res.uploadId, videoId: res.videoId });
      }
      const i = init.current;
      setClient("uploading");
      const parts = await uploadParts({ file: f, partSize: i.partSize, targets: i.parts, put: putPart, done: done.current, onProgress: setProgress, signal: ac.signal });
      await completeUpload(i.uploadId, parts);
      setClient("uploaded");
    } catch (e) {
      if (ac.signal.aborted) return;
      // Expired presigned URLs: start a fresh upload session on the next retry.
      if (e instanceof UploadError && e.status === 403) { init.current = null; done.current.clear(); }
      setClient("failed");
      setError(describe(e));
    }
  }, []);

  const start = useCallback((f: File) => {
    file.current = f;
    init.current = null;
    done.current = new Map();
    setFileInfo({ name: f.name, size: f.size });
    setProgress({ ...EMPTY, total: f.size });
    setSession(null);
    void run();
  }, [run]);

  const cancel = useCallback(() => {
    abort.current?.abort();
    if (init.current) void abortUpload(init.current.uploadId).catch(() => {});
    init.current = null;
    done.current = new Map();
    setClient("cancelled");
  }, []);

  const reset = useCallback(() => {
    abort.current?.abort();
    file.current = null;
    init.current = null;
    done.current = new Map();
    setFileInfo(null);
    setSession(null);
    setProgress(EMPTY);
    setError(null);
    setClient("idle");
  }, []);

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

  useEffect(() => {
    if (phase !== "preparing" && phase !== "uploading") return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [phase]);

  return {
    phase, progress, fileInfo, session, serverStatus: status.data,
    processingFailed: s === "failed", error: s === "failed" ? "Processing failed. Please upload the file again." : error,
    start, cancel, retry: run, reset,
  };
}

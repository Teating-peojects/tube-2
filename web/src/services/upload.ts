import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import { captionContentType } from "@/config/upload";
import { xhrPut, type PartDone, type PutPart } from "@/lib/uploader";
import type { ServerUploadStatus, UploadInit, UploadMeta } from "@/types";
import { ApiError, api } from "./api";
import { mockAsset, mockComplete, mockInit, mockPublish, mockPut, mockSaveDraft, mockStatus } from "./mock/studio";

const send = (method: string, body?: unknown): RequestInit => ({ method, body: body === undefined ? undefined : JSON.stringify(body) });

export const putPart: PutPart = env.useMock ? mockPut : xhrPut;

export const initUpload = (v: { filename: string; size: number; contentType: string }): Promise<UploadInit> =>
  env.useMock ? mockInit(v) : api(endpoints.upload.init, send("POST", v));
export const completeUpload = (id: string, parts: PartDone[]): Promise<void> =>
  env.useMock ? mockComplete(id) : api(endpoints.upload.complete(id), send("POST", { parts }));
export const abortUpload = (id: string): Promise<void> => (env.useMock ? Promise.resolve() : api(endpoints.upload.abort(id), send("DELETE")));
export const getUploadStatus = (id: string): Promise<ServerUploadStatus> => (env.useMock ? mockStatus(id) : api(endpoints.upload.status(id)));

/** Uploads a thumbnail or caption file through a presigned URL and returns its asset id. */
export async function uploadAsset(uploadId: string, kind: "thumbnail" | "caption", file: File, lang?: string): Promise<string> {
  if (env.useMock) return mockAsset();
  const contentType = kind === "caption" ? captionContentType(file) : file.type;
  const init = await api<{ assetId: string; url: string }>(endpoints.upload.assets(uploadId), send("POST", { kind, contentType, size: file.size, lang }));
  const res = await fetch(init.url, { method: "PUT", headers: { "Content-Type": contentType }, body: file });
  if (!res.ok) throw new ApiError(res.status, "Asset upload failed", "asset_failed");
  return init.assetId;
}

export const saveDraft = (videoId: string, meta: Partial<UploadMeta>): Promise<void> =>
  env.useMock ? mockSaveDraft() : api(endpoints.videoMetadata(videoId), send("PATCH", meta));
export const publishVideo = (videoId: string, meta: UploadMeta): Promise<void> =>
  env.useMock ? mockPublish(videoId, meta) : api(endpoints.videoPublish(videoId), send("POST", meta));

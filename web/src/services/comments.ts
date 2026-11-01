import { endpoints } from "@/config/endpoints";
import { env } from "@/config/env";
import type { Comment, CommentSort, Paginated } from "@/types";
import { api } from "./api";
import { mockCreateComment, mockGetComments, mockGetReplies } from "./mock/watch";

export async function getComments(videoId: string, p: { cursor?: string; sort: CommentSort }): Promise<Paginated<Comment>> {
  if (env.useMock) return mockGetComments(videoId, p);
  const q = new URLSearchParams({ sort: p.sort });
  if (p.cursor) q.set("cursor", p.cursor);
  return api(`${endpoints.comments(videoId)}?${q}`);
}

export async function getReplies(commentId: string): Promise<Paginated<Comment>> {
  if (env.useMock) return mockGetReplies(commentId);
  return api(endpoints.commentReplies(commentId));
}

export async function createComment(videoId: string, text: string, parentId?: string): Promise<Comment> {
  if (env.useMock) return mockCreateComment(videoId, text, parentId);
  return api(endpoints.comments(videoId), { method: "POST", body: JSON.stringify({ text, parentId }) });
}

export async function likeComment(commentId: string, liked: boolean): Promise<void> {
  if (env.useMock) return;
  await api(endpoints.commentLike(commentId), { method: "PUT", body: JSON.stringify({ liked }) });
}

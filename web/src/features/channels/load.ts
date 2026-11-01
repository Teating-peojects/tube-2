import { cache } from "react";
import { serverInit } from "@/lib/server-fetch";
import { ApiError } from "@/services/api";
import { getChannel } from "@/services/channels";

/** Server-side, de-duplicated per request (layout, metadata and pages share one fetch). */
export const loadChannel = cache(async (id: string) => {
  try {
    return await getChannel(id, await serverInit());
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
});

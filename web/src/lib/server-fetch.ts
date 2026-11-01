import { cookies } from "next/headers";

/**
 * For Server Components only: forwards the visitor's cookies to the API so user-specific data
 * (private playlists, reactions, subscription state) is correct during server rendering.
 */
export async function serverInit(): Promise<RequestInit> {
  const cookie = (await cookies()).toString();
  return cookie ? { headers: { Cookie: cookie }, cache: "no-store" } : { cache: "no-store" };
}

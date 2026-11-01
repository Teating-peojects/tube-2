import { env } from "@/config/env";

/** Error codes the UI understands. Anything else is shown as a generic error. */
export class ApiError extends Error {
  constructor(public status: number, message: string, public code?: string, public retryAfter?: number) {
    super(message);
    this.name = "ApiError";
  }
}

// Cookie-based session: credentials are sent, no tokens in localStorage.
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${env.apiUrl}${path}`, {
      credentials: "include",
      ...init,
      headers: { "Content-Type": "application/json", ...init.headers },
    });
  } catch {
    throw new ApiError(0, "Network error", "network");
  }

  if (!res.ok) {
    let code: string | undefined;
    try {
      const body = (await res.json()) as { code?: unknown };
      if (typeof body.code === "string") code = body.code.slice(0, 50);
    } catch { /* non-JSON error body */ }
    const ra = Number(res.headers.get("Retry-After"));

    // The signed age cookie is missing or expired: the server is the authority, so send the visitor back to the gate.
    if (res.status === 403 && code === "age_verification_required" && !path.startsWith("/api/v1/age/")) {
      if (typeof window !== "undefined") {
        if (!location.pathname.startsWith("/age-verification")) {
          location.assign(`/age-verification?next=${encodeURIComponent(location.pathname + location.search)}`);
        }
      } else {
        // Server Component render: redirect() throws a control-flow error that Next turns into a redirect.
        const { redirect } = await import("next/navigation");
        redirect("/age-verification");
      }
    }

    // A 401 outside /auth means an existing session expired (SessionWatcher decides what to do).
    if (res.status === 401 && typeof window !== "undefined" && !path.startsWith("/api/v1/auth/")) {
      window.dispatchEvent(new Event("xh:unauthorized"));
    }
    // Never surface backend messages/stack traces to the UI.
    throw new ApiError(res.status, "Request failed", code, Number.isFinite(ra) && ra > 0 ? ra : undefined);
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

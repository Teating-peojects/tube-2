import { NextResponse, type NextRequest } from "next/server";

// UI-level gates. The backend must still enforce age restrictions and authorization on every API call.
const PUBLIC = ["/age-verification", "/terms", "/privacy", "/community-guidelines", "/dmca"];
const PROTECTED = ["/upload", "/studio", "/history", "/watch-later", "/subscriptions", "/liked", "/notifications", "/profile", "/settings", "/messages"];
const PROTECTED_EXACT = ["/playlists"]; // /playlists/[id] stays public (backend hides private ones)
const SESSION_COOKIE = process.env.SESSION_COOKIE_NAME; // optional; empty disables the optimistic redirect

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  if (PUBLIC.some((p) => pathname.startsWith(p))) return NextResponse.next();

  // 1) Age gate
  if (req.cookies.get("xh_age_ok")?.value !== "1") {
    const url = req.nextUrl.clone();
    url.pathname = "/age-verification";
    url.search = "";
    return NextResponse.redirect(url);
  }

  // 2) Optimistic auth redirect (cookie presence only; AuthGuard + backend do the real check)
  if (SESSION_COOKIE && (PROTECTED_EXACT.includes(pathname) || PROTECTED.some((p) => pathname === p || pathname.startsWith(`${p}/`))) && !req.cookies.get(SESSION_COOKIE)) {
    const url = req.nextUrl.clone();
    url.pathname = "/login";
    url.search = `?next=${encodeURIComponent(pathname + req.nextUrl.search)}`;
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = { matcher: ["/((?!_next|api|favicon.ico|.*\\..*).*)"] };

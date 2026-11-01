import type { User } from "@/types";
import { ApiError } from "../api";

// MOCK ONLY. Demo account: demo@redxtube.test / demo / Demo-12345. Verification code: 123456.
// Real sessions are httpOnly cookies set by the backend; this mock fakes one with a plain cookie.
const COOKIE = "xh_session";
const wait = (ms = 450) => new Promise((r) => setTimeout(r, ms));

type Rec = { user: User; password: string };
const users = new Map<string, Rec>([
  ["u-demo", {
    password: "Demo-12345",
    user: { id: "u-demo", username: "demo", displayName: "Demo User", email: "demo@redxtube.test", avatarUrl: null, emailVerified: true, isCreator: false, joinedAt: "2026-01-15T00:00:00Z" },
  }],
]);

const setCookie = (id: string) => { document.cookie = `${COOKIE}=${id}; Path=/; Max-Age=2592000; SameSite=Lax`; };
const clearCookie = () => { document.cookie = `${COOKIE}=; Path=/; Max-Age=0; SameSite=Lax`; };
const currentId = () => document.cookie.match(new RegExp(`(?:^|; )${COOKIE}=([^;]*)`))?.[1];

let failures = 0;
let lockedUntil = 0;
let codeAttempts = 0;

export async function mockLogin(identifier: string, password: string): Promise<User> {
  await wait();
  if (Date.now() < lockedUntil) throw new ApiError(429, "", "rate_limited", Math.ceil((lockedUntil - Date.now()) / 1000));
  const key = identifier.trim().toLowerCase();
  const rec = [...users.values()].find((r) => r.user.email === key || r.user.username.toLowerCase() === key);
  if (!rec || rec.password !== password) {
    if (++failures >= 5) { lockedUntil = Date.now() + 30_000; failures = 0; }
    throw new ApiError(401, "", "invalid_credentials");
  }
  failures = 0;
  setCookie(rec.user.id);
  if (!rec.user.emailVerified) throw new ApiError(403, "", "verification_required");
  return rec.user;
}

export async function mockRegister(v: { email: string; username: string; password: string }): Promise<User> {
  await wait();
  const email = v.email.trim().toLowerCase();
  const taken = [...users.values()].some((r) => r.user.email === email || r.user.username.toLowerCase() === v.username.toLowerCase());
  if (taken) throw new ApiError(409, "", "conflict");
  const user: User = {
    id: `u-${Date.now()}`, username: v.username, displayName: v.username, email, avatarUrl: null,
    emailVerified: false, isCreator: false, joinedAt: new Date().toISOString(),
  };
  users.set(user.id, { user, password: v.password });
  setCookie(user.id);
  return user;
}

export async function mockLogout() { await wait(200); clearCookie(); }

export async function mockMe(): Promise<User> {
  await wait(150);
  const id = currentId();
  const rec = id ? users.get(id) : undefined;
  if (!rec) { clearCookie(); throw new ApiError(401, "", "unauthenticated"); }
  return rec.user;
}

export async function mockForgot(_email: string) { await wait(); }

export async function mockReset(token: string, _password: string) {
  await wait();
  if (token.length < 8) throw new ApiError(400, "", "invalid_token");
}

export async function mockVerify(code: string): Promise<User> {
  await wait();
  const id = currentId();
  const rec = id ? users.get(id) : undefined;
  if (!rec) throw new ApiError(401, "", "unauthenticated");
  if (codeAttempts >= 5) throw new ApiError(429, "", "rate_limited", 60);
  if (code !== "123456") { codeAttempts++; throw new ApiError(400, "", "invalid_code"); }
  codeAttempts = 0;
  rec.user = { ...rec.user, emailVerified: true };
  return rec.user;
}

export async function mockResend() { await wait(); }

const me = () => { const r = users.get(currentId() ?? ""); if (!r) throw new ApiError(401, "", "unauthenticated"); return r; };
export async function mockUpdateAccount(v: { displayName: string; bio: string }): Promise<User> {
  await wait(); const r = me(); r.user = { ...r.user, displayName: v.displayName, bio: v.bio }; return r.user;
}
export async function mockChangePassword(current: string, next: string) {
  await wait(); const r = me();
  if (r.password !== current) throw new ApiError(400, "", "invalid_credentials");
  r.password = next;
}
export async function mockLogoutAll() { await wait(200); clearCookie(); }
export async function mockDeleteAccount(password: string) {
  await wait(); const r = me();
  if (r.password !== password) throw new ApiError(400, "", "invalid_credentials");
  users.delete(r.user.id); clearCookie();
}

/** Only allow same-site relative redirects (prevents open-redirect attacks via ?next=). */
export function safeNext(next: string | null | undefined, fallback = "/"): string {
  if (!next) return fallback;
  if (!next.startsWith("/") || next.startsWith("//") || next.includes("\\") || /[\r\n]/.test(next)) return fallback;
  if (/^\/(login|register|verify|forgot-password|reset-password)(\/|\?|$)/.test(next)) return fallback;
  return next;
}

export function maskEmail(email: string): string {
  const [name = "", domain = ""] = email.split("@");
  return `${name.slice(0, 1)}***@${domain}`;
}

/** Allow only http(s) URLs for user-provided external links (blocks javascript:, data:, etc.). */
export function safeExternalUrl(raw: string): string | null {
  try {
    const u = new URL(raw);
    return u.protocol === "https:" || u.protocol === "http:" ? u.toString() : null;
  } catch {
    return null;
  }
}

import { ApiError } from "@/services/api";

function wait(sec: number) {
  return sec >= 60 ? `${Math.ceil(sec / 60)} min` : `${sec} sec`;
}

/** Maps API errors to safe, user-friendly messages (no backend details are shown). */
export function authErrorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return "Can’t reach REDXTUBE. Check your connection and try again.";
    if (e.status === 429) return e.retryAfter ? `Too many attempts. Try again in ${wait(e.retryAfter)}.` : "Too many attempts. Please try again later.";
    switch (e.code) {
      case "invalid_credentials": return "Incorrect email/username or password.";
      case "invalid_code": return "That code is incorrect or has expired.";
      case "invalid_token": return "This link is invalid or has expired.";
      case "conflict": case "email_taken": case "username_taken": return "That email or username is already in use.";
    }
    if (e.status === 401) return "Incorrect email/username or password.";
    if (e.status === 409) return "That email or username is already in use.";
  }
  return "Something went wrong. Please try again.";
}

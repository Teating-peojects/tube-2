import { describe, expect, it } from "vitest";
import { ApiError } from "@/services/api";
import { authErrorMessage } from "./errors";

describe("authErrorMessage", () => {
  it("maps known codes without leaking backend details", () => {
    expect(authErrorMessage(new ApiError(401, "SQL error at line 3", "invalid_credentials"))).toBe("Incorrect email/username or password.");
    expect(authErrorMessage(new ApiError(400, "", "invalid_code"))).toMatch(/incorrect or has expired/);
  });
  it("explains rate limits", () => {
    expect(authErrorMessage(new ApiError(429, "", "rate_limited", 30))).toBe("Too many attempts. Try again in 30 sec.");
    expect(authErrorMessage(new ApiError(429, "", "rate_limited", 120))).toBe("Too many attempts. Try again in 2 min.");
  });
  it("handles offline and unknown errors", () => {
    expect(authErrorMessage(new ApiError(0, "", "network"))).toMatch(/Can’t reach/);
    expect(authErrorMessage(new Error("boom"))).toBe("Something went wrong. Please try again.");
  });
});

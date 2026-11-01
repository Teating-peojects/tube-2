import { describe, expect, it } from "vitest";
import { maskEmail, safeExternalUrl, safeNext } from "./auth";

describe("safeNext (open-redirect protection)", () => {
  it("allows same-site paths", () => {
    expect(safeNext("/watch/v1")).toBe("/watch/v1");
    expect(safeNext("/search?q=a")).toBe("/search?q=a");
  });
  it.each(["//evil.com", "https://evil.com", "/\\evil.com", "evil", "/a\nb", "/login", "/register?next=/"])("rejects %j", (v) => {
    expect(safeNext(v)).toBe("/");
  });
  it("uses the fallback for empty input", () => {
    expect(safeNext(null)).toBe("/");
    expect(safeNext(undefined, "/home")).toBe("/home");
  });
});

describe("helpers", () => {
  it("masks emails", () => expect(maskEmail("jane@example.com")).toBe("j***@example.com"));
  it("only allows http(s) external links", () => {
    expect(safeExternalUrl("javascript:alert(1)")).toBeNull();
    expect(safeExternalUrl("data:text/html,hi")).toBeNull();
    expect(safeExternalUrl("not a url")).toBeNull();
    expect(safeExternalUrl("https://example.com")).toBe("https://example.com/");
  });
});

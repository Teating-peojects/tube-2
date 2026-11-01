import { describe, expect, it } from "vitest";
import { formatBytes, formatDuration, formatEta, formatViews, timeAgo } from "./format";

describe("format", () => {
  it("formats durations", () => {
    expect(formatDuration(65)).toBe("01:05");
    expect(formatDuration(3661)).toBe("1:01:01");
  });
  it("formats views compactly", () => expect(formatViews(1200)).toBe("1.2K"));
  it("formats bytes", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(5 * 1024 ** 3)).toBe("5.0 GB");
  });
  it("formats ETA", () => {
    expect(formatEta(Infinity)).toBe("calculating…");
    expect(formatEta(45)).toBe("45s left");
    expect(formatEta(125)).toBe("2m 5s left");
  });
  it("formats relative time", () => {
    const now = Date.parse("2026-01-10T00:00:00Z");
    expect(timeAgo("2026-01-08T00:00:00Z", now)).toBe("2 days ago");
  });
});

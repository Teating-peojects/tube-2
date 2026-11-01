import { describe, expect, it } from "vitest";
import { uploadLimits, validateCaption, validateImage, validateVideoFile } from "./upload";

const file = (name: string, type: string, size: number) => {
  const f = new File([new Uint8Array(1)], name, { type });
  Object.defineProperty(f, "size", { value: size }); // avoid allocating gigabytes in a test
  return f;
};

describe("upload validation", () => {
  it("matches the backend limits (5 GiB long, 200 MiB short)", () => {
    expect(uploadLimits.maxBytes).toBe(5 * 1024 ** 3);
    expect(uploadLimits.maxShortBytes).toBe(200 * 1024 ** 2);
  });
  it("accepts a normal video and rejects oversize / wrong type / empty files", () => {
    expect(validateVideoFile(file("a.mp4", "video/mp4", 1_000_000))).toBeNull();
    expect(validateVideoFile(file("a.mp4", "video/mp4", uploadLimits.maxBytes))).toBeNull();
    expect(validateVideoFile(file("a.mp4", "video/mp4", uploadLimits.maxBytes + 1))).toMatch(/too large/);
    expect(validateVideoFile(file("a.txt", "text/plain", 10))).toMatch(/Unsupported format/);
    expect(validateVideoFile(file("a.mp4", "video/mp4", 0))).toMatch(/empty/);
  });
  it("validates thumbnails and captions", () => {
    expect(validateImage(file("t.png", "image/png", 1000))).toBeNull();
    expect(validateImage(file("t.gif", "image/gif", 1000))).toMatch(/JPG, PNG or WebP/);
    expect(validateCaption(file("c.vtt", "text/vtt", 1000))).toBeNull();
    expect(validateCaption(file("c.doc", "", 1000))).toMatch(/\.vtt or \.srt/);
  });
});

import { formatBytes } from "@/utils/format";

export const uploadLimits = {
  // Must match the database/API limits: LONG videos <= 5 GiB, SHORT videos (<= 60 s) <= 200 MiB.
  // The duration is unknown before upload, so the client enforces the larger limit and the API rejects an oversized SHORT.
  maxBytes: 5 * 1024 ** 3,
  maxShortBytes: 200 * 1024 ** 2,
  extensions: [".mp4", ".mov", ".mkv", ".webm", ".avi"],
  maxThumbBytes: 2 * 1024 ** 2,
  maxCaptionBytes: 1024 ** 2,
} as const;

export const uploadCategories = ["Entertainment", "Music", "Gaming", "Shorts", "Documentary", "Comedy", "Education", "Other"] as const;

export const languages = [
  { code: "en", label: "English" }, { code: "bn", label: "Bengali" }, { code: "hi", label: "Hindi" }, { code: "es", label: "Spanish" },
  { code: "fr", label: "French" }, { code: "de", label: "German" }, { code: "pt", label: "Portuguese" }, { code: "ru", label: "Russian" },
  { code: "ar", label: "Arabic" }, { code: "ja", label: "Japanese" }, { code: "ko", label: "Korean" }, { code: "zh", label: "Chinese" },
] as const;

const ext = (name: string) => name.slice(name.lastIndexOf(".")).toLowerCase();

// Client checks are for fast feedback only; the backend must validate type, size and content again.
export function validateVideoFile(f: File): string | null {
  if (!uploadLimits.extensions.includes(ext(f.name) as never)) return `Unsupported format. Use ${uploadLimits.extensions.join(", ")}.`;
  if (f.type && !f.type.startsWith("video/")) return "That file doesn’t look like a video.";
  if (f.size === 0) return "That file is empty.";
  if (f.size > uploadLimits.maxBytes) return `File is too large. Maximum size is ${formatBytes(uploadLimits.maxBytes)}.`;
  return null;
}
export function validateImage(f: File): string | null {
  if (!["image/jpeg", "image/png", "image/webp"].includes(f.type)) return "Thumbnail must be a JPG, PNG or WebP image.";
  if (f.size > uploadLimits.maxThumbBytes) return `Thumbnail must be under ${formatBytes(uploadLimits.maxThumbBytes)}.`;
  return null;
}
export function validateCaption(f: File): string | null {
  if (![".vtt", ".srt"].includes(ext(f.name))) return "Captions must be a .vtt or .srt file.";
  if (f.size > uploadLimits.maxCaptionBytes) return `Caption file must be under ${formatBytes(uploadLimits.maxCaptionBytes)}.`;
  return null;
}
export const captionContentType = (f: File) => (ext(f.name) === ".vtt" ? "text/vtt" : "application/x-subrip");

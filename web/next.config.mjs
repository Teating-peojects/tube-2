const cdn = process.env.NEXT_PUBLIC_CDN_URL;
const remotePatterns = cdn
  ? [{ protocol: new URL(cdn).protocol.replace(":", ""), hostname: new URL(cdn).hostname }]
  : [];

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  images: { remotePatterns, formats: ["image/avif", "image/webp"] },
  async headers() {
    return [{
      source: "/(.*)",
      headers: [
        { key: "X-Content-Type-Options", value: "nosniff" },
        { key: "X-Frame-Options", value: "DENY" },
        { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
        { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=(), usb=()" },
        { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
      ],
    }];
  },
};
export default nextConfig;

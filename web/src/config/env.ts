export const env = {
  apiUrl: process.env.NEXT_PUBLIC_API_URL ?? "",
  cdnUrl: process.env.NEXT_PUBLIC_CDN_URL ?? "",
  wsUrl: process.env.NEXT_PUBLIC_WS_URL ?? "",
  useMock: process.env.NEXT_PUBLIC_USE_MOCK === "true",
} as const;

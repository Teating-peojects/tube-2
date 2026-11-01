// Replace placeholders (or set the NEXT_PUBLIC_* env vars) before launch.
export const site = {
  name: "REDXTUBE",
  url: "https://redxtube.com",
  legalName: process.env.NEXT_PUBLIC_LEGAL_NAME ?? "[legal entity name]",
  supportEmail: process.env.NEXT_PUBLIC_SUPPORT_EMAIL ?? "[support email]",
  dmcaEmail: process.env.NEXT_PUBLIC_DMCA_EMAIL ?? "[DMCA agent email]",
  updated: process.env.NEXT_PUBLIC_LEGAL_UPDATED ?? "[date]",
} as const;

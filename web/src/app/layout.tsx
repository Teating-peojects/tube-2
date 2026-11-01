import type { Metadata, Viewport } from "next";
import { Manrope } from "next/font/google";
import type { ReactNode } from "react";
import { ToastViewport } from "@/components/ui/ToastViewport";
import { ModalHost } from "@/features/safety/ModalHost";
import { QueryProvider } from "@/lib/query-provider";
import "./globals.css";

const manrope = Manrope({ subsets: ["latin"], variable: "--font-manrope", display: "swap" });

export const metadata: Metadata = {
  metadataBase: new URL("https://redxtube.com"),
  title: { default: "REDXTUBE", template: "%s · REDXTUBE" },
  description: "REDXTUBE — a premium, age-restricted (18+) video platform.",
  robots: { index: true, follow: true },
  // Voluntary adult-content label recognised by parental-control filters.
  other: { rating: "RTA-5042-1996-1400-1577-RTA" },
};
export const viewport: Viewport = { themeColor: "#0D0D0E", viewportFit: "cover" };

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={manrope.variable}>
      <body>
        <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:rounded-md focus:bg-accent focus:px-3 focus:py-2">Skip to content</a>
        <QueryProvider>
          {children}
          <ToastViewport />
          <ModalHost />
        </QueryProvider>
      </body>
    </html>
  );
}

import type { Metadata, Viewport } from "next";
import "./globals.css";
import { SessionProvider } from "@naditos/web-common/session";

export const metadata: Metadata = {
  title: "NADITOS · Police",
  description: "Officer enforcement app",
  manifest: "/manifest.webmanifest",
};
export const viewport: Viewport = {
  width: "device-width", initialScale: 1, themeColor: "#0f172a",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body><SessionProvider>{children}</SessionProvider></body>
    </html>
  );
}

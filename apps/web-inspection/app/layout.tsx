import type { Metadata, Viewport } from "next";
import "./globals.css";
import { SessionProvider } from "@naditos/web-common/session";

export const metadata: Metadata = {
  title: "NADITOS · Inspection Authority",
  description: "National transport intelligence platform — vehicle inspection authority console",
};
export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: "#060a18",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="nadit-dark">
      <body>
        <SessionProvider>{children}</SessionProvider>
      </body>
    </html>
  );
}

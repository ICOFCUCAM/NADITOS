import type { Metadata } from "next";
import "./globals.css";
import { SessionProvider } from "@naditos/web-common/session";

export const metadata: Metadata = {
  title: "NADITOS · Ministry",
  description: "National Digital Transport Operating System — admin",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <SessionProvider>{children}</SessionProvider>
      </body>
    </html>
  );
}

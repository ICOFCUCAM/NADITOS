import type { Metadata } from "next";
import "./globals.css";
import { SessionProvider } from "@naditos/web-common/session";

export const metadata: Metadata = {
  title: "NADITOS · My Vehicles",
  description: "Citizen portal — vehicles, fines, payments",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="nadit-light">
      <body><SessionProvider>{children}</SessionProvider></body>
    </html>
  );
}

import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Telemetry Platform",
  description: "Unified product analytics and observability",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}

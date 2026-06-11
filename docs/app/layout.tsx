import type { Metadata, Viewport } from "next";
import "nextra-theme-docs/style.css";
import "../globals.css";
import "../versioning.css";
import "../shell.css";

export const metadata: Metadata = {
  title: {
    default: "Gestalt",
    template: "%s – Gestalt",
  },
  icons: {
    icon: "/favicon.svg",
  },
};

export const viewport: Viewport = {
  themeColor: "#ffffff",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" dir="ltr" suppressHydrationWarning>
      <body>{children}</body>
    </html>
  );
}

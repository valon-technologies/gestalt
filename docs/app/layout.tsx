import type { Metadata, Viewport } from "next";
import "nextra-theme-docs/style.css";
import "../globals.css";

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
  themeColor: "#FDFCF9",
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

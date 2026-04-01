import type { Metadata } from "next";
import { Bitter, IBM_Plex_Sans, IBM_Plex_Mono } from "next/font/google";
import "./globals.css";

const bitter = Bitter({
  subsets: ["latin"],
  weight: ["400", "600", "700"],
  variable: "--font-heading",
});

const ibmPlexSans = IBM_Plex_Sans({
  subsets: ["latin"],
  weight: ["400", "500", "600"],
  variable: "--font-body",
});

const ibmPlexMono = IBM_Plex_Mono({
  subsets: ["latin"],
  weight: ["400"],
  variable: "--font-mono",
});

export const metadata: Metadata = {
  title: "Gestalt",
  description: "Integration management for Gestalt",
};

const themeScript = `
  (function() {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const getTheme = function() {
      const stored = localStorage.getItem('theme');
      return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system';
    };
    const applyTheme = function(theme) {
      const useDark = theme === 'dark' || (theme === 'system' && media.matches);
      document.documentElement.classList.toggle('dark', useDark);
    };

    applyTheme(getTheme());
    media.addEventListener('change', function() {
      if (getTheme() === 'system') {
        applyTheme('system');
      }
    });
  })();
`;

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body className={`${bitter.variable} ${ibmPlexSans.variable} ${ibmPlexMono.variable} font-sans antialiased`}>
        {children}
      </body>
    </html>
  );
}

import type { Metadata } from "next";
import localFont from "next/font/local";
import "./globals.css";

const seasonSerif = localFont({
  src: [
    { path: "../../public/fonts/SeasonSerif_Regular.woff", weight: "400", style: "normal" },
    { path: "../../public/fonts/SeasonSerif_RegularItalic.woff", weight: "400", style: "italic" },
  ],
  variable: "--font-display",
});

const melangeGrotesk = localFont({
  src: [
    { path: "../../public/fonts/KMRMelangeGrotesk_Regular.woff", weight: "400", style: "normal" },
    { path: "../../public/fonts/KMRMelangeGrotesk_Bold.woff", weight: "700", style: "normal" },
    { path: "../../public/fonts/KMRMelangeGrotesk_Italic.woff", weight: "400", style: "italic" },
    { path: "../../public/fonts/KMRMelangeGrotesk_BoldItalic.woff", weight: "700", style: "italic" },
  ],
  variable: "--font-body",
  adjustFontFallback: false,
});

const geistMono = localFont({
  src: [
    { path: "../../public/fonts/GeistMono_Regular.woff2", weight: "400", style: "normal" },
  ],
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
      <body className={`${seasonSerif.variable} ${melangeGrotesk.variable} ${geistMono.variable} font-sans antialiased gradient-warm`}>
        {children}
      </body>
    </html>
  );
}

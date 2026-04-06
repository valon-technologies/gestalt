import type { Metadata, Viewport } from "next";
import { Footer, Layout, Navbar } from "nextra-theme-docs";
import { Head } from "nextra/components";
import { getPageMap } from "nextra/page-map";
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

const navbar = (
  <Navbar
    logo={
      <span
        style={{
          fontFamily: "'Season Serif', Georgia, serif",
          fontSize: "1.7rem",
          letterSpacing: "-0.03em",
        }}
      >
        Gestalt
      </span>
    }
  />
);

const footer = (
  <Footer>
    <span>
      Gestalt, a self-hosted integration platform by{" "}
      <a
        href="https://valon.ai"
        target="_blank"
        rel="noopener noreferrer"
        style={{ textDecoration: "underline", textUnderlineOffset: "0.18em" }}
      >
        Valon Technologies
      </a>
    </span>
  </Footer>
);

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" dir="ltr" suppressHydrationWarning>
      <Head>
        <meta name="theme-color" content="#FDFCF9" />
      </Head>
      <body>
        <Layout
          navbar={navbar}
          footer={footer}
          pageMap={await getPageMap()}
          docsRepositoryBase="https://github.com/valon-technologies/gestalt/tree/main/docs"
          darkMode={false}
          nextThemes={{
            defaultTheme: "light",
            forcedTheme: "light",
            storageKey: "gestalt-docs-theme",
          }}
          sidebar={{
            defaultMenuCollapseLevel: 1,
          }}
          toc={{
            float: true,
          }}
          navigation={{
            prev: true,
            next: true,
          }}
        >
          {children}
        </Layout>
      </body>
    </html>
  );
}

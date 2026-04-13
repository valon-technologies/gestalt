import type { Metadata, Viewport } from "next";
import { Footer, Layout, Navbar } from "nextra-theme-docs";
import { Head, Search } from "nextra/components";
import { GitHubIcon } from "nextra/icons";
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

const repositoryUrl = "https://github.com/valon-technologies/gestalt";

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

const search = (
  <div className="docs-header-search">
    <a
      href={repositoryUrl}
      target="_blank"
      rel="noopener noreferrer"
      className="docs-header-repo-link"
      aria-label="View the Gestalt GitHub repository"
    >
      <GitHubIcon height="20" aria-hidden="true" />
      <span>GitHub</span>
    </a>
    <Search />
  </div>
);

const footer = <Footer />;

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
          search={search}
          pageMap={await getPageMap()}
          docsRepositoryBase="https://github.com/valon-technologies/gestalt/tree/main/docs"
          nextThemes={{
            defaultTheme: "system",
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

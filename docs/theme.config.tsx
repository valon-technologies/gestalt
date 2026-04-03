import { useConfig } from "nextra-theme-docs";
import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: <strong style={{ fontFamily: "'Bitter', Georgia, serif" }}>Gestalt</strong>,
  project: {
    link: "https://github.com/valon-technologies/gestalt",
  },
  docsRepositoryBase:
    "https://github.com/valon-technologies/gestalt/tree/main/docs",
  head: function Head() {
    const { title } = useConfig();
    const fullTitle = title ? `${title} – Gestalt` : "Gestalt";
    return (
      <>
        <title>{fullTitle}</title>
        <meta property="og:title" content={fullTitle} />
        <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
      </>
    );
  },
  footer: {
    content: <span>Gestalt, a self-hosted integration platform</span>,
  },
  sidebar: {
    defaultMenuCollapseLevel: 1,
  },
  toc: {
    float: true,
  },
  navigation: {
    prev: true,
    next: true,
  },
};

export default config;

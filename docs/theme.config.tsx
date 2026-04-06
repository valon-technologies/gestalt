import { useConfig } from "nextra-theme-docs";
import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: (
    <span style={{ fontFamily: "'Season Serif', Georgia, serif", fontSize: "1.7rem", letterSpacing: "-0.03em" }}>
      Gestalt
    </span>
  ),
  project: {
    link: "https://github.com/valon-technologies/gestalt",
  },
  docsRepositoryBase:
    "https://github.com/valon-technologies/gestalt/tree/main/docs",
  darkMode: false,
  nextThemes: {
    defaultTheme: "light",
    forcedTheme: "light",
    storageKey: "gestalt-docs-theme",
  },
  backgroundColor: {
    light: "#FFFFFF",
    dark: "#FFFFFF",
  },
  color: {
    hue: 37,
    saturation: 84,
    lightness: 48,
  },
  head: function Head() {
    const { title } = useConfig();
    const fullTitle = title ? `${title} – Gestalt` : "Gestalt";
    return (
      <>
        <title>{fullTitle}</title>
        <meta property="og:title" content={fullTitle} />
        <meta name="theme-color" content="#FDFCF9" />
        <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
      </>
    );
  },
  footer: {
    content: (
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
    ),
  },
  search: {
    placeholder: "Search documentation",
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

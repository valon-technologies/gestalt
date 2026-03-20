import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: <strong>Gestalt</strong>,
  project: {
    link: "https://github.com/valon-technologies/gestalt",
  },
  docsRepositoryBase:
    "https://github.com/valon-technologies/gestalt/tree/main/gestalt/docs",
  footer: {
    content: <span>Gestalt — self-serve integration platform</span>,
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

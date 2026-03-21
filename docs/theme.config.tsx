import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: <strong>Gestalt</strong>,
  project: {
    link: "https://github.com/valon-technologies/toolshed",
  },
  docsRepositoryBase:
    "https://github.com/valon-technologies/toolshed/tree/main/toolshed/docs",
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

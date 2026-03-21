import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: <strong>Toolshed</strong>,
  project: {
    link: "https://github.com/valon-technologies/toolshed",
  },
  docsRepositoryBase:
    "https://github.com/valon-technologies/toolshed/tree/main/toolshed/docs",
  footer: {
    content: <span>Toolshed — self-serve integration platform</span>,
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

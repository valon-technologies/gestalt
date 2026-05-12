import { Footer, Layout, Navbar } from "nextra-theme-docs";
import { Search } from "nextra/components";
import { GitHubIcon } from "nextra/icons";
import { getPageMap } from "nextra/page-map";
import VersionPicker from "../../components/VersionPicker";

const repositoryUrl = "https://github.com/valon-technologies/gestalt";
const repositoryRef = process.env.GESTALT_DOCS_REPOSITORY_REF || "main";

const navbar = (
  <Navbar
    logo={
      <span
        style={{
          fontFamily: "'Season Serif', Georgia, serif",
          fontSize: "1.7rem",
        }}
      >
        Gestalt
      </span>
    }
  />
);

const search = (
  <div className="docs-header-search">
    <VersionPicker />
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

export default async function DocsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <Layout
      navbar={navbar}
      footer={footer}
      search={search}
      pageMap={await getPageMap()}
      docsRepositoryBase={`https://github.com/valon-technologies/gestalt/tree/${repositoryRef}/docs`}
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
  );
}

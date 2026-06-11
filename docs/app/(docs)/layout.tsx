import { Layout, Navbar } from "nextra-theme-docs";
import { getPageMap } from "nextra/page-map";
import ShellFooter from "../../components/shell/footer";
import ShellHeaderActions from "../../components/shell/header-actions";
import ShellLogo from "../../components/shell/logo";
import SidebarVersionPicker from "../../components/shell/sidebar-version-picker";

const repositoryRef = process.env.GESTALT_DOCS_REPOSITORY_REF || "main";

export default async function DocsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <Layout
      navbar={<Navbar logo={<ShellLogo />} />}
      footer={<ShellFooter />}
      search={<ShellHeaderActions />}
      darkMode={false}
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
      <SidebarVersionPicker />
      {children}
    </Layout>
  );
}

import { Layout, Navbar } from "nextra-theme-docs";
import ShellFooter from "../../components/shell/footer";
import ShellHeaderActions from "../../components/shell/header-actions";
import ShellLogo from "../../components/shell/logo";

// The registry is unversioned and has no page map: it gets the shared shell
// (navbar, search, theme, footer) but no docs sidebar, TOC, or version picker.
export default function RegistryLayout({
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
      // One empty meta entry: nextra's normalize-pages crashes on a truly
      // empty page map, and the registry must not list docs routes (their
      // hrefs would bypass the version base path).
      pageMap={[{ data: {} }]}
      nextThemes={{
        defaultTheme: "system",
        storageKey: "gestalt-docs-theme",
      }}
    >
      {children}
    </Layout>
  );
}

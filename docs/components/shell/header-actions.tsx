"use client";

import { usePathname } from "next/navigation";
import { Search } from "nextra/components";
import { GitHubIcon } from "nextra/icons";
import VersionPicker from "../VersionPicker";

const repositoryUrl = "https://github.com/valon-technologies/gestalt";

// Plain anchors that bypass the version base path: docs links always target
// the /latest/ rolling alias (production trees live under /latest, /dev,
// /versions/*), and the registry lives outside every version tree. Dev
// serves the docs unversioned at the root.
const docsHref = process.env.NODE_ENV === "development" ? "/" : "/latest/";

export default function ShellHeaderActions() {
  const pathname = usePathname();
  const onRegistry =
    pathname === "/registry" || pathname.startsWith("/registry/");
  return (
    <div className="docs-header-search">
      <a
        className="shell-link shell-nav-link"
        href={docsHref}
        aria-current={onRegistry ? undefined : "true"}
      >
        Docs
      </a>
      <a
        className="shell-link shell-nav-link"
        href="/registry"
        aria-current={onRegistry ? "true" : undefined}
      >
        Registry
      </a>
      {/* Desktop docs pages get the picker at the sidebar top; the mobile
          drawer has no sidebar, so it keeps a picker here. Never on the
          registry, which is unversioned. */}
      {!onRegistry && (
        <div className="shell-mobile-version">
          <VersionPicker />
        </div>
      )}
      <a
        href={repositoryUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="shell-button docs-header-repo-link"
        aria-label="View the Gestalt GitHub repository"
      >
        <GitHubIcon height="20" aria-hidden="true" />
        <span>GitHub</span>
      </a>
      <Search />
    </div>
  );
}

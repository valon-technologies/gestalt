"use client";

import { Search } from "nextra/components";
import { GitHubIcon } from "nextra/icons";
import VersionPicker from "../VersionPicker";
import { docsBaseUrl, registryBaseUrl } from "./site-bases";

const repositoryUrl = "https://github.com/valon-technologies/gestalt";

// Plain anchors that bypass the version base path: docs links always target
// the /latest/ rolling alias (production trees live under /latest, /dev,
// /versions/*), and the registry lives outside every version tree. Dev
// serves the docs unversioned at the root.
const docsHref =
  process.env.NODE_ENV === "development" ? "/" : `${docsBaseUrl}/latest/`;

// Which half of the site the navbar belongs to is layout knowledge — the
// pathname can't answer it once the registry serves from its own origin —
// so the layouts declare it.
export default function ShellHeaderActions({
  side = "docs",
}: {
  side?: "docs" | "registry";
}) {
  const onRegistry = side === "registry";
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
        href={registryBaseUrl}
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

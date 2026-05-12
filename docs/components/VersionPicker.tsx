"use client";

import { useEffect, useMemo, useState } from "react";

type DocsVersion = {
  label: string;
  pathPrefix: string;
  stable?: boolean;
  tag: string;
};

type VersionsManifest = {
  latest: DocsVersion & { version: string };
  versions: DocsVersion[];
};

const currentPrefix = process.env.NEXT_PUBLIC_GESTALT_DOCS_BASE_PATH || "";
const currentLabel =
  process.env.NEXT_PUBLIC_GESTALT_DOCS_CURRENT_LABEL || "Unversioned";

function optionValue(version: Pick<DocsVersion, "pathPrefix">) {
  return version.pathPrefix.replace(/\/$/, "");
}

function suffixForPath(pathname: string, prefixes: string[]) {
  const matched = prefixes
    .filter((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`))
    .sort((left, right) => right.length - left.length)[0];
  if (!matched) {
    return pathname === "/" ? "/" : pathname;
  }
  const suffix = pathname.slice(matched.length);
  return suffix || "/";
}

export default function VersionPicker() {
  const [manifest, setManifest] = useState<VersionsManifest | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch("/versions.json")
      .then((response) => (response.ok ? response.json() : null))
      .then((data) => {
        if (!cancelled && data) {
          setManifest(data);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setManifest(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const versions = useMemo(() => {
    if (!manifest) {
      return [{ label: currentLabel, pathPrefix: currentPrefix || "/" }];
    }
    return [
      {
        ...manifest.latest,
        label: `${manifest.latest.label} (${manifest.latest.version})`,
      },
      ...manifest.versions,
    ];
  }, [manifest]);

  const selected = currentPrefix || optionValue(versions[0]);

  return (
    <label className="docs-version-picker">
      <span className="docs-version-picker-label">Docs version</span>
      <select
        aria-label="Docs version"
        value={selected}
        onChange={(event) => {
          const nextPrefix = event.target.value;
          const prefixes = versions
            .map((version) => optionValue(version))
            .filter((prefix) => prefix !== "/");
          const suffix = suffixForPath(window.location.pathname, prefixes);
          const targetPath = `${nextPrefix}${suffix === "/" ? "/" : suffix}`;
          window.location.assign(
            `${targetPath}${window.location.search}${window.location.hash}`,
          );
        }}
      >
        {versions.map((version) => (
          <option key={version.pathPrefix} value={optionValue(version)}>
            {version.label}
          </option>
        ))}
      </select>
    </label>
  );
}

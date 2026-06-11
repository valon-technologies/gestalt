"use client";

import { useEffect, useMemo, useState } from "react";

type DocsVersion = {
  development?: boolean;
  label: string;
  pathPrefix: string;
  stable?: boolean;
  tag: string;
  version?: string;
};

type VersionsManifest = {
  development?: DocsVersion & { development: true; version: string };
  latest: DocsVersion & { version: string };
  versions: Array<DocsVersion & { version: string }>;
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

function selectedValue(
  prefix: string,
  visibleVersions: Array<Pick<DocsVersion, "pathPrefix">>,
  routeVersions: Array<Pick<DocsVersion, "pathPrefix" | "tag">>,
  latest: Pick<DocsVersion, "pathPrefix" | "tag"> | null,
) {
  if (!prefix) {
    return optionValue(visibleVersions[0]);
  }

  const currentRoute = routeVersions.find(
    (version) => optionValue(version) === prefix,
  );
  if (currentRoute?.tag && currentRoute.tag === latest?.tag) {
    return optionValue(latest);
  }

  return prefix;
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

  const { routeVersions, visibleVersions } = useMemo(() => {
    if (!manifest) {
      const fallback = [
        { label: currentLabel, pathPrefix: currentPrefix || "/", tag: "" },
      ];
      return { routeVersions: fallback, visibleVersions: fallback };
    }

    const development = manifest.development
      ? [
          {
            ...manifest.development,
            label: `${manifest.development.label} (${manifest.development.version})`,
          },
        ]
      : [];
    const latest = {
      ...manifest.latest,
      label: manifest.latest.version,
    };

    return {
      routeVersions: [
        manifest.latest,
        ...(manifest.development ? [manifest.development] : []),
        ...manifest.versions,
      ],
      visibleVersions: [
        latest,
        ...development,
        ...manifest.versions.filter(
          (version) => version.tag !== manifest.latest.tag,
        ),
      ],
    };
  }, [manifest]);

  const selected = selectedValue(
    currentPrefix,
    visibleVersions,
    routeVersions,
    manifest?.latest ?? null,
  );

  return (
    <label className="docs-version-picker">
      <select
        className="shell-select"
        aria-label="Docs version"
        value={selected}
        onChange={(event) => {
          const nextPrefix = event.target.value;
          const prefixes = routeVersions
            .map((version) => optionValue(version))
            .filter((prefix) => prefix !== "/");
          const suffix = suffixForPath(window.location.pathname, prefixes);
          const targetPath = `${nextPrefix}${suffix === "/" ? "/" : suffix}`;
          window.location.assign(
            `${targetPath}${window.location.search}${window.location.hash}`,
          );
        }}
      >
        {visibleVersions.map((version) => (
          <option key={version.pathPrefix} value={optionValue(version)}>
            {version.label}
          </option>
        ))}
      </select>
    </label>
  );
}

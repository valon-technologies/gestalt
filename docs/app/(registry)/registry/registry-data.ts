import type { MouseEvent } from "react";

export type ConfigTarget = {
  section: string;
  entryKind: string;
  requiredSet?: Record<string, string>;
};

export type ProviderVersion = {
  version: string;
  metadata: string;
  kind: string;
  runtime: string;
  platforms: string[];
  yanked?: boolean;
};

export type ProviderDoc = {
  path: string;
  title: string;
  sourcePath: string;
  rawUrl: string;
  editUrl: string;
};

export type Provider = {
  package: string;
  packagePath: string;
  name: string;
  kind: string;
  configTarget: ConfigTarget;
  displayName: string;
  description: string;
  manifestVersion: string | null;
  latestInstallableVersion: string | null;
  versions: ProviderVersion[];
  registryPath: string;
  sourceUrl: string | null;
  readmeUrl: string | null;
  manifestUrl: string | null;
  iconUrl: string | null;
  docs?: ProviderDoc[];
};

export type Catalog = {
  schema: string;
  schemaVersion: number;
  repository: string;
  indexUrl: string;
  providers: Provider[];
};

export type RouteSelection = {
  kind: string;
  name: string;
  docPath: string;
};

export const allKinds = "all";
export const catalogSchemaVersion = 1;
// Route prefix comes from the shared site-bases contract: none when the
// registry serves from its own origin (the deployed registry subdomain),
// /registry when mounted on a shared origin (dev). The parse base is the
// canonical registry origin; it only anchors relative-URL parsing.
import { registryRoutePrefix } from "../../../components/shell/site-bases";

export { registryRoutePrefix };
export const urlParseBase = "https://registry.gestaltd.ai";

// Raw GitHub SVG responses include a sandboxing CSP, so render fetched SVGs as data URLs.
const svgIconDataUrlCache = new Map<string, Promise<string | null>>();
const providerDocTextCache = new Map<string, Promise<string | null>>();

// Kinds are internal identifiers; map the ones whose title-case reads wrong.
const kindLabelOverrides: Record<string, string> = {
  externalcredentials: "External Credentials",
  indexeddb: "IndexedDB",
  ui: "UI",
  oidc: "OIDC",
};

export function kindLabel(kind: string) {
  return (
    kindLabelOverrides[kind] ??
    kind.replace(/_/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase())
  );
}

export function pluralize(count: number, singular: string, plural: string) {
  return `${count} ${count === 1 ? singular : plural}`;
}

export function selectionFromPath(pathname: string): RouteSelection | null {
  const normalized = pathname.replace(/^\/registry/, "");
  const match = normalized.match(/^\/providers\/([^/]+)\/([^/]+)(?:\/(.*))?\/?$/);
  if (!match) {
    return null;
  }
  return {
    kind: decodeURIComponent(match[1]),
    name: decodeURIComponent(match[2]),
    docPath: normalizeDocPath(match[3] ? `/${decodeURIComponent(match[3])}/` : "/"),
  };
}

export function normalizeDocPath(path: string) {
  const clean = path.split(/[?#]/, 1)[0].replace(/^\/+/, "").replace(/\/+$/, "");
  return clean ? `/${clean}/` : "/";
}

export function providerHref(provider: Provider) {
  return `${registryRoutePrefix}${provider.registryPath}`;
}

export function providerDocHref(provider: Provider, doc: ProviderDoc) {
  const base = providerHref(provider);
  const docPath = normalizeDocPath(doc.path);
  if (docPath === "/") {
    return base;
  }
  return `${base.replace(/\/$/, "")}${docPath}`;
}

export function shouldHandleNavigationClick(
  event: MouseEvent<HTMLAnchorElement>,
) {
  return (
    event.button === 0 &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.shiftKey &&
    !event.altKey &&
    !event.defaultPrevented
  );
}

export function isSvgIconUrl(iconUrl: string) {
  try {
    return new URL(iconUrl, urlParseBase).pathname
      .toLowerCase()
      .endsWith(".svg");
  } catch {
    return iconUrl.toLowerCase().split("?")[0].endsWith(".svg");
  }
}

export function loadSvgIconDataUrl(iconUrl: string) {
  const cached = svgIconDataUrlCache.get(iconUrl);
  if (cached) {
    return cached;
  }
  const promise = fetch(iconUrl)
    .then(async (response) => {
      if (!response.ok) {
        return null;
      }
      const svg = (await response.text()).trim();
      if (!svg.includes("<svg")) {
        return null;
      }
      return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
    })
    .catch(() => null);
  svgIconDataUrlCache.set(iconUrl, promise);
  return promise;
}

export function loadProviderDocText(rawUrl: string) {
  const cached = providerDocTextCache.get(rawUrl);
  if (cached) {
    return cached;
  }
  const promise = fetch(rawUrl)
    .then(async (response) => {
      if (!response.ok) {
        return null;
      }
      return response.text();
    })
    .catch(() => null);
  providerDocTextCache.set(rawUrl, promise);
  return promise;
}

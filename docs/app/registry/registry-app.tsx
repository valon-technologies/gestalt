"use client";

import type { MouseEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { renderMarkdown } from "./registry-markdown";
import styles from "./registry.module.css";

type ConfigTarget = {
  section: string;
  entryKind: string;
  requiredSet?: Record<string, string>;
};

type ProviderVersion = {
  version: string;
  metadata: string;
  kind: string;
  runtime: string;
  platforms: string[];
  yanked?: boolean;
};

type ProviderDoc = {
  path: string;
  title: string;
  sourcePath: string;
  rawUrl: string;
  editUrl: string;
};

type Provider = {
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

type Catalog = {
  schema: string;
  schemaVersion: number;
  repository: string;
  indexUrl: string;
  providers: Provider[];
};

type LoadState =
  | { status: "loading" }
  | { status: "loaded"; catalog: Catalog }
  | { status: "error"; message: string };

type RouteSelection = {
  kind: string;
  name: string;
  docPath: string;
};

type DocLoadState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "loaded"; source: string }
  | { status: "error"; message: string };

type ThemePreference = "light" | "dark" | "system";
type ResolvedTheme = "light" | "dark";

const allKinds = "all";
const catalogSchemaVersion = 1;
const themeStorageKey = "gestalt-docs-theme";
const themeOptions: { label: string; value: ThemePreference }[] = [
  { label: "System", value: "system" },
  { label: "Light", value: "light" },
  { label: "Dark", value: "dark" },
];
const urlParseBase = "https://registry.gestaltd.ai";
// Raw GitHub SVG responses include a sandboxing CSP, so render fetched SVGs as data URLs.
const svgIconDataUrlCache = new Map<string, Promise<string | null>>();
const providerDocTextCache = new Map<string, Promise<string | null>>();

export default function RegistryApp({ catalogUrl }: { catalogUrl: string }) {
  const [loadState, setLoadState] = useState<LoadState>({ status: "loading" });
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState(allKinds);
  const [selectedRoute, setSelectedRoute] = useState<RouteSelection | null>(
    null,
  );
  const [routePrefix, setRoutePrefix] = useState("");
  const [themePreference, setThemePreference] =
    useState<ThemePreference>("system");
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>("light");

  useEffect(() => {
    const storedTheme = window.localStorage.getItem(themeStorageKey);
    if (isThemePreference(storedTheme)) {
      setThemePreference(storedTheme);
    }
  }, []);

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    const updateResolvedTheme = () => {
      setResolvedTheme(
        resolveThemePreference(themePreference, mediaQuery.matches),
      );
    };
    updateResolvedTheme();
    mediaQuery.addEventListener("change", updateResolvedTheme);
    return () => {
      mediaQuery.removeEventListener("change", updateResolvedTheme);
    };
  }, [themePreference]);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", resolvedTheme === "dark");
    document.documentElement.style.colorScheme = resolvedTheme;
  }, [resolvedTheme]);

  useEffect(() => {
    const updateRoute = () => {
      const pathname = window.location.pathname;
      const isDocsRegistryRoute =
        pathname === "/registry" ||
        pathname === "/registry.html" ||
        pathname.startsWith("/registry/");
      setRoutePrefix(isDocsRegistryRoute ? "/registry" : "");
      setSelectedRoute(selectionFromPath(pathname));
    };
    updateRoute();
    window.addEventListener("popstate", updateRoute);
    return () => window.removeEventListener("popstate", updateRoute);
  }, []);

  useEffect(() => {
    let cancelled = false;
    async function loadCatalog() {
      setLoadState({ status: "loading" });
      try {
        const response = await fetch(catalogUrl, { cache: "no-store" });
        if (!response.ok) {
          throw new Error(`catalog returned ${response.status}`);
        }
        const catalog = (await response.json()) as Catalog;
        if (catalog.schema !== "gestaltd-provider-catalog") {
          throw new Error("catalog schema mismatch");
        }
        if (catalog.schemaVersion !== catalogSchemaVersion) {
          throw new Error("catalog schema version mismatch");
        }
        if (!cancelled) {
          setLoadState({ status: "loaded", catalog });
        }
      } catch (error) {
        if (!cancelled) {
          setLoadState({
            status: "error",
            message:
              error instanceof Error ? error.message : "catalog unavailable",
          });
        }
      }
    }
    loadCatalog();
    return () => {
      cancelled = true;
    };
  }, [catalogUrl]);

  const providers = loadState.status === "loaded" ? loadState.catalog.providers : [];

  const kinds = useMemo(() => {
    const counts = new Map<string, number>();
    for (const provider of providers) {
      counts.set(provider.kind, (counts.get(provider.kind) ?? 0) + 1);
    }
    return Array.from(counts)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([value, count]) => ({ value, count }));
  }, [providers]);

  const filteredProviders = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return providers.filter((provider) => {
      if (kind !== allKinds && provider.kind !== kind) {
        return false;
      }
      if (!normalizedQuery) {
        return true;
      }
      const haystack = [
        provider.displayName,
        provider.name,
        provider.kind,
        provider.package,
        provider.description,
      ]
        .join(" ")
        .toLowerCase();
      return haystack.includes(normalizedQuery);
    });
  }, [kind, providers, query]);

  const selectedProvider = useMemo(() => {
    if (selectedRoute) {
      const byRoute = providers.find(
        (provider) =>
          provider.kind === selectedRoute.kind &&
          provider.name === selectedRoute.name,
      );
      if (byRoute) {
        return byRoute;
      }
    }
    return filteredProviders[0] ?? providers[0] ?? null;
  }, [filteredProviders, providers, selectedRoute]);

  function selectProvider(provider: Provider) {
    const href = `${routePrefix}${provider.registryPath}`;
    window.history.pushState(null, "", href);
    setSelectedRoute({ kind: provider.kind, name: provider.name, docPath: "/" });
  }

  function selectProviderDoc(provider: Provider, doc: ProviderDoc) {
    const href = providerDocHref(provider, doc, routePrefix);
    window.history.pushState(null, "", href);
    setSelectedRoute({
      kind: provider.kind,
      name: provider.name,
      docPath: normalizeDocPath(doc.path),
    });
  }

  function chooseThemePreference(nextThemePreference: ThemePreference) {
    setThemePreference(nextThemePreference);
    window.localStorage.setItem(themeStorageKey, nextThemePreference);
  }

  const shellClassName =
    resolvedTheme === "dark"
      ? `${styles.shell} ${styles.darkShell}`
      : `${styles.shell} ${styles.lightShell}`;

  return (
    <main className={shellClassName}>
      <header className={styles.header}>
        <a className={styles.brand} href={routePrefix || "/"}>
          <span className={styles.brandWord}>Gestalt</span>
          <span className={styles.brandDivider} aria-hidden="true" />
          <span className={styles.brandContext}>Provider Registry</span>
        </a>
        <div className={styles.headerActions}>
          <div className={styles.themeControl} aria-label="Theme">
            {themeOptions.map((option) => (
              <button
                aria-pressed={themePreference === option.value}
                className={
                  themePreference === option.value
                    ? styles.activeTheme
                    : undefined
                }
                key={option.value}
                onClick={() => chooseThemePreference(option.value)}
                type="button"
              >
                {option.label}
              </button>
            ))}
          </div>
          <nav className={styles.nav}>
            <a href="https://gestaltd.ai">Docs</a>
            <a href="https://github.com/valon-technologies/gestalt-providers">
              GitHub
            </a>
          </nav>
        </div>
      </header>

      <section className={styles.toolbar}>
        <label className={styles.search}>
          <span>Search</span>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="slack, indexeddb, runtime"
          />
        </label>
        <div className={styles.kindTabs} aria-label="Provider kind">
          <button
            className={kind === allKinds ? styles.activeTab : undefined}
            onClick={() => setKind(allKinds)}
            type="button"
          >
            All <span>{providers.length}</span>
          </button>
          {kinds.map((entry) => (
            <button
              className={kind === entry.value ? styles.activeTab : undefined}
              key={entry.value}
              onClick={() => setKind(entry.value)}
              type="button"
            >
              {kindLabel(entry.value)} <span>{entry.count}</span>
            </button>
          ))}
        </div>
      </section>

      {loadState.status === "error" ? (
        <section className={styles.statePanel}>
          <h1>Registry unavailable</h1>
          <p>{loadState.message}</p>
        </section>
      ) : (
        <section className={styles.content}>
          <div className={styles.listPane}>
            <div className={styles.listHeader}>
              <span>{loadState.status === "loaded" ? filteredProviders.length : 0} providers</span>
              <span>Installable packages</span>
            </div>
            {loadState.status === "loading" ? (
              <div className={styles.statePanel}>Loading registry</div>
            ) : (
              <div className={styles.providerList}>
                {filteredProviders.map((provider) => (
                  <button
                    type="button"
                    key={provider.package}
                    className={
                      selectedProvider?.package === provider.package
                        ? `${styles.providerCard} ${styles.selectedCard}`
                        : styles.providerCard
                    }
                    onClick={() => selectProvider(provider)}
                  >
                    <ProviderIcon provider={provider} />
                    <span className={styles.providerText}>
                      <strong>{provider.displayName}</strong>
                      <small>{provider.packagePath}</small>
                    </span>
                    <span className={styles.kindBadge}>{kindLabel(provider.kind)}</span>
                  </button>
                ))}
              </div>
            )}
          </div>

          <aside className={styles.detailPane}>
            {selectedProvider ? (
              <ProviderDetail
                onSelectDoc={(doc) => selectProviderDoc(selectedProvider, doc)}
                provider={selectedProvider}
                routePrefix={routePrefix}
                selectedDocPath={selectedRoute?.docPath ?? "/"}
              />
            ) : (
              <div className={styles.statePanel}>No provider selected</div>
            )}
          </aside>
        </section>
      )}
    </main>
  );
}

function isThemePreference(value: string | null): value is ThemePreference {
  return value === "light" || value === "dark" || value === "system";
}

function resolveThemePreference(
  themePreference: ThemePreference,
  systemPrefersDark: boolean,
): ResolvedTheme {
  if (themePreference === "system") {
    return systemPrefersDark ? "dark" : "light";
  }
  return themePreference;
}

function selectionFromPath(pathname: string): RouteSelection | null {
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

function ProviderDetail({
  onSelectDoc,
  provider,
  routePrefix,
  selectedDocPath,
}: {
  onSelectDoc: (doc: ProviderDoc) => void;
  provider: Provider;
  routePrefix: string;
  selectedDocPath: string;
}) {
  const latest = provider.versions[0];
  const installCommand = provider.configTarget.requiredSet?.path
    ? `gestaltd provider add ${provider.package} --set path=/`
    : `gestaltd provider add ${provider.package}`;
  return (
    <div className={styles.detail}>
      <div className={styles.detailTitle}>
        <ProviderIcon provider={provider} large />
        <div>
          <span className={styles.kindBadge}>{kindLabel(provider.kind)}</span>
          <h1 className={styles.providerName}>{provider.displayName}</h1>
          <p>{provider.description || provider.packagePath}</p>
        </div>
      </div>

      <div className={styles.installBlock}>
        <span>Install</span>
        <code>{installCommand}</code>
      </div>

      <dl className={styles.metaGrid}>
        <div>
          <dt>Package</dt>
          <dd>{provider.package}</dd>
        </div>
        <div>
          <dt>Config target</dt>
          <dd>{provider.configTarget.section}</dd>
        </div>
        <div>
          <dt>Latest</dt>
          <dd>{provider.latestInstallableVersion ?? "Not released"}</dd>
        </div>
        <div>
          <dt>Manifest</dt>
          <dd>{provider.manifestVersion ?? "Release only"}</dd>
        </div>
      </dl>

      {latest ? (
        <section className={styles.versionPanel}>
          <div className={styles.panelTitle}>
            <h2>{latest.version}</h2>
            <span>{latest.runtime}</span>
          </div>
          <div className={styles.platforms}>
            {latest.platforms.map((platform) => (
              <span key={platform}>
                {platform === "generic" ? "all platforms" : platform}
              </span>
            ))}
          </div>
        </section>
      ) : null}

      <section className={styles.versionPanel}>
        <div className={styles.panelTitle}>
          <h2>Versions</h2>
          <span>{provider.versions.length}</span>
        </div>
        <div className={styles.versionList}>
          {provider.versions.slice(0, 12).map((version) => (
            <a href={version.metadata} key={version.version}>
              <span>{version.version}</span>
              <small>{version.platforms.length} targets</small>
            </a>
          ))}
        </div>
      </section>

      <ProviderDocs
        onSelectDoc={onSelectDoc}
        provider={provider}
        routePrefix={routePrefix}
        selectedDocPath={selectedDocPath}
      />

      <div className={styles.links}>
        {provider.sourceUrl ? <a href={provider.sourceUrl}>Source</a> : null}
        {provider.readmeUrl ? <a href={provider.readmeUrl}>README</a> : null}
        {provider.manifestUrl ? <a href={provider.manifestUrl}>Manifest</a> : null}
      </div>
    </div>
  );
}

function ProviderDocs({
  onSelectDoc,
  provider,
  routePrefix,
  selectedDocPath,
}: {
  onSelectDoc: (doc: ProviderDoc) => void;
  provider: Provider;
  routePrefix: string;
  selectedDocPath: string;
}) {
  const docs = provider.docs ?? [];
  if (docs.length === 0) {
    return null;
  }
  const normalizedSelectedPath = normalizeDocPath(selectedDocPath);
  const selectedDoc =
    docs.find((doc) => normalizeDocPath(doc.path) === normalizedSelectedPath) ??
    (normalizedSelectedPath === "/" ? docs[0] : null);

  return (
    <section className={styles.docsPanel}>
      <div className={styles.docsHeader}>
        <h2>Documentation</h2>
        {selectedDoc?.editUrl ? <a href={selectedDoc.editUrl}>Edit</a> : null}
      </div>
      <nav className={styles.docsNav} aria-label={`${provider.displayName} docs`}>
        {docs.map((doc) => {
          const isActive =
            selectedDoc !== null &&
            normalizeDocPath(doc.path) === normalizeDocPath(selectedDoc.path);
          return (
            <a
              aria-current={isActive ? "page" : undefined}
              className={isActive ? styles.activeDocLink : undefined}
              href={providerDocHref(provider, doc, routePrefix)}
              key={doc.path}
              onClick={(event) => {
                if (!shouldHandleDocNavigationClick(event)) {
                  return;
                }
                event.preventDefault();
                onSelectDoc(doc);
              }}
            >
              {doc.title}
            </a>
          );
        })}
      </nav>
      {selectedDoc ? (
        <ProviderDocBody
          docs={docs}
          provider={provider}
          routePrefix={routePrefix}
          selectedDoc={selectedDoc}
        />
      ) : (
        <div className={styles.docsState}>
          <h3>Documentation page not found</h3>
          <p>Select one of the available provider docs pages.</p>
        </div>
      )}
    </section>
  );
}

function shouldHandleDocNavigationClick(event: MouseEvent<HTMLAnchorElement>) {
  return (
    event.button === 0 &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.shiftKey &&
    !event.altKey &&
    !event.defaultPrevented
  );
}

function ProviderDocBody({
  docs,
  provider,
  routePrefix,
  selectedDoc,
}: {
  docs: ProviderDoc[];
  provider: Provider;
  routePrefix: string;
  selectedDoc: ProviderDoc;
}) {
  const loadState = useProviderDocSource(selectedDoc);
  if (loadState.status === "loading" || loadState.status === "idle") {
    return <div className={styles.docsState}>Loading documentation</div>;
  }
  if (loadState.status === "error") {
    return (
      <div className={styles.docsState}>
        <h3>Documentation unavailable</h3>
        <p>{loadState.message}</p>
      </div>
    );
  }
  return (
    <div className={styles.markdownBody}>
      {renderMarkdown(loadState.source, {
        resolveLink: (href) =>
          resolveProviderDocLink(href, provider, docs, selectedDoc, routePrefix),
      })}
    </div>
  );
}

function ProviderIcon({
  provider,
  large = false,
}: {
  provider: Provider;
  large?: boolean;
}) {
  const renderableIconUrl = useRenderableIconUrl(provider.iconUrl);

  if (renderableIconUrl) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        alt=""
        className={large ? styles.largeIcon : styles.icon}
        src={renderableIconUrl}
      />
    );
  }
  return (
    <span className={large ? styles.largeFallbackIcon : styles.fallbackIcon}>
      {provider.displayName.slice(0, 1).toUpperCase()}
    </span>
  );
}

function useRenderableIconUrl(iconUrl: string | null) {
  const [renderableIconUrl, setRenderableIconUrl] = useState<string | null>(
    iconUrl && !isSvgIconUrl(iconUrl) ? iconUrl : null,
  );

  useEffect(() => {
    let cancelled = false;
    if (!iconUrl) {
      setRenderableIconUrl(null);
      return () => {
        cancelled = true;
      };
    }
    if (!isSvgIconUrl(iconUrl)) {
      setRenderableIconUrl(iconUrl);
      return () => {
        cancelled = true;
      };
    }
    setRenderableIconUrl(null);
    void loadSvgIconDataUrl(iconUrl).then((dataUrl) => {
      if (!cancelled) {
        setRenderableIconUrl(dataUrl);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [iconUrl]);

  return renderableIconUrl;
}

function isSvgIconUrl(iconUrl: string) {
  try {
    return new URL(iconUrl, urlParseBase).pathname
      .toLowerCase()
      .endsWith(".svg");
  } catch {
    return iconUrl.toLowerCase().split("?")[0].endsWith(".svg");
  }
}

function loadSvgIconDataUrl(iconUrl: string) {
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

function useProviderDocSource(doc: ProviderDoc) {
  const [loadState, setLoadState] = useState<DocLoadState>({ status: "idle" });

  useEffect(() => {
    let cancelled = false;
    setLoadState({ status: "loading" });
    void loadProviderDocText(doc.rawUrl).then((source) => {
      if (cancelled) {
        return;
      }
      if (source === null) {
        setLoadState({
          status: "error",
          message: `Unable to load ${doc.sourcePath}`,
        });
        return;
      }
      setLoadState({ status: "loaded", source });
    });
    return () => {
      cancelled = true;
    };
  }, [doc.rawUrl, doc.sourcePath]);

  return loadState;
}

function loadProviderDocText(rawUrl: string) {
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

function normalizeDocPath(path: string) {
  const clean = path.split(/[?#]/, 1)[0].replace(/^\/+/, "").replace(/\/+$/, "");
  return clean ? `/${clean}/` : "/";
}

function providerDocHref(provider: Provider, doc: ProviderDoc, routePrefix: string) {
  const base = `${routePrefix}${provider.registryPath}`;
  const docPath = normalizeDocPath(doc.path);
  if (docPath === "/") {
    return base;
  }
  return `${base.replace(/\/$/, "")}${docPath}`;
}

function resolveProviderDocLink(
  href: string,
  provider: Provider,
  docs: ProviderDoc[],
  currentDoc: ProviderDoc,
  routePrefix: string,
) {
  const trimmed = href.trim();
  if (!trimmed) {
    return null;
  }
  if (trimmed.startsWith("#")) {
    return { href: trimmed, external: false };
  }
  try {
    const parsed = new URL(trimmed);
    if (["http:", "https:", "mailto:"].includes(parsed.protocol)) {
      return { href: trimmed, external: parsed.protocol !== "mailto:" };
    }
    return null;
  } catch {
    // Relative links are handled below.
  }
  if (trimmed.startsWith("/providers/")) {
    return { href: `${routePrefix}${trimmed}`, external: false };
  }
  if (trimmed.startsWith("/")) {
    return { href: `https://gestaltd.ai${trimmed}`, external: true };
  }

  const [withoutHash, hash = ""] = trimmed.split("#", 2);
  const targetDoc = docForRelativeHref(withoutHash, docs, currentDoc);
  if (targetDoc) {
    return {
      href: `${providerDocHref(provider, targetDoc, routePrefix)}${hash ? `#${hash}` : ""}`,
      external: false,
    };
  }
  return null;
}

function docForRelativeHref(
  href: string,
  docs: ProviderDoc[],
  currentDoc: ProviderDoc,
) {
  const cleanHref = href.split("?", 1)[0].replace(/^\.\//, "");
  if (!cleanHref) {
    return currentDoc;
  }
  if (!cleanHref.endsWith(".md") && !cleanHref.endsWith(".mdx")) {
    return null;
  }
  const fileName = cleanHref.split("/").pop() ?? "";
  const stem = fileName.replace(/\.mdx?$/, "");
  const docPath = stem === "index" ? "/" : `/${stem}/`;
  return docs.find((doc) => normalizeDocPath(doc.path) === docPath) ?? null;
}

function kindLabel(kind: string) {
  return kind
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

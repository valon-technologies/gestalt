"use client";

import { useEffect, useMemo, useState } from "react";
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
};

const allKinds = "all";
const catalogSchemaVersion = 1;
const urlParseBase = "https://registry.gestaltd.ai";
// Raw GitHub SVG responses include a sandboxing CSP, so render fetched SVGs as data URLs.
const svgIconDataUrlCache = new Map<string, Promise<string | null>>();

export default function RegistryApp({ catalogUrl }: { catalogUrl: string }) {
  const [loadState, setLoadState] = useState<LoadState>({ status: "loading" });
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState(allKinds);
  const [selectedRoute, setSelectedRoute] = useState<RouteSelection | null>(
    null,
  );
  const [routePrefix, setRoutePrefix] = useState("");

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
    setSelectedRoute({ kind: provider.kind, name: provider.name });
  }

  return (
    <main className={styles.shell}>
      <header className={styles.header}>
        <a className={styles.brand} href={routePrefix || "/"}>
          <span className={styles.brandWord}>Gestalt</span>
          <span className={styles.brandDivider} aria-hidden="true" />
          <span className={styles.brandContext}>Provider Registry</span>
        </a>
        <nav className={styles.nav}>
          <a href="https://gestaltd.ai">Docs</a>
          <a href="https://github.com/valon-technologies/gestalt-providers">
            GitHub
          </a>
        </nav>
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
              <ProviderDetail provider={selectedProvider} />
            ) : (
              <div className={styles.statePanel}>No provider selected</div>
            )}
          </aside>
        </section>
      )}
    </main>
  );
}

function selectionFromPath(pathname: string): RouteSelection | null {
  const normalized = pathname.replace(/^\/registry/, "");
  const match = normalized.match(/^\/providers\/([^/]+)\/([^/]+)\/?$/);
  if (!match) {
    return null;
  }
  return {
    kind: decodeURIComponent(match[1]),
    name: decodeURIComponent(match[2]),
  };
}

function ProviderDetail({ provider }: { provider: Provider }) {
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

      <div className={styles.links}>
        {provider.sourceUrl ? <a href={provider.sourceUrl}>Source</a> : null}
        {provider.readmeUrl ? <a href={provider.readmeUrl}>README</a> : null}
        {provider.manifestUrl ? <a href={provider.manifestUrl}>Manifest</a> : null}
      </div>
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

function kindLabel(kind: string) {
  return kind
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

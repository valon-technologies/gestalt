"use client";

import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import RegistryBrowse from "./registry-browse";
import RegistryDetail from "./registry-detail";
import {
  type Catalog,
  type Provider,
  type ProviderDoc,
  type RouteSelection,
  allKinds,
  catalogSchemaVersion,
  kindLabel,
  normalizeDocPath,
  providerDocHref,
  providerHref,
  registryBrowsePath,
  selectionFromPath,
  shouldHandleNavigationClick,
} from "./registry-data";
import styles from "./registry.module.css";

type LoadState =
  | { status: "loading" }
  | { status: "loaded"; catalog: Catalog }
  | { status: "error"; message: string };

const baseTitle = "Provider Registry – Gestalt";

function filtersFromSearch(search: string) {
  const params = new URLSearchParams(search);
  return {
    kind: params.get("kind") ?? allKinds,
    query: params.get("q") ?? "",
  };
}

export default function RegistryApp({ catalogUrl }: { catalogUrl: string }) {
  const [loadState, setLoadState] = useState<LoadState>({ status: "loading" });
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState(allKinds);
  const [selectedRoute, setSelectedRoute] = useState<RouteSelection | null>(
    null,
  );

  useEffect(() => {
    const updateFromLocation = () => {
      setSelectedRoute(selectionFromPath(window.location.pathname));
      const filters = filtersFromSearch(window.location.search);
      setKind(filters.kind);
      setQuery(filters.query);
    };
    updateFromLocation();
    window.addEventListener("popstate", updateFromLocation);
    return () => window.removeEventListener("popstate", updateFromLocation);
  }, []);

  // Browse filters are shareable URL state (?kind=…&q=…). This effect
  // syncs state back INTO the URL, so it must skip its first run: on
  // mount the state was just hydrated FROM the URL, and the route state
  // queued by the location effect above is not visible to this closure
  // yet — syncing here would rewrite a deep provider URL to the browse
  // path before React re-renders.
  const syncedFiltersOnce = useRef(false);
  useEffect(() => {
    if (!syncedFiltersOnce.current) {
      syncedFiltersOnce.current = true;
      return;
    }
    if (selectedRoute !== null) {
      return;
    }
    const params = new URLSearchParams();
    if (kind !== allKinds) {
      params.set("kind", kind);
    }
    if (query.trim()) {
      params.set("q", query.trim());
    }
    const search = params.toString();
    const next = `${registryBrowsePath}${search ? `?${search}` : ""}`;
    if (`${window.location.pathname}${window.location.search}` !== next) {
      window.history.replaceState(null, "", next);
    }
  }, [kind, query, selectedRoute]);

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

  const selectedProvider = useMemo(() => {
    if (!selectedRoute) {
      return null;
    }
    return (
      providers.find(
        (provider) =>
          provider.kind === selectedRoute.kind &&
          provider.name === selectedRoute.name,
      ) ?? null
    );
  }, [providers, selectedRoute]);

  useEffect(() => {
    document.title = selectedProvider
      ? `${selectedProvider.displayName} – ${baseTitle}`
      : baseTitle;
  }, [selectedProvider]);

  function selectProvider(provider: Provider) {
    window.history.pushState(null, "", providerHref(provider));
    setSelectedRoute({ kind: provider.kind, name: provider.name, docPath: "/" });
    window.scrollTo(0, 0);
  }

  function selectProviderDoc(provider: Provider, doc: ProviderDoc) {
    window.history.pushState(null, "", providerDocHref(provider, doc));
    setSelectedRoute({
      kind: provider.kind,
      name: provider.name,
      docPath: normalizeDocPath(doc.path),
    });
  }

  function backToBrowse() {
    window.history.pushState(null, "", registryBrowsePath);
    setSelectedRoute(null);
    setKind(allKinds);
    setQuery("");
    window.scrollTo(0, 0);
  }

  let view: ReactNode;
  if (loadState.status === "error") {
    view = (
      <section className={styles.statePanel}>
        <h1>Registry unavailable</h1>
        <p>{loadState.message}</p>
      </section>
    );
  } else if (selectedRoute === null) {
    view = (
      <RegistryBrowse
        providers={providers}
        loading={loadState.status === "loading"}
        kind={kind}
        query={query}
        onKindChange={setKind}
        onQueryChange={setQuery}
        onSelectProvider={selectProvider}
      />
    );
  } else if (loadState.status === "loading") {
    view = <section className={styles.statePanel}>Loading registry</section>;
  } else if (!selectedProvider) {
    view = (
      <section className={styles.statePanel}>
        <h1>Provider not found</h1>
        <p>
          No {kindLabel(selectedRoute.kind)} provider named “
          {selectedRoute.name}” exists in the registry.
        </p>
        <p>
          <a
            className="shell-link"
            href={registryBrowsePath}
            onClick={(event) => {
              if (!shouldHandleNavigationClick(event)) {
                return;
              }
              event.preventDefault();
              backToBrowse();
            }}
          >
            ← All providers
          </a>
        </p>
      </section>
    );
  } else {
    view = (
      <RegistryDetail
        provider={selectedProvider}
        selectedDocPath={selectedRoute.docPath}
        onSelectDoc={(doc) => selectProviderDoc(selectedProvider, doc)}
        onBackToBrowse={backToBrowse}
      />
    );
  }

  return (
    <main className={styles.shell} data-registry-shell="true">
      {view}
    </main>
  );
}

"use client";

import { useMemo } from "react";
import ProviderIcon from "./provider-icon";
import {
  type Provider,
  allKinds,
  kindLabel,
  pluralize,
  providerHref,
  shouldHandleNavigationClick,
} from "./registry-data";
import styles from "./registry.module.css";

export default function RegistryBrowse({
  providers,
  loading,
  kind,
  query,
  onKindChange,
  onQueryChange,
  onSelectProvider,
}: {
  providers: Provider[];
  loading: boolean;
  kind: string;
  query: string;
  onKindChange: (kind: string) => void;
  onQueryChange: (query: string) => void;
  onSelectProvider: (provider: Provider) => void;
}) {
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

  const hasFilters = kind !== allKinds || query.trim() !== "";

  return (
    <>
      <header className={styles.pageHeader}>
        <h1>Provider registry</h1>
        <p>
          Every integration gestaltd can run —{" "}
          {loading ? "providers" : pluralize(providers.length, "provider", "providers")}{" "}
          across apps like Slack and GitHub, auth, storage, and runtimes.
          Install any of them with <code>gestaltd provider add</code>.
        </p>
      </header>
      <section className={styles.toolbar}>
        <div className={styles.kindTabs} aria-label="Provider kind">
          <button
            className={kind === allKinds ? styles.activeTab : undefined}
            onClick={() => onKindChange(allKinds)}
            type="button"
          >
            All <span>{providers.length}</span>
          </button>
          {kinds.map((entry) => (
            <button
              className={kind === entry.value ? styles.activeTab : undefined}
              key={entry.value}
              onClick={() => onKindChange(entry.value)}
              type="button"
            >
              {kindLabel(entry.value)} <span>{entry.count}</span>
            </button>
          ))}
        </div>
        <input
          aria-label="Filter providers"
          className={styles.searchInput}
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder="Filter providers…"
        />
      </section>

      <section className={styles.browse}>
        {loading ? (
          <div className={styles.statePanel}>Loading registry</div>
        ) : (
          <>
            {filteredProviders.length !== providers.length ? (
              <div className={styles.resultsBar}>
                <span>
                  {filteredProviders.length} of{" "}
                  {pluralize(providers.length, "provider", "providers")}
                </span>
              </div>
            ) : null}
            {filteredProviders.length === 0 ? (
              <div className={styles.emptyState}>
                <h2>No providers match</h2>
                <p>
                  Nothing in the catalog matches
                  {query.trim() ? <> “{query.trim()}”</> : null}
                  {kind !== allKinds ? <> in {kindLabel(kind)}</> : null}.
                </p>
                {hasFilters ? (
                  <button
                    className="shell-button"
                    onClick={() => {
                      onKindChange(allKinds);
                      onQueryChange("");
                    }}
                    type="button"
                  >
                    Clear filters
                  </button>
                ) : null}
              </div>
            ) : (
              <div className={styles.providerGrid}>
                {filteredProviders.map((provider) => (
                  <a
                    className={`${styles.providerCard} shell-card`}
                    href={providerHref(provider)}
                    key={provider.package}
                    onClick={(event) => {
                      if (!shouldHandleNavigationClick(event)) {
                        return;
                      }
                      event.preventDefault();
                      onSelectProvider(provider);
                    }}
                  >
                    <span className={styles.cardHead}>
                      <ProviderIcon provider={provider} />
                      <span className={styles.providerText}>
                        <strong>{provider.displayName}</strong>
                        <small>{provider.packagePath}</small>
                      </span>
                      <span className={`${styles.kindBadge} shell-pill`}>
                        {kindLabel(provider.kind)}
                      </span>
                    </span>
                    {provider.description ? (
                      <span className={styles.cardDescription}>
                        {provider.description}
                      </span>
                    ) : null}
                  </a>
                ))}
              </div>
            )}
          </>
        )}
      </section>
    </>
  );
}

"use client";

import { useEffect, useState } from "react";
import { renderMarkdown } from "./registry-markdown";
import ProviderIcon from "./provider-icon";
import ShikiCode from "./shiki-code";
import {
  type Provider,
  type ProviderDoc,
  docsBaseUrl,
  kindLabel,
  loadProviderDocText,
  normalizeDocPath,
  pluralize,
  providerDocHref,
  registryBrowsePath,
  registryRoutePrefix,
  shouldHandleNavigationClick,
} from "./registry-data";
import styles from "./registry.module.css";

type DocLoadState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "loaded"; source: string }
  | { status: "error"; message: string };

function DownloadIcon() {
  return (
    <svg
      aria-hidden="true"
      fill="none"
      height="14"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth="24"
      viewBox="0 0 256 256"
      width="14"
    >
      <path d="M224 152v56a8 8 0 0 1-8 8H40a8 8 0 0 1-8-8v-56" />
      <polyline points="88 104 128 144 168 104" />
      <line x1="128" y1="40" x2="128" y2="144" />
    </svg>
  );
}

export default function RegistryDetail({
  provider,
  selectedDocPath,
  onSelectDoc,
  onBackToBrowse,
}: {
  provider: Provider;
  selectedDocPath: string;
  onSelectDoc: (doc: ProviderDoc) => void;
  onBackToBrowse: () => void;
}) {
  const latest = provider.versions[0];
  const installCommand = provider.configTarget.requiredSet?.path
    ? `gestaltd provider add ${provider.package} --set path=/`
    : `gestaltd provider add ${provider.package}`;
  return (
    <section className={styles.detailPage}>
      <div className={styles.detailColumn}>
        <a
          className="shell-link"
          href={registryBrowsePath}
          onClick={(event) => {
            if (!shouldHandleNavigationClick(event)) {
              return;
            }
            event.preventDefault();
            onBackToBrowse();
          }}
        >
          ← All providers
        </a>

        <div className={styles.detail}>
          <div className={styles.detailTitle}>
            <ProviderIcon provider={provider} large />
            <div className={styles.detailHeading}>
              <h1 className={styles.providerName}>{provider.displayName}</h1>
              <span className={`${styles.kindBadge} shell-pill`}>
                {kindLabel(provider.kind)}
              </span>
            </div>
            <p>{provider.description || provider.packagePath}</p>
          </div>

          <div className={styles.installBlock}>
            <span className="shell-eyebrow">Install</span>
            <code>{installCommand}</code>
          </div>

          <dl className={styles.metaGrid}>
            <div>
              <dt className="shell-eyebrow">Package</dt>
              <dd>
                <a className="shell-link" href={`https://${provider.package}`}>
                  {provider.package}
                </a>
              </dd>
            </div>
            <div>
              <dt className="shell-eyebrow">Config target</dt>
              <dd>{provider.configTarget.section}</dd>
            </div>
            <div>
              <dt className="shell-eyebrow">Latest</dt>
              <dd>{provider.latestInstallableVersion ?? "Not released"}</dd>
            </div>
            <div>
              <dt className="shell-eyebrow">Manifest</dt>
              <dd>{provider.manifestVersion ?? "Release only"}</dd>
            </div>
          </dl>

          {latest ? (
            <section>
              <div className={styles.panelTitle}>
                <h2>{latest.version}</h2>
                <span className="shell-pill">{latest.runtime}</span>
              </div>
              <div className={styles.platforms}>
                {latest.platforms.map((platform) => (
                  <span className="shell-pill" key={platform}>
                    {platform === "generic" ? "all platforms" : platform}
                  </span>
                ))}
              </div>
            </section>
          ) : null}

          <section>
            <div className={styles.panelTitle}>
              <h2>Versions</h2>
              <span>{provider.versions.length}</span>
            </div>
            <div className={styles.versionList}>
              {provider.versions.slice(0, 12).map((version) => (
                <div className={styles.versionRow} key={version.version}>
                  <a className="shell-link" href={version.metadata}>
                    <DownloadIcon />
                    {version.version}
                  </a>
                  <small>
                    {pluralize(version.platforms.length, "platform", "platforms")}
                  </small>
                </div>
              ))}
            </div>
          </section>

          <ProviderDocs
            onSelectDoc={onSelectDoc}
            provider={provider}
            selectedDocPath={selectedDocPath}
          />

          <div className={styles.links}>
            {provider.sourceUrl ? (
              <a className="shell-button" href={provider.sourceUrl}>
                Source
              </a>
            ) : null}
            {provider.readmeUrl ? (
              <a className="shell-button" href={provider.readmeUrl}>
                README
              </a>
            ) : null}
            {provider.manifestUrl ? (
              <a className="shell-button" href={provider.manifestUrl}>
                Manifest
              </a>
            ) : null}
          </div>
        </div>
      </div>
    </section>
  );
}

function ProviderDocs({
  onSelectDoc,
  provider,
  selectedDocPath,
}: {
  onSelectDoc: (doc: ProviderDoc) => void;
  provider: Provider;
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
        <h2 className="shell-eyebrow">Documentation</h2>
        {selectedDoc?.editUrl ? (
          <a className="shell-link" href={selectedDoc.editUrl}>
            Edit
          </a>
        ) : null}
      </div>
      {docs.length > 1 ? (
        <nav className={styles.docsNav} aria-label={`${provider.displayName} docs`}>
          {docs.map((doc) => {
            const isActive =
              selectedDoc !== null &&
              normalizeDocPath(doc.path) === normalizeDocPath(selectedDoc.path);
            return (
              <a
                aria-current={isActive ? "page" : undefined}
                className="shell-link"
                href={providerDocHref(provider, doc)}
                key={doc.path}
                onClick={(event) => {
                  if (!shouldHandleNavigationClick(event)) {
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
      ) : null}
      {selectedDoc ? (
        <ProviderDocBody
          docs={docs}
          provider={provider}
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

function ProviderDocBody({
  docs,
  provider,
  selectedDoc,
}: {
  docs: ProviderDoc[];
  provider: Provider;
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
          resolveProviderDocLink(href, provider, docs, selectedDoc),
        renderCode: (language, text, key) => (
          <ShikiCode key={key} language={language} text={text} />
        ),
      })}
    </div>
  );
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

function resolveProviderDocLink(
  href: string,
  provider: Provider,
  docs: ProviderDoc[],
  currentDoc: ProviderDoc,
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
    return { href: `${registryRoutePrefix}${trimmed}`, external: false };
  }
  if (trimmed.startsWith("/")) {
    // Absolute site paths are docs links; docsBaseUrl is the deployed
    // docs origin (empty in dev, where both halves share one origin).
    return { href: `${docsBaseUrl}${trimmed}`, external: docsBaseUrl !== "" };
  }

  const [withoutHash, hash = ""] = trimmed.split("#", 2);
  const targetDoc = docForRelativeHref(withoutHash, docs, currentDoc);
  if (targetDoc) {
    return {
      href: `${providerDocHref(provider, targetDoc)}${hash ? `#${hash}` : ""}`,
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

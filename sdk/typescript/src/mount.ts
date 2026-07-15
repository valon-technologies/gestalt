/**
 * Resolves the absolute mount prefix for the running app.
 *
 * @module mount
 */

const APP_NAME_META = "gestalt-app-name";

/**
 * Returns the exact app key under which gestaltd registered the mounted UI.
 *
 * Browser-only. Throws when gestaltd app metadata is unavailable.
 */
export function appName(): string {
  if (typeof document === "undefined") {
    throw new Error("appName() requires a browser document");
  }

  const meta = document.querySelector<HTMLMetaElement>(
    `meta[name="${APP_NAME_META}"]`,
  );
  const name = meta?.content.trim();

  if (!name) {
    throw new Error(
      'appName() requires <meta name="gestalt-app-name" content="..."> injected by gestaltd',
    );
  }

  return name;
}

/**
 * Returns the current app's operation API prefix.
 *
 * Example: "/api/v1/ciCd"
 */
export function appApiBase(): string {
  return `/api/v1/${encodeURIComponent(appName())}`;
}

/**
 * Returns the base path for absolute links from application code.
 *
 * gestaltd injects a base element with an href attribute into the served
 * HTML (via `injectAppContext`); this reads that element's path.
 */
export function base(): string {
  if (typeof document === "undefined") {
    throw new Error("base() requires a browser document");
  }
  const baseEl = document.querySelector("base[href]");
  if (!baseEl || baseEl.tagName !== "BASE") {
    throw new Error("base() requires <base href> to be present in the document");
  }
  const href = (baseEl as HTMLBaseElement).href.trim();
  if (!href) {
    throw new Error("base() requires <base href> to be present in the document");
  }
  const pathname = new URL(href).pathname.replace(/\/+$/, "");
  return pathname;
}

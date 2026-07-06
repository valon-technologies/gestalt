/**
 * Resolves the absolute mount prefix for the running app.
 *
 * @module mount
 */

/**
 * Returns the base path for absolute links from application code.
 *
 * gestaltd injects a base element with an href attribute into the served
 * HTML (via `injectBaseHref`); this reads that element's path.
 */
export function base(): string {
  if (typeof document === "undefined") {
    throw new Error("base() requires a browser document");
  }
  const baseEl = document.querySelector("base[href]");
  if (!baseEl || baseEl.tagName !== "BASE") {
    throw new Error("base() requires <base href> to be present in the document");
  }
  const href = baseEl.getAttribute("href")?.trim();
  if (!href) {
    throw new Error("base() requires <base href> to be present in the document");
  }
  const pathname = new URL(href, "http://localhost").pathname.replace(/\/+$/, "");
  return pathname;
}

// Build-time constants describing where the two halves of the site live.
// The deployed site serves the registry from its own origin
// (registry.gestaltd.ai) and the docs from gestaltd.ai; dev serves both
// halves from one origin, with the registry mounted under /registry.
//
// NEXT_PUBLIC_REGISTRY_BASE_URL: an absolute URL means the registry
// serves from that origin's root; a path means it is mounted under that
// prefix on the current origin (the dev default).
export const registryBaseUrl =
  process.env.NEXT_PUBLIC_REGISTRY_BASE_URL || "/registry";

// The registry SPA's route prefix follows from the mount: served from an
// origin root there is no prefix to strip or prepend.
export const registryRoutePrefix = registryBaseUrl.startsWith("/")
  ? registryBaseUrl
  : "";

// The browse page's own URL — the prefix when mounted under one, the
// origin root otherwise. History APIs and hrefs treat an empty string as
// "stay where you are", so the root must be spelled "/".
export const registryBrowsePath = registryRoutePrefix || "/";

// Prefix for links from the registry origin back to docs routes; empty
// when both halves share an origin.
export const docsBaseUrl = process.env.NEXT_PUBLIC_DOCS_BASE_URL || "";

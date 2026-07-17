/**
 * External gestaltd address normalization and REST URL joining.
 */

export function normalizeAddress(address: string | URL): string {
  const parsed = address instanceof URL ? address : parseAddressString(address);
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error(
      `address must use http or https: ${JSON.stringify(parsed.protocol)}`,
    );
  }
  return parsed.toString().replace(/\/$/, "");
}

/**
 * Join a deployment base URL with a REST path without dropping path prefixes.
 */
export function appendRestPath(
  baseUrl: string,
  path: string,
  query?: ReadonlyArray<readonly [string, string]>,
): string {
  const parsed = new URL(baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`);
  const basePath = parsed.pathname.replace(/\/$/, "");
  const restPath = path.startsWith("/") ? path : `/${path}`;
  parsed.pathname = `${basePath}${restPath}`;
  parsed.search = "";
  if (query) {
    for (const [key, value] of query) {
      parsed.searchParams.append(key, value);
    }
  }
  return parsed.toString();
}

function parseAddressString(address: string): URL {
  const trimmed = address.trim();
  if (!trimmed) {
    throw new Error("address is required");
  }
  try {
    return new URL(trimmed);
  } catch {
    throw new Error(`address must be an absolute URL: ${JSON.stringify(address)}`);
  }
}

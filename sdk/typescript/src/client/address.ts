/**
 * External gestaltd address normalization and validation.
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

/**
 * Address normalization for public Gestalt clients.
 */

export function normalizeClientAddress(address?: string | URL): string {
  if (address === undefined) {
    const location = (globalThis as { location?: { origin?: string } }).location;
    if (location?.origin) {
      return `${location.origin.replace(/\/+$/, "")}/`;
    }
    throw new Error(
      "address is required when no browser location is available (use bound provider gRPC via request.gestalt())",
    );
  }
  const raw = typeof address === "string" ? address.trim() : address.toString().trim();
  if (!raw) {
    throw new Error("address must not be empty");
  }
  const parsed = new URL(raw);
  if (parsed.search || parsed.hash) {
    throw new Error("address must not include query strings or fragments");
  }
  return `${parsed.origin.replace(/\/+$/, "")}/`;
}

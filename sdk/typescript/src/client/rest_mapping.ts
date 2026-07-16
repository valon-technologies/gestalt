/**
 * Protobuf-JSON request mapping for the public REST transport.
 */

import type { JsonValue } from "@bufbuild/protobuf";

import type { PublicMethod } from "./generated/methods.ts";

export function snakeToCamel(name: string): string {
  return name.replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase());
}

export function resolvePathValue(
  request: Record<string, JsonValue>,
  fieldName: string,
): JsonValue | undefined {
  if (fieldName in request) {
    return request[fieldName];
  }
  const camel = snakeToCamel(fieldName);
  if (camel in request) {
    return request[camel];
  }
  return undefined;
}

export function buildRestPath(
  template: string,
  request: Record<string, JsonValue>,
): { path: string; pathFields: Set<string> } {
  const pathFields = new Set<string>();
  const path = template.replace(/\{([^}]+)\}/g, (_match, field: string) => {
    pathFields.add(field);
    pathFields.add(snakeToCamel(field));
    const value = resolvePathValue(request, field);
    if (value === undefined || value === null) {
      throw new Error(`missing path parameter ${field}`);
    }
    if (typeof value === "object") {
      throw new Error(`path parameter ${field} must be scalar`);
    }
    return encodeURIComponent(String(value));
  });
  return { path, pathFields };
}

export function appendQueryParams(
  target: URL,
  prefix: string,
  value: JsonValue,
): void {
  if (value === undefined || value === null) {
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      if (item === undefined || item === null) {
        continue;
      }
      if (typeof item === "object") {
        throw new Error(`repeated query field ${prefix} must contain scalars`);
      }
      target.searchParams.append(prefix, String(item));
    }
    return;
  }
  if (typeof value === "object") {
    for (const [key, nested] of Object.entries(value)) {
      appendQueryParams(target, prefix ? `${prefix}.${key}` : key, nested);
    }
    return;
  }
  target.searchParams.append(prefix, String(value));
}

export function buildRestQuery(
  request: Record<string, JsonValue>,
  pathFields: Set<string>,
): URLSearchParams {
  const params = new URLSearchParams();
  const url = new URL("http://gestalt.local/");
  for (const [key, value] of Object.entries(request)) {
    if (pathFields.has(key) || value === undefined || value === null) {
      continue;
    }
    appendQueryParams(url, key, value);
  }
  url.searchParams.forEach((value, key) => {
    params.append(key, value);
  });
  return params;
}

export function buildRestBody(
  http: NonNullable<PublicMethod["http"]>,
  request: Record<string, JsonValue>,
  pathFields: Set<string>,
): JsonValue | undefined {
  if (http.verb === "GET" || http.verb === "DELETE") {
    return undefined;
  }
  if (http.body === "*") {
    const body: Record<string, JsonValue> = {};
    for (const [key, value] of Object.entries(request)) {
      if (pathFields.has(key) || value === undefined) {
        continue;
      }
      body[key] = value;
    }
    return body;
  }
  if (http.body === "") {
    return undefined;
  }
  const camel = snakeToCamel(http.body);
  const value = request[camel] ?? request[http.body];
  return value === undefined ? {} : value;
}

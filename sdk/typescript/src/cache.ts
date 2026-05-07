import { connect } from "node:net";

import { createClient, type Client, type Interceptor } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import { Cache as CacheService } from "./internal/gen/v1/cache_pb.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

/** Base environment variable for discovering cache runtime sockets. */
export const ENV_CACHE_SOCKET = "GESTALT_CACHE_SOCKET";
const CACHE_SOCKET_TOKEN_SUFFIX = "_TOKEN";
const CACHE_RELAY_TOKEN_HEADER = "x-gestalt-host-service-relay-token";
/** Base environment variable for the default cache relay token. */
export const ENV_CACHE_SOCKET_TOKEN =
  `${ENV_CACHE_SOCKET}${CACHE_SOCKET_TOKEN_SUFFIX}`;

/**
 * Single cache entry used by batch cache APIs.
 */
export interface CacheEntry {
  key: string;
  value: Uint8Array;
}

/**
 * Optional TTL applied when setting cache values.
 */
export interface CacheSetOptions {
  ttlMs?: number;
}

/**
 * Runtime hooks required to implement a Gestalt cache provider.
 */
export interface CacheProviderOptions extends ProviderBaseOptions {
  get: (key: string) => MaybePromise<Uint8Array | null | undefined>;
  set: (
    key: string,
    value: Uint8Array,
    options?: CacheSetOptions,
  ) => MaybePromise<void>;
  delete: (key: string) => MaybePromise<boolean>;
  touch: (key: string, ttlMs: number) => MaybePromise<boolean>;
  getMany?: (keys: string[]) => MaybePromise<Record<string, Uint8Array>>;
  setMany?: (
    entries: CacheEntry[],
    options?: CacheSetOptions,
  ) => MaybePromise<void>;
  deleteMany?: (keys: string[]) => MaybePromise<number | bigint>;
}

/**
 * Returns the environment variable name used to discover a cache socket.
 */
export function cacheSocketEnv(name?: string): string {
  const trimmed = name?.trim() ?? "";
  if (!trimmed) {
    return ENV_CACHE_SOCKET;
  }
  return `${ENV_CACHE_SOCKET}_${trimmed.replace(/[^A-Za-z0-9]/gu, "_").toUpperCase()}`;
}

/**
 * Returns the environment variable name used to discover a cache relay token.
 */
export function cacheSocketTokenEnv(name?: string): string {
  return `${cacheSocketEnv(name)}${CACHE_SOCKET_TOKEN_SUFFIX}`;
}

function cacheTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("cache transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(`cache tcp target ${JSON.stringify(rawTarget)} is missing host:port`);
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(`cache tls target ${JSON.stringify(rawTarget)} is missing host:port`);
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(`cache unix target ${JSON.stringify(rawTarget)} is missing a socket path`);
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(`Unsupported cache target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`);
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

/**
 * Client for invoking a host-provided cache over the Gestalt transport.
 *
 * @example
 * ```ts
 * import { Cache } from "@valon-technologies/gestalt";
 *
 * const cache = new Cache();
 * await cache.set("session", new TextEncoder().encode("hello"));
 * ```
 */
export class Cache {
  private readonly client: Client<typeof CacheService>;

  constructor(name?: string) {
    const envName = cacheSocketEnv(name);
    const target = process.env[envName];
    if (!target) {
      throw new Error(`cache: ${envName} is not set`);
    }
    const token = process.env[cacheSocketTokenEnv(name)]?.trim() ?? "";
    const transportOptions = cacheTransportOptions(target);
    const interceptors: Interceptor[] = token
      ? [
          (next) => async (req) => {
            req.header.set(CACHE_RELAY_TOKEN_HEADER, token);
            return await next(req);
          },
        ]
      : [];
    const transport = createGrpcTransport({
      ...transportOptions,
      ...(transportOptions.nodeOptions
        ? {
            nodeOptions: {
              createConnection: () =>
                connect({ path: transportOptions.nodeOptions!.path }),
            },
          }
        : {}),
      interceptors,
    });
    this.client = createClient(CacheService, transport);
  }

  /** Returns a cached value, or `undefined` when the key is missing. */
  async get(key: string): Promise<Uint8Array | undefined> {
    const response = await this.client.get({
      key,
    });
    if (!response.found) {
      return undefined;
    }
    return cloneBytes(response.value);
  }

  /** Returns the subset of requested keys that currently exist. */
  async getMany(keys: string[]): Promise<Record<string, Uint8Array>> {
    const response = await this.client.getMany({
      keys: [...keys],
    });
    return entriesToRecord(response.entries);
  }

  /** Stores a cached value with an optional TTL. */
  async set(
    key: string,
    value: Uint8Array,
    options?: CacheSetOptions,
  ): Promise<void> {
    const ttl = toProtoDuration(options?.ttlMs);
    await this.client.set({
      key,
      value: cloneBytes(value),
      ...(ttl ? { ttl } : {}),
    });
  }

  /** Stores multiple values with an optional shared TTL. */
  async setMany(
    entries: Iterable<CacheEntry>,
    options?: CacheSetOptions,
  ): Promise<void> {
    const ttl = toProtoDuration(options?.ttlMs);
    await this.client.setMany({
      entries: cloneEntries(entries),
      ...(ttl ? { ttl } : {}),
    });
  }

  /** Deletes a cached value and reports whether it existed. */
  async delete(key: string): Promise<boolean> {
    const response = await this.client.delete({
      key,
    });
    return response.deleted;
  }

  /** Deletes several cached values and returns the number removed. */
  async deleteMany(keys: string[]): Promise<number | bigint> {
    const response = await this.client.deleteMany({
      keys: [...keys],
    });
    return toJsInt(response.deleted);
  }

  /** Refreshes the TTL for an existing key. */
  async touch(key: string, ttlMs: number): Promise<boolean> {
    const ttl = toProtoDuration(ttlMs);
    const response = await this.client.touch({
      key,
      ...(ttl ? { ttl } : {}),
    });
    return response.touched;
  }
}

/**
 * Cache provider implementation consumed by the Gestalt runtime.
 */
export class CacheProvider extends ProviderBase {
  readonly kind = "cache" as const;

  private readonly getHandler: CacheProviderOptions["get"];
  private readonly setHandler: CacheProviderOptions["set"];
  private readonly deleteHandler: CacheProviderOptions["delete"];
  private readonly touchHandler: CacheProviderOptions["touch"];
  private readonly getManyHandler: CacheProviderOptions["getMany"];
  private readonly setManyHandler: CacheProviderOptions["setMany"];
  private readonly deleteManyHandler: CacheProviderOptions["deleteMany"];

  constructor(options: CacheProviderOptions) {
    super(options);
    this.getHandler = options.get;
    this.setHandler = options.set;
    this.deleteHandler = options.delete;
    this.touchHandler = options.touch;
    this.getManyHandler = options.getMany;
    this.setManyHandler = options.setMany;
    this.deleteManyHandler = options.deleteMany;
  }

  async get(key: string): Promise<Uint8Array | undefined> {
    const value = await this.getHandler(key);
    if (value == null) {
      return undefined;
    }
    return cloneBytes(value);
  }

  async getMany(keys: string[]): Promise<Record<string, Uint8Array>> {
    if (this.getManyHandler) {
      return cloneRecord(await this.getManyHandler([...keys]));
    }
    const values = createCacheRecord();
    for (const key of keys) {
      const value = await this.get(key);
      if (value !== undefined) {
        values[key] = cloneBytes(value);
      }
    }
    return values;
  }

  async set(
    key: string,
    value: Uint8Array,
    options?: CacheSetOptions,
  ): Promise<void> {
    await this.setHandler(key, cloneBytes(value), cloneSetOptions(options));
  }

  async setMany(
    entries: Iterable<CacheEntry>,
    options?: CacheSetOptions,
  ): Promise<void> {
    if (this.setManyHandler) {
      await this.setManyHandler(cloneEntries(entries), cloneSetOptions(options));
      return;
    }
    for (const entry of entries) {
      await this.set(entry.key, entry.value, options);
    }
  }

  async delete(key: string): Promise<boolean> {
    return await this.deleteHandler(key);
  }

  async deleteMany(keys: string[]): Promise<number | bigint> {
    if (this.deleteManyHandler) {
      return await this.deleteManyHandler([...keys]);
    }
    let deleted = 0;
    const seen = new Set<string>();
    for (const key of keys) {
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      if (await this.delete(key)) {
        deleted += 1;
      }
    }
    return deleted;
  }

  async touch(key: string, ttlMs: number): Promise<boolean> {
    return await this.touchHandler(key, ttlMs);
  }
}

/**
 * Creates a cache provider from standard CRUD handlers.
 */
export function defineCacheProvider(options: CacheProviderOptions): CacheProvider {
  return new CacheProvider(options);
}

/**
 * Runtime type guard for cache providers loaded from user modules.
 */
export function isCacheProvider(value: unknown): value is CacheProvider {
  return (
    value instanceof CacheProvider ||
    (typeof value === "object" &&
      value !== null &&
      "kind" in value &&
      (value as { kind?: unknown }).kind === "cache" &&
      "get" in value &&
      "set" in value &&
      "delete" in value &&
      "touch" in value)
  );
}

function cloneBytes(value: Uint8Array | ArrayBuffer): Uint8Array {
  if (value instanceof Uint8Array) {
    return new Uint8Array(value);
  }
  return new Uint8Array(value);
}

function cloneEntries(entries: Iterable<CacheEntry>): CacheEntry[] {
  return [...entries].map((entry) => ({
    key: entry.key,
    value: cloneBytes(entry.value),
  }));
}

function cloneRecord(entries: Record<string, Uint8Array>): Record<string, Uint8Array> {
  const cloned = createCacheRecord();
  for (const [key, value] of Object.entries(entries)) {
    cloned[key] = cloneBytes(value);
  }
  return cloned;
}

function cloneSetOptions(options?: CacheSetOptions): CacheSetOptions | undefined {
  if (!options || options.ttlMs === undefined) {
    return undefined;
  }
  return {
    ttlMs: options.ttlMs,
  };
}

function entriesToRecord(
  entries: ReadonlyArray<{ key: string; found: boolean; value: Uint8Array }>,
): Record<string, Uint8Array> {
  const values = createCacheRecord();
  for (const entry of entries) {
    if (!entry.found) {
      continue;
    }
    values[entry.key] = cloneBytes(entry.value);
  }
  return values;
}

function createCacheRecord(): Record<string, Uint8Array> {
  return Object.create(null) as Record<string, Uint8Array>;
}

function toProtoDuration(ttlMs: number | undefined): { seconds: bigint; nanos: number } | undefined {
  if (ttlMs === undefined || !Number.isFinite(ttlMs) || ttlMs <= 0) {
    return undefined;
  }
  const wholeMs = Math.trunc(ttlMs);
  const seconds = Math.trunc(wholeMs / 1000);
  const nanos = Math.trunc((wholeMs % 1000) * 1_000_000);
  return {
    seconds: BigInt(seconds),
    nanos,
  };
}

function toJsInt(value: bigint): number | bigint {
  const asNumber = Number(value);
  return Number.isSafeInteger(asNumber) ? asNumber : value;
}

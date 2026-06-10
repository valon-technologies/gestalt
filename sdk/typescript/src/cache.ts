import { CacheClient } from "./cache_client.ts";
import { ProviderBase, type ProviderBaseOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";
import type { DurationMs } from "./rpc_support.ts";

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
 * Fakeable cache contract shared by SDK callers and tests.
 */
export interface Cache {
  get(key: string): Promise<Uint8Array | undefined>;
  getMany(keys: string[]): Promise<Record<string, Uint8Array>>;
  set(
    key: string,
    value: Uint8Array,
    options?: CacheSetOptions,
  ): Promise<void>;
  setMany(
    entries: Iterable<CacheEntry>,
    options?: CacheSetOptions,
  ): Promise<void>;
  delete(key: string): Promise<boolean>;
  deleteMany(keys: string[]): Promise<number | bigint>;
  touch(key: string, ttlMs: number): Promise<boolean>;
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
 * Client for invoking a provider-backed cache over the Gestalt transport.
 *
 * @example
 * ```ts
 * import { Cache } from "@valon-technologies/gestalt";
 *
 * const cache = new Cache();
 * await cache.set("session", new TextEncoder().encode("hello"));
 * ```
 */
class CacheImpl implements Cache {
  private readonly client: CacheClient;

  constructor(name?: string) {
    this.client = CacheClient.connect(name);
  }

  /** Returns a cached value, or `undefined` when the key is missing. */
  async get(key: string): Promise<Uint8Array | undefined> {
    return await this.client.get(key);
  }

  /** Returns the subset of requested keys that currently exist. */
  async getMany(keys: string[]): Promise<Record<string, Uint8Array>> {
    return await this.client.getMany([...keys]);
  }

  /** Stores a cached value with an optional TTL. */
  async set(
    key: string,
    value: Uint8Array,
    options?: CacheSetOptions,
  ): Promise<void> {
    await this.client.set(key, value, normalizeTtl(options?.ttlMs));
  }

  /** Stores multiple values with an optional shared TTL. */
  async setMany(
    entries: Iterable<CacheEntry>,
    options?: CacheSetOptions,
  ): Promise<void> {
    await this.client.setMany([...entries], normalizeTtl(options?.ttlMs));
  }

  /** Deletes a cached value and reports whether it existed. */
  async delete(key: string): Promise<boolean> {
    return await this.client.delete(key);
  }

  /** Deletes several cached values and returns the number removed. */
  async deleteMany(keys: string[]): Promise<number | bigint> {
    return toJsInt(await this.client.deleteMany([...keys]));
  }

  /** Refreshes the TTL for an existing key. */
  async touch(key: string, ttlMs: number): Promise<boolean> {
    return await this.client.touch(key, normalizeTtl(ttlMs));
  }
}

export const Cache = CacheImpl;

/**
 * Cache provider implementation consumed by the Gestalt runtime.
 */
export class CacheProvider extends ProviderBase implements Cache {
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


function createCacheRecord(): Record<string, Uint8Array> {
  return Object.create(null) as Record<string, Uint8Array>;
}


function normalizeTtl(ttlMs: number | undefined): DurationMs | undefined {
  if (ttlMs === undefined || !Number.isFinite(ttlMs) || ttlMs <= 0) {
    return undefined;
  }
  return Math.trunc(ttlMs);
}

function toJsInt(value: bigint): number | bigint {
  const asNumber = Number(value);
  return Number.isSafeInteger(asNumber) ? asNumber : value;
}

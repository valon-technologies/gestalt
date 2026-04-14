import { connect } from "node:net";

import { createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import { Cache as CacheService } from "../gen/v1/cache_pb.ts";
import { RuntimeProvider, type RuntimeProviderOptions } from "./provider.ts";
import type { MaybePromise } from "./api.ts";

const ENV_CACHE_SOCKET = "GESTALT_CACHE_SOCKET";

export interface CacheEntry {
  key: string;
  value: Uint8Array;
}

export interface CacheSetOptions {
  ttlMs?: number;
}

export interface CacheProviderOptions extends RuntimeProviderOptions {
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

export function cacheSocketEnv(name?: string): string {
  const trimmed = name?.trim() ?? "";
  if (!trimmed) {
    return ENV_CACHE_SOCKET;
  }
  return `${ENV_CACHE_SOCKET}_${trimmed.replace(/[^A-Za-z0-9]/g, "_").toUpperCase()}`;
}

export class Cache {
  private readonly client: Client<typeof CacheService>;

  constructor(name?: string) {
    const envName = cacheSocketEnv(name);
    const socketPath = process.env[envName];
    if (!socketPath) {
      throw new Error(`cache: ${envName} is not set`);
    }

    const transport = createGrpcTransport({
      baseUrl: "http://localhost",
      nodeOptions: {
        createConnection: () => connect(socketPath),
      },
    });
    this.client = createClient(CacheService, transport);
  }

  async get(key: string): Promise<Uint8Array | undefined> {
    const response = await this.client.get({
      key,
    });
    if (!response.found) {
      return undefined;
    }
    return cloneBytes(response.value);
  }

  async getMany(keys: string[]): Promise<Record<string, Uint8Array>> {
    const response = await this.client.getMany({
      keys: [...keys],
    });
    return entriesToRecord(response.entries);
  }

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

  async delete(key: string): Promise<boolean> {
    const response = await this.client.delete({
      key,
    });
    return response.deleted;
  }

  async deleteMany(keys: string[]): Promise<number | bigint> {
    const response = await this.client.deleteMany({
      keys: [...keys],
    });
    return toJsInt(response.deleted);
  }

  async touch(key: string, ttlMs: number): Promise<boolean> {
    const ttl = toProtoDuration(ttlMs);
    const response = await this.client.touch({
      key,
      ...(ttl ? { ttl } : {}),
    });
    return response.touched;
  }
}

export class CacheProvider extends RuntimeProvider {
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

export function defineCacheProvider(options: CacheProviderOptions): CacheProvider {
  return new CacheProvider(options);
}

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

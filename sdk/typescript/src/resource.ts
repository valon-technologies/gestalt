import type { MaybePromise } from "./api.ts";

export type DatastoreCapability =
  | "key_value"
  | "sql"
  | "blob_store";

export interface KVEntry {
  key: string;
  value: Uint8Array;
}

export interface KeyValueStore {
  get(key: string): MaybePromise<Uint8Array | null | undefined>;
  put(key: string, value: Uint8Array): MaybePromise<void>;
  delete(key: string): MaybePromise<void>;
  list(prefix?: string): MaybePromise<KVEntry[]>;
}

export interface SQLRows {
  columns: string[];
  rows: unknown[][];
}

export interface SQLExecResult {
  rowsAffected: number | bigint;
  lastInsertId: number | bigint;
}

export interface SQLMigration {
  version: number;
  description: string;
  sql: string;
}

export interface SQLStore {
  query(sql: string, params?: unknown[]): MaybePromise<SQLRows>;
  exec(sql: string, params?: unknown[]): MaybePromise<SQLExecResult>;
  migrate(migrations: SQLMigration[]): MaybePromise<void>;
}

export interface BlobEntry {
  key: string;
  size: number | bigint;
  contentType?: string;
  lastModified?: Date;
}

export interface BlobStore {
  get(key: string): MaybePromise<Uint8Array | null | undefined>;
  put(key: string, value: Uint8Array, contentType?: string): MaybePromise<void>;
  delete(key: string): MaybePromise<void>;
  list(prefix?: string): MaybePromise<BlobEntry[]>;
}

// ---------------------------------------------------------------------------
// Provider interfaces (namespace-aware, implemented by resource providers)
// ---------------------------------------------------------------------------

export interface DatastoreProvider {
  configure(name: string, config: Record<string, unknown>): MaybePromise<void>;
  capabilities(): DatastoreCapability[];
  healthCheck?(): MaybePromise<void>;
  close?(): MaybePromise<void>;
}

export interface KeyValueDatastoreProvider extends DatastoreProvider {
  kvGet(namespace: string, key: string): MaybePromise<Uint8Array | null | undefined>;
  kvPut(namespace: string, key: string, value: Uint8Array, ttlSeconds?: number): MaybePromise<void>;
  kvDelete(namespace: string, key: string): MaybePromise<void>;
  kvList(namespace: string, prefix?: string): MaybePromise<KVEntry[]>;
}

export interface SQLDatastoreProvider extends DatastoreProvider {
  sqlQuery(namespace: string, sql: string, params?: unknown[]): MaybePromise<SQLRows>;
  sqlExec(namespace: string, sql: string, params?: unknown[]): MaybePromise<SQLExecResult>;
  sqlMigrate(namespace: string, migrations: SQLMigration[]): MaybePromise<void>;
}

export interface BlobStoreDatastoreProvider extends DatastoreProvider {
  blobGet(namespace: string, key: string): MaybePromise<{ data: Uint8Array; contentType: string; metadata?: Record<string, string> } | null>;
  blobPut(namespace: string, key: string, value: Uint8Array, contentType?: string, metadata?: Record<string, string>): MaybePromise<void>;
  blobDelete(namespace: string, key: string): MaybePromise<void>;
  blobList(namespace: string, prefix?: string): MaybePromise<BlobEntry[]>;
}

import { createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { IndexedDB as IndexedDBService } from "../gen/v1/datastore_pb";

const ENV_INDEXEDDB_SOCKET = "GESTALT_INDEXEDDB_SOCKET";

export class NotFoundError extends Error {
  constructor(message?: string) {
    super(message ?? "not found");
    this.name = "NotFoundError";
  }
}

export class AlreadyExistsError extends Error {
  constructor(message?: string) {
    super(message ?? "already exists");
    this.name = "AlreadyExistsError";
  }
}

export type Record = { [key: string]: unknown };

export interface KeyRange {
  lower?: unknown;
  upper?: unknown;
  lowerOpen?: boolean;
  upperOpen?: boolean;
}

export interface IndexSchema {
  name: string;
  keyPath: string[];
  unique?: boolean;
}

export interface ObjectStoreSchema {
  indexes?: IndexSchema[];
}

export class IndexedDB {
  private client: Client<typeof IndexedDBService>;

  constructor() {
    const socketPath = process.env[ENV_INDEXEDDB_SOCKET];
    if (!socketPath) {
      throw new Error(`${ENV_INDEXEDDB_SOCKET} is not set`);
    }
    const transport = createGrpcTransport({
      baseUrl: `http://localhost`,
      nodeOptions: { path: socketPath },
    });
    this.client = createClient(IndexedDBService, transport);
  }

  async createObjectStore(name: string, schema?: ObjectStoreSchema): Promise<void> {
    await this.client.createObjectStore({
      name,
      schema: {
        indexes: (schema?.indexes ?? []).map((idx) => ({
          name: idx.name,
          keyPath: idx.keyPath,
          unique: idx.unique ?? false,
        })),
        columns: [],
      },
    });
  }

  async deleteObjectStore(name: string): Promise<void> {
    await this.client.deleteObjectStore({ name });
  }

  objectStore(name: string): ObjectStore {
    return new ObjectStore(this.client, name);
  }
}

export class ObjectStore {
  constructor(
    private client: Client<typeof IndexedDBService>,
    private store: string,
  ) {}

  async get(id: string): Promise<Record> {
    const resp = await rpc(() => this.client.get({ store: this.store, id }));
    return (resp.record ?? {}) as Record;
  }

  async getKey(id: string): Promise<string> {
    const resp = await rpc(() => this.client.getKey({ store: this.store, id }));
    return resp.key;
  }

  async add(record: Record): Promise<void> {
    await rpc(() => this.client.add({ store: this.store, record: record as any }));
  }

  async put(record: Record): Promise<void> {
    await rpc(() => this.client.put({ store: this.store, record: record as any }));
  }

  async delete(id: string): Promise<void> {
    await rpc(() => this.client.delete({ store: this.store, id }));
  }

  async clear(): Promise<void> {
    await this.client.clear({ store: this.store });
  }

  async getAll(keyRange?: KeyRange): Promise<Record[]> {
    const resp = await this.client.getAll({
      store: this.store,
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.records.map((r) => r as unknown as Record);
  }

  async getAllKeys(keyRange?: KeyRange): Promise<string[]> {
    const resp = await this.client.getAllKeys({
      store: this.store,
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.keys;
  }

  async count(keyRange?: KeyRange): Promise<number> {
    const resp = await this.client.count({
      store: this.store,
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return Number(resp.count);
  }

  async deleteRange(keyRange: KeyRange): Promise<number> {
    const resp = await this.client.deleteRange({
      store: this.store,
      range: toProtoKeyRange(keyRange),
    });
    return Number(resp.deleted);
  }

  index(name: string): Index {
    return new Index(this.client, this.store, name);
  }
}

export class Index {
  constructor(
    private client: Client<typeof IndexedDBService>,
    private store: string,
    private indexName: string,
  ) {}

  async get(...values: unknown[]): Promise<Record> {
    const resp = await rpc(() =>
      this.client.indexGet({
        store: this.store,
        index: this.indexName,
        values: values.map(toProtoValue),
      }),
    );
    return (resp.record ?? {}) as Record;
  }

  async getKey(...values: unknown[]): Promise<string> {
    const resp = await rpc(() =>
      this.client.indexGetKey({
        store: this.store,
        index: this.indexName,
        values: values.map(toProtoValue),
      }),
    );
    return resp.key;
  }

  async getAll(keyRange?: KeyRange, ...values: unknown[]): Promise<Record[]> {
    const resp = await this.client.indexGetAll({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoValue),
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.records.map((r) => r as unknown as Record);
  }

  async getAllKeys(keyRange?: KeyRange, ...values: unknown[]): Promise<string[]> {
    const resp = await this.client.indexGetAllKeys({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoValue),
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.keys;
  }

  async count(keyRange?: KeyRange, ...values: unknown[]): Promise<number> {
    const resp = await this.client.indexCount({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoValue),
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return Number(resp.count);
  }

  async delete(...values: unknown[]): Promise<number> {
    const resp = await this.client.indexDelete({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoValue),
    });
    return Number(resp.deleted);
  }
}

function toProtoValue(v: unknown): any {
  if (v === null || v === undefined) return { kind: { case: "nullValue", value: 0 } };
  if (typeof v === "boolean") return { kind: { case: "boolValue", value: v } };
  if (typeof v === "number") return { kind: { case: "numberValue", value: v } };
  if (typeof v === "string") return { kind: { case: "stringValue", value: v } };
  return { kind: { case: "stringValue", value: String(v) } };
}

function toProtoKeyRange(kr: KeyRange): any {
  return {
    lower: kr.lower !== undefined ? toProtoValue(kr.lower) : undefined,
    upper: kr.upper !== undefined ? toProtoValue(kr.upper) : undefined,
    lowerOpen: kr.lowerOpen ?? false,
    upperOpen: kr.upperOpen ?? false,
  };
}

async function rpc<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (err: any) {
    if (err?.code === 5) throw new NotFoundError(err.message);
    if (err?.code === 6) throw new AlreadyExistsError(err.message);
    throw err;
  }
}

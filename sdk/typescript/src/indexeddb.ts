import { createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import {
  IndexedDB as IndexedDBService,
  CursorDirection as ProtoCursorDirection,
} from "../gen/v1/datastore_pb";

const ENV_INDEXEDDB_SOCKET = "GESTALT_INDEXEDDB_SOCKET";

/**
 * Returns the environment variable name used to discover an IndexedDB socket.
 */
export function indexedDBSocketEnv(name?: string): string {
  const trimmed = name?.trim() ?? "";
  if (!trimmed) return ENV_INDEXEDDB_SOCKET;
  return `${ENV_INDEXEDDB_SOCKET}_${trimmed.replace(/[^A-Za-z0-9]/g, "_").toUpperCase()}`;
}

function indexedDBTransportOptions(rawTarget: string): {
  baseUrl: string;
  nodeOptions?: { path: string };
} {
  const target = rawTarget.trim();
  if (!target) {
    throw new Error("IndexedDB transport target is required");
  }
  if (target.startsWith("tcp://")) {
    const address = target.slice("tcp://".length).trim();
    if (!address) {
      throw new Error(`IndexedDB tcp target ${JSON.stringify(rawTarget)} is missing host:port`);
    }
    return { baseUrl: `http://${address}` };
  }
  if (target.startsWith("tls://")) {
    const address = target.slice("tls://".length).trim();
    if (!address) {
      throw new Error(`IndexedDB tls target ${JSON.stringify(rawTarget)} is missing host:port`);
    }
    return { baseUrl: `https://${address}` };
  }
  if (target.startsWith("unix://")) {
    const socketPath = target.slice("unix://".length).trim();
    if (!socketPath) {
      throw new Error(`IndexedDB unix target ${JSON.stringify(rawTarget)} is missing a socket path`);
    }
    return { baseUrl: "http://localhost", nodeOptions: { path: socketPath } };
  }
  if (target.includes("://")) {
    const parsed = new URL(target);
    throw new Error(`Unsupported IndexedDB target scheme ${JSON.stringify(parsed.protocol.replace(/:$/, ""))}`);
  }
  return { baseUrl: "http://localhost", nodeOptions: { path: target } };
}

class AsyncQueue<T> implements AsyncIterable<T> {
  private queue: T[] = [];
  private waiting: ((result: IteratorResult<T>) => void) | null = null;
  private closed = false;

  push(value: T) {
    if (this.waiting) {
      const resolve = this.waiting;
      this.waiting = null;
      resolve({ value, done: false });
    } else {
      this.queue.push(value);
    }
  }

  end() {
    this.closed = true;
    if (this.waiting) {
      const resolve = this.waiting;
      this.waiting = null;
      resolve({ value: undefined as any, done: true });
    }
  }

  [Symbol.asyncIterator]() {
    return this;
  }

  async next(): Promise<IteratorResult<T>> {
    if (this.queue.length > 0) {
      return { value: this.queue.shift()!, done: false };
    }
    if (this.closed) {
      return { value: undefined as any, done: true };
    }
    return new Promise((resolve) => {
      this.waiting = resolve;
    });
  }

  async throw(err: unknown): Promise<IteratorResult<T>> {
    this.closed = true;
    if (this.waiting) {
      const resolve = this.waiting;
      this.waiting = null;
      resolve({ value: undefined as any, done: true });
    }
    return { value: undefined as any, done: true };
  }
}

/**
 * Cursor iteration direction.
 */
export enum CursorDirection {
  Next = 0,
  NextUnique = 1,
  Prev = 2,
  PrevUnique = 3,
}

const CURSOR_DIRECTION_TO_PROTO: { [K in CursorDirection]: ProtoCursorDirection } = {
  [CursorDirection.Next]: ProtoCursorDirection.CURSOR_NEXT,
  [CursorDirection.NextUnique]: ProtoCursorDirection.CURSOR_NEXT_UNIQUE,
  [CursorDirection.Prev]: ProtoCursorDirection.CURSOR_PREV,
  [CursorDirection.PrevUnique]: ProtoCursorDirection.CURSOR_PREV_UNIQUE,
};

/**
 * Options for opening a cursor over an object store or index.
 */
export interface OpenCursorOptions {
  range?: KeyRange;
  direction?: CursorDirection;
}

/**
 * Streaming cursor over an object store or secondary index.
 */
export class Cursor {
  private sendQueue: AsyncQueue<any>;
  private responseIterator: AsyncIterator<any>;
  private _key: unknown = undefined;
  private _primaryKey: string = "";
  private _value: Record | undefined = undefined;
  private _done = false;

  private _indexCursor = false;

  private constructor(
    sendQueue: AsyncQueue<any>,
    responseIterator: AsyncIterator<any>,
    indexCursor: boolean = false,
  ) {
    this.sendQueue = sendQueue;
    this.responseIterator = responseIterator;
    this._indexCursor = indexCursor;
  }

  /**
   * Opens a low-level cursor stream for object-store and index helpers.
   *
   * @internal
   */
  static async open(
    client: Client<typeof IndexedDBService>,
    store: string,
    options?: OpenCursorOptions & { keysOnly?: boolean; index?: string; indexValues?: unknown[] },
  ): Promise<Cursor | null> {
    const sendQueue = new AsyncQueue<any>();
    const direction = options?.direction ?? CursorDirection.Next;

    sendQueue.push({
      msg: {
        case: "open" as const,
        value: {
          store,
          range: options?.range ? toProtoKeyRange(options.range) : undefined,
          direction: CURSOR_DIRECTION_TO_PROTO[direction],
          keysOnly: options?.keysOnly ?? false,
          index: options?.index ?? "",
          values: (options?.indexValues ?? []).map(toProtoTypedValue),
        },
      },
    });

    const responses = client.openCursor(sendQueue);
    const responseIterator = responses[Symbol.asyncIterator]();

    const isIndex = !!(options?.index);
    const cursor = new Cursor(sendQueue, responseIterator, isIndex);
    // Read the open ack to surface creation errors synchronously.
    await cursor.recvOpenAck();
    return cursor;
  }

  /**
   * Current cursor key.
   */
  get key(): unknown {
    return this._key;
  }

  /**
   * Current primary key for the pointed record.
   */
  get primaryKey(): string {
    return this._primaryKey;
  }

  /**
   * Current record value.
   */
  get value(): Record | undefined {
    return this._value;
  }

  /**
   * Whether the cursor has reached the end of the stream.
   */
  get done(): boolean {
    return this._done;
  }

  /**
   * Advances the cursor to the next entry.
   */
  async continue(): Promise<boolean> {
    this.sendQueue.push({
      msg: { case: "command" as const, value: { command: { case: "next" as const, value: true } } },
    });
    return this.pull();
  }

  /**
   * Advances the cursor to a specific key.
   */
  async continueToKey(key: unknown): Promise<boolean> {
    this.sendQueue.push({
      msg: {
        case: "command" as const,
        value: {
          command: {
            case: "continueToKey" as const,
            value: { key: toProtoCursorKey(key, this._indexCursor) },
          },
        },
      },
    });
    return this.pull();
  }

  /**
   * Advances the cursor by `count` entries.
   */
  async advance(count: number): Promise<boolean> {
    this.sendQueue.push({
      msg: {
        case: "command" as const,
        value: { command: { case: "advance" as const, value: count } },
      },
    });
    return this.pull();
  }

  /**
   * Deletes the current entry.
   */
  async delete(): Promise<void> {
    if (this._done) throw new NotFoundError("cursor is exhausted");
    this.sendQueue.push({
      msg: {
        case: "command" as const,
        value: { command: { case: "delete" as const, value: true } },
      },
    });
    await this.recvMutationAck();
  }

  /**
   * Updates the current entry with a replacement record.
   */
  async update(record: Record): Promise<void> {
    if (this._done) throw new NotFoundError("cursor is exhausted");
    this.sendQueue.push({
      msg: {
        case: "command" as const,
        value: { command: { case: "update" as const, value: toProtoRecord(record) } },
      },
    });
    await this.recvMutationAck();
  }

  /**
   * Closes the cursor stream.
   */
  close(): void {
    this.sendQueue.push({
      msg: {
        case: "command" as const,
        value: { command: { case: "close" as const, value: true } },
      },
    });
    this.sendQueue.end();
    this._done = true;
    this._key = undefined;
    this._primaryKey = "";
    this._value = undefined;
  }

  private resetState(): void {
    this._done = true;
    this._key = undefined;
    this._primaryKey = "";
    this._value = undefined;
  }

  private mapCursorError(err: any): never {
    if (err?.code === 5) throw new NotFoundError(err.message);
    if (err?.code === 6) throw new AlreadyExistsError(err.message);
    throw err;
  }

  private async recvOpenAck(): Promise<void> {
    try {
      const { value: resp, done } = await this.responseIterator.next();
      if (done || !resp) {
        this.sendQueue.end();
        this.resetState();
        throw new Error("cursor stream ended during open");
      }
      if (resp.result?.case !== "done" || resp.result.value !== false) {
        this.sendQueue.end();
        this.resetState();
        throw new Error("unexpected cursor open ack");
      }
    } catch (err: any) {
      this.mapCursorError(err);
    }
  }

  private async recvMutationAck(): Promise<void> {
    try {
      const { value: resp, done } = await this.responseIterator.next();
      if (done || !resp) {
        this.sendQueue.end();
        this.resetState();
        throw new Error("cursor stream ended during mutation");
      }
      if (resp.result?.case === "entry") {
        this.refreshFromEntry(resp.result.value);
        return;
      }
      if (resp.result?.case === "done") return;
      throw new Error("unexpected cursor mutation ack");
    } catch (err: any) {
      this.mapCursorError(err);
    }
  }

  private async pull(): Promise<boolean> {
    let resp: any;
    let done: boolean | undefined;
    try {
      ({ value: resp, done } = await this.responseIterator.next());
    } catch (err: any) {
      this.mapCursorError(err);
    }
    if (done || !resp) {
      this.resetState();
      return false;
    }
    if (resp.result?.case === "done" && resp.result.value === true) {
      this.resetState();
      return false;
    }
    if (resp.result?.case === "done") {
      // done=false is an ack (e.g. open ack), not exhaustion.
      return false;
    }
    if (resp.result?.case === "entry") {
      this.refreshFromEntry(resp.result.value);
      this._done = false;
      return true;
    }
    return false;
  }

  private refreshFromEntry(entry: any): void {
    if (!this._indexCursor && entry.key.length === 1) {
      this._key = fromProtoKeyValue(entry.key[0]);
    } else if (entry.key.length > 0) {
      this._key = entry.key.map(fromProtoKeyValue);
    } else {
      this._key = undefined;
    }
    this._primaryKey = entry.primaryKey;
    this._value = entry.record ? fromProtoRecord(entry.record) : undefined;
  }
}

/**
 * Error returned when an IndexedDB record cannot be found.
 */
export class NotFoundError extends Error {
  constructor(message?: string) {
    super(message ?? "not found");
    this.name = "NotFoundError";
  }
}

/**
 * Error returned when a write conflicts with an existing unique value.
 */
export class AlreadyExistsError extends Error {
  constructor(message?: string) {
    super(message ?? "already exists");
    this.name = "AlreadyExistsError";
  }
}

/**
 * Plain object record stored in IndexedDB.
 */
export type Record = { [key: string]: unknown };

/**
 * Key range used to filter object store and index operations.
 */
export interface KeyRange {
  lower?: unknown;
  upper?: unknown;
  lowerOpen?: boolean;
  upperOpen?: boolean;
}

/**
 * Secondary index definition for an object store.
 */
export interface IndexSchema {
  name: string;
  keyPath: string[];
  unique?: boolean;
}

/**
 * Column type metadata used by the datastore schema.
 */
export enum ColumnType {
  String = 0,
  Int = 1,
  Float = 2,
  Bool = 3,
  Time = 4,
  Bytes = 5,
  JSON = 6,
}

/**
 * Column definition for an object store schema.
 */
export interface ColumnSchema {
  name: string;
  type?: ColumnType;
  primaryKey?: boolean;
  notNull?: boolean;
  unique?: boolean;
}

/**
 * Object store schema used during store creation.
 */
export interface ObjectStoreSchema {
  indexes?: IndexSchema[];
  columns?: ColumnSchema[];
}

/**
 * Client for invoking a host-provided IndexedDB service over the Gestalt transport.
 *
 * @example
 * ```ts
 * import { IndexedDB } from "@valon-technologies/gestalt";
 *
 * const db = new IndexedDB();
 * const todos = db.objectStore("todos");
 * ```
 */
export class IndexedDB {
  private client: Client<typeof IndexedDBService>;

 constructor(name?: string) {
    const envName = indexedDBSocketEnv(name);
    const target = process.env[envName];
    if (!target) {
      throw new Error(`${envName} is not set`);
    }
    const transport = createGrpcTransport(indexedDBTransportOptions(target));
    this.client = createClient(IndexedDBService, transport);
  }

  /**
   * Creates an object store.
   */
  async createObjectStore(name: string, schema?: ObjectStoreSchema): Promise<void> {
    await this.client.createObjectStore({
      name,
      schema: {
        indexes: (schema?.indexes ?? []).map((idx) => ({
          name: idx.name,
          keyPath: idx.keyPath,
          unique: idx.unique ?? false,
        })),
        columns: (schema?.columns ?? []).map((col) => ({
          name: col.name,
          type: col.type ?? ColumnType.String,
          primaryKey: col.primaryKey ?? false,
          notNull: col.notNull ?? false,
          unique: col.unique ?? false,
        })),
      },
    });
  }

  /**
   * Deletes an object store.
   */
  async deleteObjectStore(name: string): Promise<void> {
    await this.client.deleteObjectStore({ name });
  }

  /**
   * Returns a client bound to a single object store.
   */
  objectStore(name: string): ObjectStore {
    return new ObjectStore(this.client, name);
  }
}

/**
 * Object store client used for primary-key operations.
 */
export class ObjectStore {
  /**
   * @internal
   */
  constructor(
    private client: Client<typeof IndexedDBService>,
    private store: string,
  ) {}

  /**
   * Reads a record by primary key.
   */
  async get(id: string): Promise<Record> {
    const resp = await rpc(() => this.client.get({ store: this.store, id }));
    return fromProtoRecord(resp.record);
  }

  /**
   * Reads the generated primary key for a record.
   */
  async getKey(id: string): Promise<string> {
    const resp = await rpc(() => this.client.getKey({ store: this.store, id }));
    return resp.key;
  }

  /**
   * Inserts a new record and fails if it already exists.
   */
  async add(record: Record): Promise<void> {
    await rpc(() => this.client.add({ store: this.store, record: toProtoRecord(record) }));
  }

  /**
   * Inserts or replaces a record.
   */
  async put(record: Record): Promise<void> {
    await rpc(() => this.client.put({ store: this.store, record: toProtoRecord(record) }));
  }

  /**
   * Deletes a record by primary key.
   */
  async delete(id: string): Promise<void> {
    await rpc(() => this.client.delete({ store: this.store, id }));
  }

  /**
   * Removes all records from the object store.
   */
  async clear(): Promise<void> {
    await this.client.clear({ store: this.store });
  }

  /**
   * Reads all records in the object store or within a key range.
   */
  async getAll(keyRange?: KeyRange): Promise<Record[]> {
    const resp = await this.client.getAll({
      store: this.store,
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.records.map((r) => fromProtoRecord(r));
  }

  /**
   * Reads all primary keys in the object store or within a key range.
   */
  async getAllKeys(keyRange?: KeyRange): Promise<string[]> {
    const resp = await this.client.getAllKeys({
      store: this.store,
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.keys;
  }

  /**
   * Counts records in the object store or within a key range.
   */
  async count(keyRange?: KeyRange): Promise<number> {
    const resp = await this.client.count({
      store: this.store,
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return Number(resp.count);
  }

  /**
   * Deletes records within a key range.
   */
  async deleteRange(keyRange: KeyRange): Promise<number> {
    const resp = await this.client.deleteRange({
      store: this.store,
      range: toProtoKeyRange(keyRange),
    });
    return Number(resp.deleted);
  }

  /**
   * Opens a cursor over the object store.
   */
  async openCursor(options?: OpenCursorOptions): Promise<Cursor | null> {
    return Cursor.open(this.client, this.store, options);
  }

  /**
   * Opens a key-only cursor over the object store.
   */
  async openKeyCursor(options?: OpenCursorOptions): Promise<Cursor | null> {
    return Cursor.open(this.client, this.store, { ...options, keysOnly: true });
  }

  /**
   * Returns a client bound to a secondary index.
   */
  index(name: string): Index {
    return new Index(this.client, this.store, name);
  }
}

/**
 * Secondary-index client used for lookup and cursor operations.
 */
export class Index {
  /**
   * @internal
   */
  constructor(
    private client: Client<typeof IndexedDBService>,
    private store: string,
    private indexName: string,
  ) {}

  /**
   * Reads the first record matching the supplied index values.
   */
  async get(...values: unknown[]): Promise<Record> {
    const resp = await rpc(() =>
      this.client.indexGet({
        store: this.store,
        index: this.indexName,
        values: values.map(toProtoTypedValue),
      }),
    );
    return fromProtoRecord(resp.record);
  }

  /**
   * Reads the primary key for the first matching index entry.
   */
  async getKey(...values: unknown[]): Promise<string> {
    const resp = await rpc(() =>
      this.client.indexGetKey({
        store: this.store,
        index: this.indexName,
        values: values.map(toProtoTypedValue),
      }),
    );
    return resp.key;
  }

  /**
   * Reads all records matching the supplied index values and optional range.
   */
  async getAll(keyRange?: KeyRange, ...values: unknown[]): Promise<Record[]> {
    const resp = await this.client.indexGetAll({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoTypedValue),
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.records.map((r) => fromProtoRecord(r));
  }

  /**
   * Reads all primary keys matching the supplied index values and optional range.
   */
  async getAllKeys(keyRange?: KeyRange, ...values: unknown[]): Promise<string[]> {
    const resp = await this.client.indexGetAllKeys({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoTypedValue),
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return resp.keys;
  }

  /**
   * Counts records matching the supplied index values and optional range.
   */
  async count(keyRange?: KeyRange, ...values: unknown[]): Promise<number> {
    const resp = await this.client.indexCount({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoTypedValue),
      range: keyRange ? toProtoKeyRange(keyRange) : undefined,
    });
    return Number(resp.count);
  }

  /**
   * Deletes records matching the supplied index values.
   */
  async delete(...values: unknown[]): Promise<number> {
    const resp = await this.client.indexDelete({
      store: this.store,
      index: this.indexName,
      values: values.map(toProtoTypedValue),
    });
    return Number(resp.deleted);
  }

  /**
   * Opens a cursor over the index.
   */
  async openCursor(options?: OpenCursorOptions, ...values: unknown[]): Promise<Cursor | null> {
    return Cursor.open(this.client, this.store, {
      ...options,
      index: this.indexName,
      indexValues: values,
    });
  }

  /**
   * Opens a key-only cursor over the index.
   */
  async openKeyCursor(options?: OpenCursorOptions, ...values: unknown[]): Promise<Cursor | null> {
    return Cursor.open(this.client, this.store, {
      ...options,
      keysOnly: true,
      index: this.indexName,
      indexValues: values,
    });
  }
}

function fromProtoKeyValue(kv: any): unknown {
  if (kv.kind?.case === "scalar") return fromProtoTypedValue(kv.kind.value);
  if (kv.kind?.case === "array") return kv.kind.value.elements.map(fromProtoKeyValue);
  return undefined;
}

function toProtoKeyValue(v: unknown): any {
  if (Array.isArray(v)) {
    return { kind: { case: "array" as const, value: { elements: v.map(toProtoKeyValue) } } };
  }
  return { kind: { case: "scalar" as const, value: toProtoTypedValue(v) } };
}

function toProtoCursorKey(key: unknown, indexCursor: boolean): any[] {
  if (indexCursor && Array.isArray(key)) {
    return key.map(toProtoKeyValue);
  }
  return [toProtoKeyValue(key)];
}

function toProtoRecord(record: Record): any {
  const fields: { [key: string]: unknown } = {};
  for (const [key, value] of Object.entries(record)) {
    fields[key] = toProtoTypedValue(value);
  }
  return { fields };
}

function fromProtoRecord(record: any): Record {
  const fields = record?.fields ?? {};
  const out: Record = {};
  for (const [key, value] of Object.entries(fields)) {
    out[key] = fromProtoTypedValue(value);
  }
  return out;
}

function toProtoTypedValue(v: unknown): any {
  if (v === null || v === undefined) return { kind: { case: "nullValue", value: 0 } };
  if (typeof v === "boolean") return { kind: { case: "boolValue", value: v } };
  if (typeof v === "bigint") return { kind: { case: "intValue", value: v } };
  if (typeof v === "number") {
    if (Number.isInteger(v) && Number.isSafeInteger(v)) {
      return { kind: { case: "intValue", value: BigInt(v) } };
    }
    return { kind: { case: "floatValue", value: v } };
  }
  if (typeof v === "string") return { kind: { case: "stringValue", value: v } };
  if (v instanceof Date) return { kind: { case: "timeValue", value: toProtoTimestamp(v) } };
  if (v instanceof Uint8Array) return { kind: { case: "bytesValue", value: v } };
  if (v instanceof ArrayBuffer) return { kind: { case: "bytesValue", value: new Uint8Array(v) } };
  return { kind: { case: "jsonValue", value: toProtoJsonValue(v) } };
}

function fromProtoTypedValue(v: any): unknown {
  switch (v?.kind?.case) {
    case undefined:
    case "nullValue":
      return null;
    case "stringValue":
      return v.kind.value;
    case "intValue":
      return toJsInt(v.kind.value);
    case "floatValue":
      return v.kind.value;
    case "boolValue":
      return v.kind.value;
    case "timeValue":
      return fromProtoTimestamp(v.kind.value);
    case "bytesValue":
      return new Uint8Array(v.kind.value);
    case "jsonValue":
      return fromProtoJsonValue(v.kind.value);
    default:
      throw new Error(`unsupported typed value kind: ${String(v?.kind?.case)}`);
  }
}

function toProtoKeyRange(kr: KeyRange): any {
  return {
    lower: kr.lower !== undefined ? toProtoTypedValue(kr.lower) : undefined,
    upper: kr.upper !== undefined ? toProtoTypedValue(kr.upper) : undefined,
    lowerOpen: kr.lowerOpen ?? false,
    upperOpen: kr.upperOpen ?? false,
  };
}

function toProtoTimestamp(value: Date): any {
  const millis = value.getTime();
  const seconds = Math.trunc(millis / 1000);
  const nanos = Math.trunc((millis % 1000) * 1_000_000);
  return { seconds: BigInt(seconds), nanos };
}

function fromProtoTimestamp(value: any): Date {
  const seconds = Number(value?.seconds ?? 0n);
  const nanos = Number(value?.nanos ?? 0);
  return new Date((seconds * 1000) + Math.trunc(nanos / 1_000_000));
}

function toJsInt(value: bigint): number | bigint {
  const asNumber = Number(value);
  return Number.isSafeInteger(asNumber) ? asNumber : value;
}

function toProtoJsonValue(value: unknown): any {
  if (value === null || value === undefined) return { kind: { case: "nullValue", value: 0 } };
  if (typeof value === "boolean") return { kind: { case: "boolValue", value } };
  if (typeof value === "number") return { kind: { case: "numberValue", value } };
  if (typeof value === "string") return { kind: { case: "stringValue", value } };
  if (value instanceof Date || value instanceof Uint8Array || value instanceof ArrayBuffer) {
    throw new Error(`unsupported JSON value type: ${value.constructor.name}`);
  }
  if (Array.isArray(value)) {
    return {
      kind: {
        case: "listValue",
        value: { values: value.map((item) => toProtoJsonValue(item)) },
      },
    };
  }
  if (typeof value === "object") {
    const fields: { [key: string]: unknown } = {};
    for (const [key, inner] of Object.entries(value as { [key: string]: unknown })) {
      fields[key] = toProtoJsonValue(inner);
    }
    return {
      kind: {
        case: "structValue",
        value: { fields },
      },
    };
  }
  throw new Error(`unsupported JSON value type: ${typeof value}`);
}

function fromProtoJsonValue(value: any): unknown {
  switch (value?.kind?.case) {
    case undefined:
    case "nullValue":
      return null;
    case "numberValue":
    case "stringValue":
    case "boolValue":
      return value.kind.value;
    case "listValue":
      return (value.kind.value?.values ?? []).map((item: unknown) => fromProtoJsonValue(item));
    case "structValue": {
      const out: Record = {};
      for (const [key, inner] of Object.entries(value.kind.value?.fields ?? {})) {
        out[key] = fromProtoJsonValue(inner);
      }
      return out;
    }
    default:
      throw new Error(`unsupported JSON value kind: ${String(value?.kind?.case)}`);
  }
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

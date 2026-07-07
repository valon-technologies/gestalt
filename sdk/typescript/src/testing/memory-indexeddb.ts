import {
  AlreadyExistsError,
  type AcquireLockResult,
  type Cursor,
  type GetAllOptions,
  type Index,
  type IndexedDB,
  type Key,
  type KeyRange,
  NotFoundError,
  type ObjectStore,
  type ObjectStoreSchema,
  type OpenCursorOptions,
  type Record as DBRecord,
  type Transaction,
  type TransactionMode,
  type TransactionOptions,
} from "../providers/indexeddb.ts";

interface StoreState {
  pk: string;
  indexes: Set<string>;
  rows: Map<string, DBRecord>;
}

export interface StoreDump {
  pk: string;
  indexes: string[];
  rows: DBRecord[];
}

export interface MemoryDumpOptions {
  exclude?: string[];
}

function unsupported(feature: string): never {
  throw new Error(`MemoryIndexedDB does not model ${feature}`);
}

function primaryKeyFor(schema?: ObjectStoreSchema): string {
  return schema?.columns?.find((column) => column.primaryKey)?.name ?? "id";
}

export class MemoryIndexedDB implements IndexedDB {
  private readonly stores = new Map<string, StoreState>();
  private lock: { holder: string; expiresAt: number } | null = null;
  private lockCounter = 0n;

  close(): void {}

  async createObjectStore(
    name: string,
    schema?: ObjectStoreSchema,
  ): Promise<ObjectStore> {
    if (this.stores.has(name)) {
      throw new AlreadyExistsError(`store ${name} already exists`);
    }
    const indexes = new Set<string>(
      (schema?.indexes ?? []).map((index) => index.name),
    );
    this.stores.set(name, { pk: primaryKeyFor(schema), indexes, rows: new Map() });
    return this.objectStore(name);
  }

  async deleteObjectStore(name: string): Promise<void> {
    if (!this.stores.delete(name)) {
      throw new NotFoundError(`store ${name} not found`);
    }
  }

  async createIndex(store: string, index: { name: string }): Promise<void> {
    const state = this.state(store);
    if (state.indexes.has(index.name)) {
      throw new AlreadyExistsError(`index ${store}/${index.name} already exists`);
    }
    state.indexes.add(index.name);
  }

  async deleteIndex(store: string, name: string): Promise<void> {
    const state = this.state(store);
    if (!state.indexes.delete(name)) {
      throw new NotFoundError(`index ${store}/${name} not found`);
    }
  }

  objectStore(name: string): ObjectStore {
    return new MemoryObjectStore(() => this.state(name));
  }

  transaction(
    _stores: string[],
    _mode?: TransactionMode,
    _options?: TransactionOptions,
  ): Promise<Transaction> {
    return unsupported("explicit transactions");
  }

  async acquireLock(
    _key: string,
    holder: string,
    ttlMs: number,
  ): Promise<AcquireLockResult> {
    const now = Date.now();
    if (this.lock && this.lock.holder !== holder && this.lock.expiresAt > now) {
      return {
        acquired: false,
        holder: this.lock.holder,
        expiresAt: new Date(this.lock.expiresAt),
        fencingToken: this.lockCounter,
      };
    }
    this.lock = { holder, expiresAt: now + ttlMs };
    this.lockCounter += 1n;
    return {
      acquired: true,
      holder,
      expiresAt: new Date(this.lock.expiresAt),
      fencingToken: this.lockCounter,
    };
  }

  async releaseLock(_key: string, holder: string): Promise<void> {
    if (this.lock?.holder === holder) {
      this.lock = null;
    }
  }

  clone(): MemoryIndexedDB {
    const copy = new MemoryIndexedDB();
    for (const [name, state] of this.stores) {
      copy.stores.set(name, {
        pk: state.pk,
        indexes: new Set(state.indexes),
        rows: new Map(
          [...state.rows].map(([key, row]) => [key, structuredClone(row)]),
        ),
      });
    }
    return copy;
  }

  dump(options?: MemoryDumpOptions): globalThis.Record<string, StoreDump> {
    const exclude = new Set(options?.exclude ?? []);
    const result: globalThis.Record<string, StoreDump> = {};
    for (const name of [...this.stores.keys()].sort()) {
      if (exclude.has(name)) {
        continue;
      }
      const state = this.stores.get(name)!;
      result[name] = {
        pk: state.pk,
        indexes: [...state.indexes].sort(),
        rows: [...state.rows.keys()]
          .sort()
          .map((key) => state.rows.get(key)!),
      };
    }
    return result;
  }

  private state(name: string): StoreState {
    const found = this.stores.get(name);
    if (!found) {
      throw new NotFoundError(`store ${name} not found`);
    }
    return found;
  }
}

class MemoryObjectStore implements ObjectStore {
  constructor(private readonly state: () => StoreState) {}

  async get(id: string): Promise<DBRecord> {
    const row = this.state().rows.get(id);
    if (row === undefined) {
      throw new NotFoundError(`record ${id} not found`);
    }
    return row;
  }

  async getKey(id: string): Promise<string> {
    if (!this.state().rows.has(id)) {
      throw new NotFoundError(`record ${id} not found`);
    }
    return id;
  }

  async add(record: DBRecord): Promise<void> {
    const state = this.state();
    const key = String(record[state.pk]);
    if (state.rows.has(key)) {
      throw new AlreadyExistsError(`record ${key} already exists`);
    }
    state.rows.set(key, record);
  }

  async put(record: DBRecord): Promise<void> {
    const state = this.state();
    state.rows.set(String(record[state.pk]), record);
  }

  async delete(id: string): Promise<void> {
    this.state().rows.delete(id);
  }

  async clear(): Promise<void> {
    this.state().rows.clear();
  }

  async getAll(query?: Key | KeyRange): Promise<DBRecord[]> {
    if (query !== undefined) {
      unsupported("queried getAll");
    }
    return [...this.state().rows.values()];
  }

  async getAllKeys(query?: Key | KeyRange): Promise<string[]> {
    if (query !== undefined) {
      unsupported("queried getAllKeys");
    }
    return [...this.state().rows.keys()];
  }

  async count(query?: Key | KeyRange): Promise<number> {
    if (query !== undefined) {
      unsupported("queried count");
    }
    return this.state().rows.size;
  }

  deleteRange(_query: Key | KeyRange): Promise<number> {
    return unsupported("deleteRange");
  }

  async openCursor(options?: OpenCursorOptions): Promise<Cursor | null> {
    if (options?.query !== undefined || options?.direction !== undefined) {
      unsupported("queried or directional cursors");
    }
    return new MemoryCursor(this.state(), false);
  }

  async openKeyCursor(options?: OpenCursorOptions): Promise<Cursor | null> {
    if (options?.query !== undefined || options?.direction !== undefined) {
      unsupported("queried or directional cursors");
    }
    return new MemoryCursor(this.state(), true);
  }

  index(_name: string): Index {
    return unsupported("secondary indexes");
  }
}

class MemoryCursor implements Cursor {
  private readonly entries: [string, DBRecord][];
  private i = -1;
  key: Key | undefined = undefined;
  primaryKey = "";
  value: DBRecord | undefined = undefined;
  done = false;

  constructor(
    private readonly store: StoreState,
    private readonly keysOnly: boolean,
  ) {
    this.entries = [...store.rows.entries()];
  }

  async continue(): Promise<boolean> {
    this.i += 1;
    const entry = this.entries[this.i];
    if (entry === undefined) {
      this.done = true;
      this.key = undefined;
      this.primaryKey = "";
      this.value = undefined;
      return false;
    }
    this.key = entry[0];
    this.primaryKey = entry[0];
    this.value = this.keysOnly ? undefined : entry[1];
    return true;
  }

  continueToKey(_key: Key): Promise<boolean> {
    return unsupported("cursor continueToKey");
  }

  advance(_count: number): Promise<boolean> {
    return unsupported("cursor advance");
  }

  async delete(): Promise<void> {
    this.store.rows.delete(this.primaryKey);
  }

  async update(record: DBRecord): Promise<void> {
    this.store.rows.set(String(record[this.store.pk]), record);
  }

  close(): void {}
}

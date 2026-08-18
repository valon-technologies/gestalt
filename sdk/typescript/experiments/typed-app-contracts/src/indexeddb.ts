import type { JsonValue } from "./model.ts";
import {
  assertIDBKey,
  cloneIDBKey,
  compareIDBKeys,
  decodeIDBKey,
  decodeStructuredClone,
  encodeIDBKey,
  encodeStructuredClone,
} from "./indexeddb-wire.ts";

declare global {
  interface IDBGetAllOptions {
    query?: IDBValidKey | IDBKeyRange | null;
    count?: number;
    direction?: IDBCursorDirection;
  }

  interface IDBRecord {
    readonly key: IDBValidKey;
    readonly primaryKey: IDBValidKey;
    readonly value: any;
  }

  interface IDBObjectStore {
    getAll(queryOrOptions?: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null, count?: number): IDBRequest<any[]>;
    getAllKeys(queryOrOptions?: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null, count?: number): IDBRequest<IDBValidKey[]>;
    getAllRecords(options?: IDBGetAllOptions): IDBRequest<IDBRecord[]>;
  }

  interface IDBIndex {
    getAll(queryOrOptions?: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null, count?: number): IDBRequest<any[]>;
    getAllKeys(queryOrOptions?: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null, count?: number): IDBRequest<IDBValidKey[]>;
    getAllRecords(options?: IDBGetAllOptions): IDBRequest<IDBRecord[]>;
  }
}

type TransactionTool = (input: any) => Promise<any>;
type Handler<Target, EventType extends Event> = ((this: Target, event: EventType) => any) | null;

export interface RemoteIndexDefinition {
  name: string;
  keyPath: string | string[];
  multiEntry?: boolean;
  unique?: boolean;
}

export interface RemoteObjectStoreDefinition {
  name: string;
  keyPath?: string | string[] | null;
  autoIncrement?: boolean;
  indexes?: RemoteIndexDefinition[];
}

export interface RemoteIDBDatabaseOptions {
  name: string;
  version: number;
  objectStores: RemoteObjectStoreDefinition[];
  upgrade?: boolean;
  nonce?: () => string;
}

export interface RemoteIDBDatabaseConnection {
  database: IDBDatabase;
  upgradeTransaction: IDBTransaction | null;
  forceClose(): void;
  versionChange(oldVersion: number, newVersion: number | null): void;
}

export interface RemoteIDBFactoryOptions {
  nonce?: () => string;
}

export function createRemoteIDBFactory(
  invoke: TransactionTool,
  options: RemoteIDBFactoryOptions = {},
): IDBFactory {
  return new RemoteFactory(invoke, options.nonce ?? (() => globalThis.crypto.randomUUID()));
}

interface StoreMetadata {
  name: string;
  keyPath: string | string[] | null;
  autoIncrement: boolean;
  indexes: Map<string, IndexMetadata>;
}

interface IndexMetadata {
  name: string;
  keyPath: string | string[];
  multiEntry: boolean;
  unique: boolean;
}

interface CursorDescriptor {
  cursorId: string;
  key: IDBValidKey;
  primaryKey: IDBValidKey;
  value?: unknown;
}

interface ExistingTransactionSession {
  transactionId: string;
  nextSequence: number;
}

export function connectRemoteIDBDatabase(
  invoke: TransactionTool,
  options: RemoteIDBDatabaseOptions,
): RemoteIDBDatabaseConnection {
  const database = new RemoteDatabase(invoke, options);
  const upgradeTransaction = options.upgrade ? database.startUpgrade() : null;
  return {
    database,
    upgradeTransaction,
    forceClose: () => database.forceClose(),
    versionChange: (oldVersion, newVersion) => database.versionChange(oldVersion, newVersion),
  };
}

class RemoteDatabase extends EventTarget implements IDBDatabase {
  readonly name: string;
  readonly version: number;
  onabort: Handler<IDBDatabase, Event> = null;
  onclose: Handler<IDBDatabase, Event> = null;
  onerror: Handler<IDBDatabase, Event> = null;
  onversionchange: Handler<IDBDatabase, IDBVersionChangeEvent> = null;

  private readonly stores = new Map<string, StoreMetadata>();
  private readonly transactions = new Set<RemoteTransaction>();
  private readonly nonce: () => string;
  private closePending = false;
  private closed = false;
  private upgrade: RemoteTransaction | null = null;

  constructor(
    private readonly invoke: TransactionTool,
    options: RemoteIDBDatabaseOptions,
  ) {
    super();
    this.name = options.name;
    this.version = options.version;
    this.nonce = options.nonce ?? (() => globalThis.crypto.randomUUID());
    for (const definition of options.objectStores) {
      if (this.stores.has(definition.name)) throw domError("ConstraintError", definition.name);
      this.stores.set(definition.name, storeMetadata(definition));
    }
  }

  get objectStoreNames(): DOMStringList {
    return new StringList([...this.stores.keys()]);
  }

  transaction(
    storeNames: string | string[],
    mode: IDBTransactionMode = "readonly",
    options: IDBTransactionOptions = {},
  ): IDBTransaction {
    if (this.closePending || this.closed || this.upgrade) throw domError("InvalidStateError");
    if (mode !== "readonly" && mode !== "readwrite") throw new TypeError(`invalid transaction mode ${mode}`);
    const scope = [...new Set(typeof storeNames === "string" ? [storeNames] : storeNames)];
    if (scope.length === 0) throw domError("InvalidAccessError");
    for (const name of scope) {
      if (!this.stores.has(name)) throw domError("NotFoundError", name);
    }
    return this.createTransaction(scope, mode, options.durability ?? "default");
  }

  close(): void {
    this.closePending = true;
    if (this.transactions.size === 0) this.closed = true;
  }

  createObjectStore(name: string, options: IDBObjectStoreParameters = {}): IDBObjectStore {
    const transaction = this.activeUpgrade();
    if (this.stores.has(name)) throw domError("ConstraintError", name);
    const keyPath = options.keyPath ?? null;
    validateKeyPath(keyPath);
    if (options.autoIncrement && (keyPath === "" || Array.isArray(keyPath))) {
      throw domError("InvalidAccessError");
    }
    const metadata = storeMetadata({
      name,
      keyPath,
      ...(options.autoIncrement === undefined ? {} : { autoIncrement: options.autoIncrement }),
    });
    this.stores.set(name, metadata);
    transaction.addScope(name);
    transaction.schemaCommand("createObjectStore", {
      store: name,
      keyPath: keyPath as JsonValue,
      autoIncrement: options.autoIncrement ?? false,
    });
    return new RemoteObjectStore(transaction, metadata);
  }

  deleteObjectStore(name: string): void {
    const transaction = this.activeUpgrade();
    if (!this.stores.has(name)) throw domError("NotFoundError", name);
    this.stores.delete(name);
    transaction.schemaCommand("deleteObjectStore", { store: name });
  }

  startUpgrade(session?: ExistingTransactionSession): IDBTransaction {
    if (this.upgrade) throw domError("InvalidStateError");
    const transaction = this.createTransaction(
      [...this.stores.keys()],
      "versionchange",
      "strict",
      session,
    );
    this.upgrade = transaction;
    return transaction;
  }

  forceClose(): void {
    if (this.closed) return;
    this.closePending = true;
    this.closed = true;
    for (const transaction of this.transactions) transaction.forceAbort(domError("UnknownError"));
    emit(this, new Event("close"), this.onclose);
  }

  versionChange(oldVersion: number, newVersion: number | null): void {
    emit(
      this,
      new RemoteVersionChangeEvent("versionchange", { oldVersion, newVersion }),
      this.onversionchange,
    );
  }

  transactionFinished(transaction: RemoteTransaction, event: "complete" | "abort", error: DOMException | null): void {
    this.transactions.delete(transaction);
    if (this.upgrade === transaction) this.upgrade = null;
    if (event === "abort") emit(this, new Event("abort", { bubbles: true }), this.onabort);
    if (error) emit(this, new Event("error", { bubbles: true }), this.onerror);
    if (this.closePending && this.transactions.size === 0) this.closed = true;
  }

  metadata(name: string): StoreMetadata {
    const metadata = this.stores.get(name);
    if (!metadata) throw domError("NotFoundError", name);
    return metadata;
  }

  renameStore(metadata: StoreMetadata, name: string): void {
    const previous = metadata.name;
    if (name === previous) return;
    if (this.stores.has(name)) throw domError("ConstraintError", name);
    this.stores.delete(previous);
    metadata.name = name;
    this.stores.set(name, metadata);
  }

  private activeUpgrade(): RemoteTransaction {
    if (!this.upgrade) throw domError("InvalidStateError");
    this.upgrade.assertActive();
    return this.upgrade;
  }

  private createTransaction(
    scope: string[],
    mode: IDBTransactionMode,
    durability: IDBTransactionDurability,
    session?: ExistingTransactionSession,
  ): RemoteTransaction {
    const transaction = new RemoteTransaction(
      this,
      this.invoke,
      scope,
      mode,
      durability,
      this.nonce,
      session,
    );
    this.transactions.add(transaction);
    return transaction;
  }
}

class RemoteTransaction extends EventTarget implements IDBTransaction {
  readonly db: IDBDatabase;
  readonly mode: IDBTransactionMode;
  readonly durability: IDBTransactionDurability;
  onabort: Handler<IDBTransaction, Event> = null;
  oncomplete: Handler<IDBTransaction, Event> = null;
  onerror: Handler<IDBTransaction, Event> = null;

  private readonly scope = new Set<string>();
  private readonly stores = new Map<string, RemoteObjectStore>();
  private readonly opened: Promise<void>;
  private chain: Promise<void> = Promise.resolve();
  private transactionId = "";
  private nextSequence = 1;
  private state: "active" | "committing" | "finished" = "active";
  private commitRequested = false;
  private autoCommitTimer: ReturnType<typeof setTimeout> | undefined;
  private queuedOperations = 0;
  private failure: DOMException | null = null;

  constructor(
    database: RemoteDatabase,
    private readonly invoke: TransactionTool,
    scope: string[],
    mode: IDBTransactionMode,
    durability: IDBTransactionDurability,
    private readonly nonce: () => string,
    session?: ExistingTransactionSession,
  ) {
    super();
    this.db = database;
    this.mode = mode;
    this.durability = durability;
    for (const name of scope) this.scope.add(name);
    if (session) {
      this.transactionId = session.transactionId;
      this.nextSequence = session.nextSequence;
      this.opened = Promise.resolve();
    } else {
      this.opened = this.open();
    }
    this.scheduleAutoCommit();
  }

  get objectStoreNames(): DOMStringList {
    return new StringList([...this.scope]);
  }

  get error(): DOMException | null {
    return this.failure;
  }

  objectStore(name: string): IDBObjectStore {
    this.assertActive();
    if (!this.scope.has(name)) throw domError("NotFoundError", name);
    let store = this.stores.get(name);
    if (!store) {
      store = new RemoteObjectStore(this, (this.db as RemoteDatabase).metadata(name));
      this.stores.set(name, store);
    }
    return store;
  }

  abort(): void {
    this.assertActive();
    this.finishAbort(null);
  }

  commit(): void {
    this.assertActive();
    this.commitRequested = true;
    if (this.queuedOperations === 0) this.scheduleAutoCommit(true);
  }

  assertActive(): void {
    if (this.state !== "active") throw domError("TransactionInactiveError");
  }

  assertWrite(): void {
    this.assertActive();
    if (this.mode === "readonly") throw domError("ReadOnlyError");
  }

  assertVersionChange(): void {
    this.assertActive();
    if (this.mode !== "versionchange") throw domError("InvalidStateError");
  }

  addScope(name: string): void {
    this.assertVersionChange();
    this.scope.add(name);
  }

  renameObjectStore(store: RemoteObjectStore, metadata: StoreMetadata, name: string): void {
    this.assertVersionChange();
    const previous = metadata.name;
    if (name === previous) return;
    (this.db as RemoteDatabase).renameStore(metadata, name);
    this.scope.delete(previous);
    this.scope.add(name);
    this.stores.delete(previous);
    this.stores.set(name, store);
    this.schemaCommand("renameObjectStore", { store: previous, name });
  }

  schemaCommand(kind: string, payload: Record<string, JsonValue>): void {
    this.assertVersionChange();
    this.enqueue(async () => {
      await this.sendRequest(kind, payload);
    });
  }

  request<T>(
    source: IDBObjectStore | IDBIndex | IDBCursor,
    kind: string,
    payload: Record<string, JsonValue> | Promise<Record<string, JsonValue>>,
    decode: (value: JsonValue, request: RemoteRequest<T>) => T,
    write = false,
  ): IDBRequest<T> {
    if (write) this.assertWrite(); else this.assertActive();
    const request = new RemoteRequest<T>(source, this);
    this.enqueue(async () => {
      try {
        const value = await this.sendRequest(kind, await payload);
        request.succeed(decode(value, request));
      } catch (error) {
        const exception = asDOMException(error);
        const prevented = request.fail(exception);
        emit(this, new Event("error", { bubbles: true, cancelable: true }), this.onerror);
        if (!prevented) throw exception;
      }
    });
    return request;
  }

  continueCursor(
    cursor: RemoteCursor,
    request: RemoteRequest<IDBCursorWithValue | null>,
    kind: "cursorContinue" | "cursorAdvance" | "cursorContinuePrimaryKey",
    payload: Record<string, JsonValue>,
  ): void {
    this.assertActive();
    request.pending();
    this.enqueue(async () => {
      try {
        const value = await this.sendRequest(kind, { cursorId: cursor.cursorId, ...payload });
        const descriptor = cursorDescriptor(value);
        if (!descriptor) request.succeed(null);
        else {
          cursor.move(descriptor);
          request.succeed(cursor);
        }
      } catch (error) {
        const exception = asDOMException(error);
        if (!request.fail(exception)) throw exception;
      }
    });
  }

  forceAbort(error: DOMException): void {
    if (this.state === "finished") return;
    this.finishAbort(error);
  }

  private async open(): Promise<void> {
    const reply = expectReply(await this.invoke({
      op: "begin",
      database: this.db.name,
      scope: [...this.scope],
      mode: this.mode,
      durability: this.durability,
    }), "opened");
    this.transactionId = stringField(reply, "transactionId");
    this.nextSequence = integerField(reply, "nextSequence");
  }

  private enqueue(operation: () => Promise<void>): void {
    if (this.autoCommitTimer) clearTimeout(this.autoCommitTimer);
    this.queuedOperations += 1;
    this.chain = this.chain
      .then(() => this.opened)
      .then(operation)
      .catch((error) => {
        if (this.state !== "finished") this.finishAbort(asDOMException(error));
      })
      .finally(() => {
        this.queuedOperations -= 1;
        if (this.queuedOperations === 0 && this.state === "active") {
          this.scheduleAutoCommit(this.commitRequested);
        }
      });
  }

  private async sendRequest(kind: string, payload: Record<string, JsonValue>): Promise<JsonValue> {
    await this.opened;
    const sequence = this.nextSequence;
    const reply = expectReply(await this.invoke({
      op: "request",
      transactionId: this.transactionId,
      sequence,
      request: { kind, ...payload },
    }), "result");
    if (
      stringField(reply, "transactionId") !== this.transactionId ||
      integerField(reply, "sequence") !== sequence
    ) {
      throw new IndexedDBProtocolError("request reply does not match its command");
    }
    this.nextSequence += 1;
    return reply.result as JsonValue;
  }

  private scheduleAutoCommit(explicit = false): void {
    if (this.queuedOperations !== 0) return;
    if (this.autoCommitTimer) clearTimeout(this.autoCommitTimer);
    this.autoCommitTimer = setTimeout(() => {
      if (this.state !== "active") return;
      this.state = "committing";
      this.chain = this.chain.then(() => this.opened).then(async () => {
        await commit(this.invoke, this.transactionId, this.nextSequence, this.nonce());
        this.state = "finished";
        emit(this, new Event("complete"), this.oncomplete);
        (this.db as RemoteDatabase).transactionFinished(this, "complete", null);
      }).catch((error) => this.finishAbort(asDOMException(error)));
    }, explicit ? 0 : 1);
  }

  private finishAbort(error: DOMException | null): void {
    if (this.state === "finished") return;
    if (this.autoCommitTimer) clearTimeout(this.autoCommitTimer);
    this.state = "finished";
    this.failure = error;
    this.chain = this.chain.then(() => this.opened).then(async () => {
      await abort(this.invoke, this.transactionId, this.nextSequence).catch(() => undefined);
    }).finally(() => {
      emit(this, new Event("abort", { bubbles: true }), this.onabort);
      (this.db as RemoteDatabase).transactionFinished(this, "abort", error);
    });
  }
}

class RemoteRequest<T> extends EventTarget implements IDBRequest<T> {
  onerror: Handler<IDBRequest<T>, Event> = null;
  onsuccess: Handler<IDBRequest<T>, Event> = null;
  readonly source: IDBObjectStore | IDBIndex | IDBCursor;
  protected transactionValue: IDBTransaction | null;
  protected state: IDBRequestReadyState = "pending";
  protected value: T | undefined;
  protected exception: DOMException | null = null;

  constructor(source: IDBObjectStore | IDBIndex | IDBCursor, transaction: IDBTransaction | null) {
    super();
    this.source = source;
    this.transactionValue = transaction;
  }

  get transaction(): IDBTransaction | null {
    return this.transactionValue;
  }

  get readyState(): IDBRequestReadyState {
    return this.state;
  }

  get result(): T {
    if (this.state !== "done") throw domError("InvalidStateError");
    return this.value as T;
  }

  get error(): DOMException | null {
    if (this.state !== "done") throw domError("InvalidStateError");
    return this.exception;
  }

  pending(): void {
    this.state = "pending";
    this.exception = null;
  }

  prepare(value: T): void {
    this.value = value;
    this.exception = null;
    this.state = "done";
  }

  succeed(value: T): void {
    this.value = value;
    this.exception = null;
    this.state = "done";
    emit(this, new Event("success"), this.onsuccess);
  }

  fail(error: DOMException): boolean {
    this.value = undefined;
    this.exception = error;
    this.state = "done";
    const event = new Event("error", { bubbles: true, cancelable: true });
    emit(this, event, this.onerror);
    return event.defaultPrevented;
  }
}

class RemoteOpenRequest extends RemoteRequest<IDBDatabase> implements IDBOpenDBRequest {
  onblocked: Handler<IDBOpenDBRequest, IDBVersionChangeEvent> = null;
  onupgradeneeded: Handler<IDBOpenDBRequest, IDBVersionChangeEvent> = null;

  constructor() {
    super(null as unknown as IDBObjectStore, null);
  }

  async upgrade(
    database: IDBDatabase,
    transaction: IDBTransaction,
    oldVersion: number,
    newVersion: number,
  ): Promise<void> {
    this.prepare(database);
    this.transactionValue = transaction;
    emit(
      this,
      new RemoteVersionChangeEvent("upgradeneeded", { oldVersion, newVersion }),
      this.onupgradeneeded,
    );
    await new Promise<void>((resolve, reject) => {
      transaction.addEventListener("complete", () => resolve(), { once: true });
      transaction.addEventListener("abort", () => reject(transaction.error ?? domError("AbortError")), {
        once: true,
      });
    });
    this.transactionValue = null;
    this.succeed(database);
  }

  clearTransaction(): void {
    this.transactionValue = null;
  }
}

class RemoteFactory implements IDBFactory {
  constructor(
    private readonly invoke: TransactionTool,
    private readonly nonce: () => string,
  ) {}

  cmp(first: any, second: any): number {
    return compareIDBKeys(first, second);
  }

  databases(): Promise<IDBDatabaseInfo[]> {
    return Promise.resolve().then(async () => {
      const reply = expectReply(await this.invoke({ op: "factoryDatabases" }), "databases");
      if (!Array.isArray(reply.databases)) throw new IndexedDBProtocolError("databases must be an array");
      return reply.databases.map((entry) => {
        if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
          throw new IndexedDBProtocolError("database metadata must be an object");
        }
        const info = entry as Record<string, unknown>;
        return { name: stringField(info, "name"), version: integerField(info, "version") };
      });
    });
  }

  deleteDatabase(name: string): IDBOpenDBRequest {
    const request = new RemoteOpenRequest();
    void Promise.resolve().then(async () => {
      try {
        expectReply(await this.invoke({ op: "factoryDelete", name: String(name) }), "deleted");
        request.succeed(undefined as unknown as IDBDatabase);
      } catch (error) {
        request.fail(asDOMException(error));
      }
    });
    return request;
  }

  open(name: string, version?: number): IDBOpenDBRequest {
    const databaseName = String(name);
    if (version !== undefined) assertDatabaseVersion(version);
    const request = new RemoteOpenRequest();
    void Promise.resolve().then(async () => {
      try {
        const reply = expectOneReply(
          await this.invoke({
            op: "factoryOpen",
            name: databaseName,
            ...(version === undefined ? {} : { version }),
          }),
          ["database", "upgrade"],
        );
        const database = new RemoteDatabase(this.invoke, {
          name: stringField(reply, "name"),
          version: integerField(reply, reply.op === "upgrade" ? "newVersion" : "version"),
          objectStores: objectStoreDefinitions(reply.objectStores),
          nonce: this.nonce,
        });
        if (reply.op === "database") {
          request.succeed(database);
          return;
        }
        const transaction = database.startUpgrade({
          transactionId: stringField(reply, "transactionId"),
          nextSequence: integerField(reply, "nextSequence"),
        });
        await request.upgrade(
          database,
          transaction,
          integerField(reply, "oldVersion"),
          integerField(reply, "newVersion"),
        );
      } catch (error) {
        request.clearTransaction();
        request.fail(asDOMException(error));
      }
    });
    return request;
  }
}

class RemoteObjectStore implements IDBObjectStore {
  readonly transaction: IDBTransaction;
  readonly keyPath: string | string[] | null;
  readonly autoIncrement: boolean;
  private readonly indexes = new Map<string, RemoteIndex>();

  constructor(
    private readonly remoteTransaction: RemoteTransaction,
    private readonly metadata: StoreMetadata,
  ) {
    this.transaction = remoteTransaction;
    this.keyPath = cloneKeyPath(metadata.keyPath);
    this.autoIncrement = metadata.autoIncrement;
  }

  get name(): string {
    return this.metadata.name;
  }

  set name(value: string) {
    this.remoteTransaction.renameObjectStore(this, this.metadata, String(value));
  }

  get indexNames(): DOMStringList {
    return new StringList([...this.metadata.indexes.keys()]);
  }

  add(value: any, key?: IDBValidKey): IDBRequest<IDBValidKey> {
    return this.write("add", value, key);
  }

  put(value: any, key?: IDBValidKey): IDBRequest<IDBValidKey> {
    return this.write("put", value, key);
  }

  delete(query: IDBValidKey | IDBKeyRange): IDBRequest<undefined> {
    return this.remoteTransaction.request(
      this,
      "delete",
      { store: this.name, query: serializeQuery(query) },
      () => undefined,
      true,
    );
  }

  clear(): IDBRequest<undefined> {
    return this.remoteTransaction.request(
      this,
      "clear",
      { store: this.name },
      () => undefined,
      true,
    );
  }

  get(query: IDBValidKey | IDBKeyRange): IDBRequest<any> {
    return this.remoteTransaction.request(
      this,
      "get",
      { store: this.name, query: serializeQuery(query) },
      decodeOptional,
    );
  }

  getKey(query: IDBValidKey | IDBKeyRange): IDBRequest<IDBValidKey | undefined> {
    return this.remoteTransaction.request(
      this,
      "getKey",
      { store: this.name, query: serializeQuery(query) },
      decodeOptionalKey,
    );
  }

  getAll(
    queryOrOptions: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null = null,
    count?: number,
  ): IDBRequest<any[]> {
    return this.remoteTransaction.request(
      this,
      "getAll",
      listOrOptionsPayload(this.name, queryOrOptions, count),
      decodeValues,
    );
  }

  getAllKeys(
    queryOrOptions: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null = null,
    count?: number,
  ): IDBRequest<IDBValidKey[]> {
    return this.remoteTransaction.request(
      this,
      "getAllKeys",
      listOrOptionsPayload(this.name, queryOrOptions, count),
      decodeKeys,
    );
  }

  getAllRecords(options: IDBGetAllOptions = {}): IDBRequest<IDBRecord[]> {
    return this.remoteTransaction.request(
      this,
      "getAllRecords",
      getAllOptionsPayload(this.name, options),
      decodeRecords,
    );
  }

  count(query?: IDBValidKey | IDBKeyRange): IDBRequest<number> {
    return this.remoteTransaction.request(
      this,
      "count",
      {
        store: this.name,
        query: query === undefined ? null : serializeQuery(query),
      },
      numberValue,
    );
  }

  openCursor(
    query: IDBValidKey | IDBKeyRange | null = null,
    direction: IDBCursorDirection = "next",
  ): IDBRequest<IDBCursorWithValue | null> {
    return this.openCursorRequest(query, direction, false) as IDBRequest<IDBCursorWithValue | null>;
  }

  openKeyCursor(
    query: IDBValidKey | IDBKeyRange | null = null,
    direction: IDBCursorDirection = "next",
  ): IDBRequest<IDBCursor | null> {
    return this.openCursorRequest(query, direction, true) as IDBRequest<IDBCursor | null>;
  }

  index(name: string): IDBIndex {
    this.remoteTransaction.assertActive();
    const metadata = this.metadata.indexes.get(name);
    if (!metadata) throw domError("NotFoundError", name);
    let index = this.indexes.get(name);
    if (!index) {
      index = new RemoteIndex(this.remoteTransaction, this, metadata);
      this.indexes.set(name, index);
    }
    return index;
  }

  createIndex(
    name: string,
    keyPath: string | string[],
    options: IDBIndexParameters = {},
  ): IDBIndex {
    this.remoteTransaction.assertVersionChange();
    validateKeyPath(keyPath);
    if (this.metadata.indexes.has(name)) throw domError("ConstraintError", name);
    if (options.multiEntry && Array.isArray(keyPath)) throw domError("InvalidAccessError");
    const metadata: IndexMetadata = {
      name,
      keyPath: cloneKeyPath(keyPath),
      multiEntry: options.multiEntry ?? false,
      unique: options.unique ?? false,
    };
    this.metadata.indexes.set(name, metadata);
    this.remoteTransaction.schemaCommand("createIndex", {
      store: this.name,
      index: name,
      keyPath: cloneKeyPath(keyPath) as JsonValue,
      multiEntry: metadata.multiEntry,
      unique: metadata.unique,
    });
    const index = new RemoteIndex(this.remoteTransaction, this, metadata);
    this.indexes.set(name, index);
    return index;
  }

  deleteIndex(name: string): void {
    this.remoteTransaction.assertVersionChange();
    if (!this.metadata.indexes.has(name)) throw domError("NotFoundError", name);
    this.metadata.indexes.delete(name);
    this.indexes.delete(name);
    this.remoteTransaction.schemaCommand("deleteIndex", { store: this.name, index: name });
  }

  renameIndex(index: RemoteIndex, metadata: IndexMetadata, name: string): void {
    this.remoteTransaction.assertVersionChange();
    const previous = metadata.name;
    if (name === previous) return;
    if (this.metadata.indexes.has(name)) throw domError("ConstraintError", name);
    this.metadata.indexes.delete(previous);
    metadata.name = name;
    this.metadata.indexes.set(name, metadata);
    this.indexes.delete(previous);
    this.indexes.set(name, index);
    this.remoteTransaction.schemaCommand("renameIndex", {
      store: this.name,
      index: previous,
      name,
    });
  }

  private write(kind: "add" | "put", value: unknown, key?: IDBValidKey): IDBRequest<IDBValidKey> {
    const encodedValue = encodeStructuredClone(value);
    const payload = encodedValue.then((encoded): Record<string, JsonValue> => ({
      store: this.name,
      value: encoded,
      ...(key === undefined ? {} : { key: serializeKey(key) }),
    }));
    return this.remoteTransaction.request(
      this,
      kind,
      payload,
      (result) => keyValue(result),
      true,
    );
  }

  private openCursorRequest(
    query: IDBValidKey | IDBKeyRange | null,
    direction: IDBCursorDirection,
    keyOnly: boolean,
  ): IDBRequest<IDBCursorWithValue | null> {
    assertDirection(direction);
    return this.remoteTransaction.request(
      this,
      "openCursor",
      {
        store: this.name,
        query: query === null ? null : serializeQuery(query),
        direction,
        keyOnly,
      },
      (value, request) => {
        const descriptor = cursorDescriptor(value);
        return descriptor
          ? new RemoteCursor(
              this.remoteTransaction,
              this,
              direction,
              keyOnly,
              descriptor,
              request as RemoteRequest<IDBCursorWithValue | null>,
            )
          : null;
      },
    );
  }
}

class RemoteIndex implements IDBIndex {
  readonly objectStore: IDBObjectStore;
  readonly keyPath: string | string[];
  readonly multiEntry: boolean;
  readonly unique: boolean;

  constructor(
    private readonly transaction: RemoteTransaction,
    objectStore: RemoteObjectStore,
    private readonly metadata: IndexMetadata,
  ) {
    this.objectStore = objectStore;
    this.keyPath = cloneKeyPath(metadata.keyPath);
    this.multiEntry = metadata.multiEntry;
    this.unique = metadata.unique;
  }

  get name(): string {
    return this.metadata.name;
  }

  set name(value: string) {
    (this.objectStore as RemoteObjectStore).renameIndex(this, this.metadata, String(value));
  }

  get(query: IDBValidKey | IDBKeyRange): IDBRequest<any> {
    return this.read("indexGet", query, decodeOptional);
  }

  getKey(query: IDBValidKey | IDBKeyRange): IDBRequest<IDBValidKey | undefined> {
    return this.read("indexGetKey", query, decodeOptionalKey);
  }

  getAll(
    queryOrOptions: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null = null,
    count?: number,
  ): IDBRequest<any[]> {
    return this.list("indexGetAll", queryOrOptions, count, decodeValues) as IDBRequest<any[]>;
  }

  getAllKeys(
    queryOrOptions: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null = null,
    count?: number,
  ): IDBRequest<IDBValidKey[]> {
    return this.list("indexGetAllKeys", queryOrOptions, count, decodeKeys) as IDBRequest<IDBValidKey[]>;
  }

  getAllRecords(options: IDBGetAllOptions = {}): IDBRequest<IDBRecord[]> {
    return this.transaction.request(
      this,
      "indexGetAllRecords",
      { ...getAllOptionsPayload(this.objectStore.name, options), index: this.name },
      decodeRecords,
    );
  }

  count(query?: IDBValidKey | IDBKeyRange): IDBRequest<number> {
    return this.transaction.request(
      this,
      "indexCount",
      {
        store: this.objectStore.name,
        index: this.name,
        query: query === undefined ? null : serializeQuery(query),
      },
      numberValue,
    );
  }

  openCursor(
    query: IDBValidKey | IDBKeyRange | null = null,
    direction: IDBCursorDirection = "next",
  ): IDBRequest<IDBCursorWithValue | null> {
    return this.openCursorRequest(query, direction, false) as IDBRequest<IDBCursorWithValue | null>;
  }

  openKeyCursor(
    query: IDBValidKey | IDBKeyRange | null = null,
    direction: IDBCursorDirection = "next",
  ): IDBRequest<IDBCursor | null> {
    return this.openCursorRequest(query, direction, true) as IDBRequest<IDBCursor | null>;
  }

  private read<T>(
    kind: string,
    query: IDBValidKey | IDBKeyRange,
    decode: (value: JsonValue) => T,
  ): IDBRequest<T> {
    return this.transaction.request(
      this,
      kind,
      { store: this.objectStore.name, index: this.name, query: serializeQuery(query) },
      decode,
    );
  }

  private list(
    kind: string,
    queryOrOptions: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null,
    count?: number,
    decode: (value: JsonValue) => unknown[] = decodeValues,
  ): IDBRequest<unknown[]> {
    return this.transaction.request(
      this,
      kind,
      {
        ...listOrOptionsPayload(this.objectStore.name, queryOrOptions, count),
        index: this.name,
      },
      decode,
    );
  }

  private openCursorRequest(
    query: IDBValidKey | IDBKeyRange | null,
    direction: IDBCursorDirection,
    keyOnly: boolean,
  ): IDBRequest<IDBCursorWithValue | null> {
    assertDirection(direction);
    return this.transaction.request(
      this,
      "openCursor",
      {
        store: this.objectStore.name,
        index: this.name,
        query: query === null ? null : serializeQuery(query),
        direction,
        keyOnly,
      },
      (value, request) => {
        const descriptor = cursorDescriptor(value);
        return descriptor
          ? new RemoteCursor(
              this.transaction,
              this,
              direction,
              keyOnly,
              descriptor,
              request as RemoteRequest<IDBCursorWithValue | null>,
            )
          : null;
      },
    );
  }
}

class RemoteCursor implements IDBCursorWithValue {
  readonly request: IDBRequest;
  readonly source: IDBObjectStore | IDBIndex;
  readonly direction: IDBCursorDirection;
  readonly cursorId: string;
  private gotValue = true;
  private currentKey: IDBValidKey;
  private currentPrimaryKey: IDBValidKey;
  private currentValue: unknown;

  constructor(
    private readonly transaction: RemoteTransaction,
    source: IDBObjectStore | IDBIndex,
    direction: IDBCursorDirection,
    private readonly keyOnly: boolean,
    descriptor: CursorDescriptor,
    request: RemoteRequest<IDBCursorWithValue | null>,
  ) {
    this.source = source;
    this.direction = direction;
    this.cursorId = descriptor.cursorId;
    this.request = request;
    this.currentKey = descriptor.key;
    this.currentPrimaryKey = descriptor.primaryKey;
    this.currentValue = descriptor.value;
  }

  get key(): IDBValidKey {
    return cloneKey(this.currentKey);
  }

  get primaryKey(): IDBValidKey {
    return cloneKey(this.currentPrimaryKey);
  }

  get value(): any {
    if (this.keyOnly) return undefined;
    return this.currentValue;
  }

  advance(count: number): void {
    if (!Number.isInteger(count) || count <= 0 || count > 0xffff_ffff) {
      throw new TypeError("count must be an unsigned long greater than zero");
    }
    this.advanceWith("cursorAdvance", { count });
  }

  continue(key?: IDBValidKey): void {
    const payload: Record<string, JsonValue> = {};
    if (key !== undefined) payload.key = serializeKey(key);
    this.advanceWith("cursorContinue", payload);
  }

  continuePrimaryKey(key: IDBValidKey, primaryKey: IDBValidKey): void {
    if (!(this.source instanceof RemoteIndex) || this.direction.endsWith("unique")) {
      throw domError("InvalidAccessError");
    }
    this.advanceWith("cursorContinuePrimaryKey", {
      key: serializeKey(key),
      primaryKey: serializeKey(primaryKey),
    });
  }

  update(value: any): IDBRequest<IDBValidKey> {
    this.assertValue();
    const encoded = encodeStructuredClone(value);
    return this.transaction.request(
      this,
      "cursorUpdate",
      encoded.then((wire) => ({ cursorId: this.cursorId, value: wire })),
      keyValue,
      true,
    );
  }

  delete(): IDBRequest<undefined> {
    this.assertValue();
    return this.transaction.request(
      this,
      "cursorDelete",
      { cursorId: this.cursorId },
      () => undefined,
      true,
    );
  }

  move(descriptor: CursorDescriptor): void {
    this.currentKey = descriptor.key;
    this.currentPrimaryKey = descriptor.primaryKey;
    this.currentValue = descriptor.value;
    this.gotValue = true;
  }

  private advanceWith(
    kind: "cursorContinue" | "cursorAdvance" | "cursorContinuePrimaryKey",
    payload: Record<string, JsonValue>,
  ): void {
    this.assertValue();
    this.gotValue = false;
    this.transaction.continueCursor(
      this,
      this.request as RemoteRequest<IDBCursorWithValue | null>,
      kind,
      payload,
    );
  }

  private assertValue(): void {
    this.transaction.assertActive();
    if (!this.gotValue) throw domError("InvalidStateError");
  }
}

export class RemoteIDBKeyRange implements IDBKeyRange {
  private constructor(
    readonly lower: any,
    readonly upper: any,
    readonly lowerOpen: boolean,
    readonly upperOpen: boolean,
  ) {}

  static only(value: any): IDBKeyRange {
    assertKey(value);
    return new RemoteIDBKeyRange(cloneKey(value), cloneKey(value), false, false);
  }

  static lowerBound(lower: any, open = false): IDBKeyRange {
    assertKey(lower);
    return new RemoteIDBKeyRange(cloneKey(lower), undefined, open, true);
  }

  static upperBound(upper: any, open = false): IDBKeyRange {
    assertKey(upper);
    return new RemoteIDBKeyRange(undefined, cloneKey(upper), true, open);
  }

  static bound(lower: any, upper: any, lowerOpen = false, upperOpen = false): IDBKeyRange {
    assertKey(lower);
    assertKey(upper);
    const order = compareKeys(lower, upper);
    if (order > 0 || (order === 0 && (lowerOpen || upperOpen))) throw domError("DataError");
    return new RemoteIDBKeyRange(cloneKey(lower), cloneKey(upper), lowerOpen, upperOpen);
  }

  includes(key: any): boolean {
    assertKey(key);
    if (this.lower !== undefined) {
      const lower = compareKeys(key, this.lower);
      if (lower < 0 || (lower === 0 && this.lowerOpen)) return false;
    }
    if (this.upper !== undefined) {
      const upper = compareKeys(key, this.upper);
      if (upper > 0 || (upper === 0 && this.upperOpen)) return false;
    }
    return true;
  }
}

export { RemoteIDBKeyRange as IDBKeyRange };

class StringList implements DOMStringList {
  readonly [index: number]: string;
  readonly length: number;

  constructor(values: string[]) {
    const sorted = [...values].sort();
    this.length = sorted.length;
    for (const [index, value] of sorted.entries()) {
      Object.defineProperty(this, index, { enumerable: true, value });
    }
  }

  contains(string: string): boolean {
    for (let index = 0; index < this.length; index += 1) {
      if (this[index] === string) return true;
    }
    return false;
  }

  item(index: number): string | null {
    return this[index] ?? null;
  }

  *[Symbol.iterator](): ArrayIterator<string> {
    for (let index = 0; index < this.length; index += 1) yield this[index]!;
  }
}

class RemoteVersionChangeEvent extends Event implements IDBVersionChangeEvent {
  readonly oldVersion: number;
  readonly newVersion: number | null;

  constructor(type: string, init: IDBVersionChangeEventInit) {
    super(type);
    this.oldVersion = init.oldVersion ?? 0;
    this.newVersion = init.newVersion ?? null;
  }
}

async function commit(
  invoke: TransactionTool,
  transactionId: string,
  sequence: number,
  nonce: string,
): Promise<void> {
  try {
    const reply = expectReply(
      await invoke({ op: "commit", transactionId, sequence, nonce }),
      "committed",
    );
    if (
      stringField(reply, "transactionId") !== transactionId ||
      stringField(reply, "nonce") !== nonce
    ) {
      throw new IndexedDBProtocolError("commit reply does not match its command");
    }
  } catch (error) {
    const status = expectReply(
      await invoke({ op: "status", transactionId, nonce }),
      "status",
    );
    if (status.outcome !== "committed") throw error;
  }
}

async function abort(
  invoke: TransactionTool,
  transactionId: string,
  sequence: number,
): Promise<void> {
  const reply = expectReply(
    await invoke({ op: "abort", transactionId, sequence }),
    "aborted",
  );
  if (stringField(reply, "transactionId") !== transactionId) {
    throw new IndexedDBProtocolError("abort reply does not match its command");
  }
}

function storeMetadata(definition: RemoteObjectStoreDefinition): StoreMetadata {
  const indexes = new Map<string, IndexMetadata>();
  for (const index of definition.indexes ?? []) {
    if (indexes.has(index.name)) throw domError("ConstraintError", index.name);
    indexes.set(index.name, {
      name: index.name,
      keyPath: cloneKeyPath(index.keyPath),
      multiEntry: index.multiEntry ?? false,
      unique: index.unique ?? false,
    });
  }
  return {
    name: definition.name,
    keyPath: cloneKeyPath(definition.keyPath ?? null),
    autoIncrement: definition.autoIncrement ?? false,
    indexes,
  };
}

function listPayload(
  store: string,
  query: IDBValidKey | IDBKeyRange | null,
  count: number | undefined,
): Record<string, JsonValue> {
  const payload: Record<string, JsonValue> = {
    store,
    query: query === null ? null : serializeQuery(query),
  };
  if (count !== undefined) {
    if (!Number.isInteger(count) || count < 0 || count > 0xffff_ffff) {
      throw new TypeError("count is outside unsigned long range");
    }
    payload.count = count;
  }
  return payload;
}

function getAllOptionsPayload(
  store: string,
  options: IDBGetAllOptions,
): Record<string, JsonValue> {
  const direction = options.direction ?? "next";
  assertDirection(direction);
  return {
    ...listPayload(store, options.query ?? null, options.count),
    direction,
  };
}

function listOrOptionsPayload(
  store: string,
  queryOrOptions: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null,
  count: number | undefined,
): Record<string, JsonValue> {
  if (isGetAllOptions(queryOrOptions)) {
    if (count !== undefined) throw new TypeError("count cannot accompany IDBGetAllOptions");
    return getAllOptionsPayload(store, queryOrOptions);
  }
  return { ...listPayload(store, queryOrOptions, count), direction: "next" };
}

function isGetAllOptions(
  value: IDBValidKey | IDBKeyRange | IDBGetAllOptions | null,
): value is IDBGetAllOptions {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    !(value instanceof Date) &&
    !ArrayBuffer.isView(value) &&
    !(value instanceof ArrayBuffer) &&
    !isKeyRange(value as IDBValidKey | IDBKeyRange)
  );
}

function serializeQuery(query: IDBValidKey | IDBKeyRange): JsonValue {
  if (isKeyRange(query)) {
    return {
      type: "range",
      lower: query.lower === undefined ? null : serializeKey(query.lower),
      upper: query.upper === undefined ? null : serializeKey(query.upper),
      lowerOpen: query.lowerOpen,
      upperOpen: query.upperOpen,
    };
  }
  return { type: "key", key: serializeKey(query) };
}

function isKeyRange(value: IDBValidKey | IDBKeyRange): value is IDBKeyRange {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    "lowerOpen" in value &&
    "upperOpen" in value &&
    "includes" in value
  );
}

function serializeKey(key: IDBValidKey): JsonValue {
  return encodeIDBKey(key);
}

function keyValue(value: JsonValue): IDBValidKey {
  return decodeIDBKey(value);
}

function decodeOptional(value: JsonValue): unknown {
  const envelope = recordValue(value);
  return envelope.present === true ? decodeStructuredClone(envelope.value as JsonValue) : undefined;
}

function decodeOptionalKey(value: JsonValue): IDBValidKey | undefined {
  const envelope = recordValue(value);
  return envelope.present === true ? decodeIDBKey(envelope.value as JsonValue) : undefined;
}

function cursorDescriptor(value: JsonValue): CursorDescriptor | null {
  if (value === null) return null;
  const descriptor = recordValue(value);
  const cursorId = descriptor.cursorId;
  if (typeof cursorId !== "string") throw new IndexedDBProtocolError("cursorId must be a string");
  return {
    cursorId,
    key: decodeIDBKey(descriptor.key as JsonValue),
    primaryKey: decodeIDBKey(descriptor.primaryKey as JsonValue),
    ...(Object.hasOwn(descriptor, "value")
      ? { value: decodeStructuredClone(descriptor.value as JsonValue) }
      : {}),
  };
}

function assertKey(value: unknown): asserts value is IDBValidKey {
  assertIDBKey(value);
}

function isSupportedKey(value: unknown): value is IDBValidKey {
  try {
    assertKey(value);
    return true;
  } catch {
    return false;
  }
}

function compareKeys(left: IDBValidKey, right: IDBValidKey): number {
  return compareIDBKeys(left, right);
}

function cloneKey<T extends IDBValidKey>(value: T): T {
  return cloneIDBKey(value);
}

function validateKeyPath(keyPath: string | string[] | null): void {
  if (keyPath === null) return;
  const valid = (path: string): boolean =>
    path === "" || path.split(".").every((part) => /^[$A-Z_a-z][$\w]*$/.test(part));
  if (typeof keyPath === "string" ? !valid(keyPath) : !keyPath.every(valid)) {
    throw domError("SyntaxError");
  }
}

function cloneKeyPath<T extends string | string[] | null>(keyPath: T): T {
  return (Array.isArray(keyPath) ? [...keyPath] : keyPath) as T;
}

function assertDirection(direction: string): asserts direction is IDBCursorDirection {
  if (!["next", "nextunique", "prev", "prevunique"].includes(direction)) {
    throw new TypeError(`invalid cursor direction ${direction}`);
  }
}

function decodeValues(value: JsonValue): any[] {
  if (!Array.isArray(value)) throw new IndexedDBProtocolError("expected an array result");
  return value.map(decodeStructuredClone);
}

function decodeKeys(value: JsonValue): IDBValidKey[] {
  if (!Array.isArray(value)) throw new IndexedDBProtocolError("expected an array result");
  return value.map(decodeIDBKey);
}

function decodeRecords(value: JsonValue): IDBRecord[] {
  if (!Array.isArray(value)) throw new IndexedDBProtocolError("expected an array result");
  return value.map((entry) => {
    const record = recordValue(entry);
    return {
      key: decodeIDBKey(record.key as JsonValue),
      primaryKey: decodeIDBKey(record.primaryKey as JsonValue),
      value: decodeStructuredClone(record.value as JsonValue),
    };
  });
}

function numberValue(value: JsonValue): number {
  if (typeof value !== "number") throw new IndexedDBProtocolError("expected a number result");
  return value;
}

function recordValue(value: JsonValue): Record<string, JsonValue> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new IndexedDBProtocolError("expected an object result");
  }
  return value;
}

function expectReply(value: unknown, op: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new IndexedDBProtocolError(`expected ${op} reply`);
  }
  const reply = value as Record<string, unknown>;
  if (reply.op === "failed") {
    throw domError(
      typeof reply.errorName === "string" ? reply.errorName : "UnknownError",
      typeof reply.message === "string" ? reply.message : "remote IndexedDB request failed",
    );
  }
  if (reply.op !== op) throw new IndexedDBProtocolError(`expected ${op} reply`);
  return reply;
}

function expectOneReply(value: unknown, operations: string[]): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new IndexedDBProtocolError("tool reply must be an object");
  }
  const reply = value as Record<string, unknown>;
  if (reply.op === "failed") {
    throw domError(
      typeof reply.errorName === "string" ? reply.errorName : "UnknownError",
      typeof reply.message === "string" ? reply.message : "remote IndexedDB request failed",
    );
  }
  if (typeof reply.op !== "string" || !operations.includes(reply.op)) {
    throw new IndexedDBProtocolError(`expected ${operations.join(" or ")} reply`);
  }
  return reply;
}

function objectStoreDefinitions(value: unknown): RemoteObjectStoreDefinition[] {
  if (!Array.isArray(value)) throw new IndexedDBProtocolError("objectStores must be an array");
  return value.map((entry) => {
    if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
      throw new IndexedDBProtocolError("object-store metadata must be an object");
    }
    const store = entry as Record<string, unknown>;
    const keyPath = store.keyPath;
    if (!(keyPath === null || typeof keyPath === "string" || isStringArray(keyPath))) {
      throw new IndexedDBProtocolError("object-store keyPath is invalid");
    }
    if (!Array.isArray(store.indexes)) throw new IndexedDBProtocolError("indexes must be an array");
    return {
      name: stringField(store, "name"),
      keyPath,
      autoIncrement: booleanField(store, "autoIncrement"),
      indexes: store.indexes.map((indexEntry) => {
        if (typeof indexEntry !== "object" || indexEntry === null || Array.isArray(indexEntry)) {
          throw new IndexedDBProtocolError("index metadata must be an object");
        }
        const index = indexEntry as Record<string, unknown>;
        const indexKeyPath = index.keyPath;
        if (!(typeof indexKeyPath === "string" || isStringArray(indexKeyPath))) {
          throw new IndexedDBProtocolError("index keyPath is invalid");
        }
        return {
          name: stringField(index, "name"),
          keyPath: indexKeyPath,
          multiEntry: booleanField(index, "multiEntry"),
          unique: booleanField(index, "unique"),
        };
      }),
    };
  });
}

function assertDatabaseVersion(value: number): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new TypeError("database version must be a positive safe integer");
  }
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((entry) => typeof entry === "string");
}

function stringField(value: Record<string, unknown>, field: string): string {
  const result = value[field];
  if (typeof result !== "string") throw new IndexedDBProtocolError(`${field} must be a string`);
  return result;
}

function integerField(value: Record<string, unknown>, field: string): number {
  const result = value[field];
  if (!Number.isSafeInteger(result) || (result as number) < 0) {
    throw new IndexedDBProtocolError(`${field} must be a non-negative safe integer`);
  }
  return result as number;
}

function booleanField(value: Record<string, unknown>, field: string): boolean {
  const result = value[field];
  if (typeof result !== "boolean") throw new IndexedDBProtocolError(`${field} must be a boolean`);
  return result;
}

function emit<Target extends EventTarget, EventType extends Event>(
  target: Target,
  event: EventType,
  handler: Handler<Target, EventType>,
): void {
  target.dispatchEvent(event);
  handler?.call(target, event);
}

function domError(name: string, message = name): DOMException {
  return new DOMException(message, name);
}

function asDOMException(error: unknown): DOMException {
  if (error instanceof DOMException) return error;
  return domError("UnknownError", error instanceof Error ? error.message : String(error));
}

export class IndexedDBProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "IndexedDBProtocolError";
  }
}

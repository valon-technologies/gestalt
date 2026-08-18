import { IDBFactory as BackendFactory, IDBKeyRange as BackendKeyRange } from "fake-indexeddb";

import {
  decodeIDBKey,
  decodeStructuredClone,
  encodeIDBKey,
  encodeStructuredClone,
} from "../src/indexeddb-wire.ts";

type Command = Record<string, any>;

interface Session {
  transaction: IDBTransaction;
  database: IDBDatabase;
  databaseName: string;
  scope: string[];
  nextSequence: number;
  cursors: Map<string, IDBCursor>;
  keepAlive: boolean;
  keepAliveStore?: string;
  pending: Array<{
    run: () => Promise<unknown>;
    resolve: (value: unknown) => void;
    reject: (error: unknown) => void;
  }>;
  upgradeRequest?: IDBOpenDBRequest;
}

const hiddenKeepAliveStore = "__gestalt_remote_keepalive__";
const storedWireMarker = "__gestalt_indexeddb_wire_v1__";

export class IndexedDBCoordinator {
  readonly commands: Command[] = [];
  readonly receipts = new Map<string, "committed" | "aborted">();
  loseNextCommitAcknowledgement = false;

  private readonly factory = new BackendFactory();
  private readonly sessions = new Map<string, Session>();
  private readonly databases = new Map<string, IDBDatabase>();
  private nextTransaction = 1;
  private nextCursor = 1;

  async initialize(): Promise<void> {
    const request = this.factory.open("gestalt-indexeddb", 1);
    request.onupgradeneeded = () => {
      const database = request.result;
      const accounts = database.createObjectStore("accounts", { keyPath: "id" });
      accounts.createIndex("byBalance", "balance");
      accounts.add({ id: "from", balance: 100, owner: "Ada" });
      accounts.add({ id: "to", balance: 10, owner: "Grace" });
      const tasks = database.createObjectStore("tasks", { keyPath: "id", autoIncrement: false });
      tasks.createIndex("byStatus", "status", { unique: false });
      tasks.createIndex("byOwner", "owner", { unique: false });
      for (const task of [
        { id: "task-1", status: "queued", owner: "Ada" },
        { id: "task-2", status: "queued", owner: "Ada" },
        { id: "task-3", status: "queued", owner: "Grace" },
        { id: "task-4", status: "running", owner: "Ada" },
      ]) tasks.add(task);
    };
    this.databases.set("gestalt-indexeddb", await requestResult(request));
  }

  async invoke(raw: unknown): Promise<unknown> {
    const command = structuredClone(raw) as Command;
    this.commands.push(command);
    if (command.op === "factoryOpen") return await this.openDatabase(command);
    if (command.op === "factoryDelete") return await this.deleteDatabase(command);
    if (command.op === "factoryDatabases") return await this.listDatabases();
    if (command.op === "begin") return await this.begin(command);
    if (command.op === "status") {
      const outcome = this.receipts.get(command.nonce) ??
        (this.sessions.has(command.transactionId) ? "active" : "unknown");
      return {
        op: "status",
        transactionId: command.transactionId,
        nonce: command.nonce,
        outcome,
      };
    }

    const session = this.sessions.get(command.transactionId);
    if (!session) throw new DOMException(command.transactionId, "InvalidStateError");
    if (command.sequence !== session.nextSequence) {
      throw new DOMException(
        `expected sequence ${session.nextSequence}, received ${command.sequence}`,
        "InvalidStateError",
      );
    }

    if (command.op === "request") {
      const result = await this.runActive(session, async () => await this.execute(session, command.request));
      session.nextSequence += 1;
      return {
        op: "result",
        transactionId: command.transactionId,
        sequence: command.sequence,
        result,
      };
    }
    if (command.op === "commit") return await this.commit(session, command);
    return await this.abort(session, command);
  }

  private async openDatabase(command: Command): Promise<unknown> {
    const name = String(command.name);
    const version = command.version as number | undefined;
    const cached = this.databases.get(name);
    if (cached && version !== undefined && version > cached.version) {
      cached.close();
      this.databases.delete(name);
    }

    const request = version === undefined ? this.factory.open(name) : this.factory.open(name, version);
    return await new Promise((resolve, reject) => {
      let upgrading = false;
      request.onupgradeneeded = (event) => {
        upgrading = true;
        const database = request.result;
        const transaction = request.transaction!;
        if (!database.objectStoreNames.contains(hiddenKeepAliveStore)) {
          database.createObjectStore(hiddenKeepAliveStore);
        }
        const transactionId = this.nextTransactionId();
        const scope = [...database.objectStoreNames].filter((store) => store !== hiddenKeepAliveStore);
        const session: Session = {
          transaction,
          database,
          databaseName: name,
          scope,
          nextSequence: 1,
          cursors: new Map(),
          keepAlive: true,
          keepAliveStore: hiddenKeepAliveStore,
          pending: [],
          upgradeRequest: request,
        };
        this.sessions.set(transactionId, session);
        keepTransactionAlive(session);
        resolve({
          op: "upgrade",
          name,
          oldVersion: event.oldVersion,
          newVersion: event.newVersion,
          objectStores: databaseDefinitions(database, transaction),
          transactionId,
          nextSequence: 1,
        });
      };
      request.onsuccess = () => {
        if (upgrading) return;
        this.databases.set(name, request.result);
        resolve({
          op: "database",
          name,
          version: request.result.version,
          objectStores: databaseDefinitions(request.result),
        });
      };
      request.onerror = () => reject(request.error);
    });
  }

  private async deleteDatabase(command: Command): Promise<unknown> {
    const name = String(command.name);
    const cached = this.databases.get(name);
    const oldVersion = cached?.version ?? 0;
    cached?.close();
    this.databases.delete(name);
    await requestResult(this.factory.deleteDatabase(name));
    return { op: "deleted", name, oldVersion };
  }

  private async listDatabases(): Promise<unknown> {
    const databases = await this.factory.databases();
    return {
      op: "databases",
      databases: databases.map(({ name, version }) => ({ name: name ?? "", version: version ?? 0 })),
    };
  }

  private async begin(command: Command): Promise<unknown> {
    const databaseName = String(command.database ?? "gestalt-indexeddb");
    const current = this.databases.get(databaseName);
    if (!current) throw new DOMException(databaseName, "NotFoundError");
    const transactionId = this.nextTransactionId();
    let database = current;
    let transaction: IDBTransaction;
    let upgradeRequest: IDBOpenDBRequest | undefined;

    if (command.mode === "versionchange") {
      current.close();
      upgradeRequest = this.factory.open(databaseName, current.version + 1);
      const opened = await upgradeTransaction(upgradeRequest);
      database = opened.database;
      transaction = opened.transaction;
    } else {
      transaction = database.transaction(command.scope, command.mode, {
        durability: command.durability,
      });
    }

    const session: Session = {
      transaction,
      database,
      databaseName,
      scope: command.scope,
      nextSequence: 1,
      cursors: new Map(),
      keepAlive: true,
      pending: [],
      ...(upgradeRequest ? { upgradeRequest } : {}),
    };
    this.sessions.set(transactionId, session);
    keepTransactionAlive(session);
    return { op: "opened", transactionId, nextSequence: 1 };
  }

  private async commit(session: Session, command: Command): Promise<unknown> {
    await this.runActive(session, async () => {
      if (session.keepAliveStore && session.database.objectStoreNames.contains(session.keepAliveStore)) {
        session.database.deleteObjectStore(session.keepAliveStore);
      }
      session.keepAlive = false;
      const completed = transactionFinished(session.transaction);
      session.transaction.commit();
      await completed;
    });
    this.sessions.delete(command.transactionId);
    this.receipts.set(command.nonce, "committed");
    if (session.upgradeRequest) {
      const database = await requestResult(session.upgradeRequest);
      this.databases.set(session.databaseName, database);
    }
    const response = {
      op: "committed",
      transactionId: command.transactionId,
      nonce: command.nonce,
    };
    if (this.loseNextCommitAcknowledgement) {
      this.loseNextCommitAcknowledgement = false;
      throw new Error("simulated response loss after durable commit");
    }
    return response;
  }

  private async abort(session: Session, command: Command): Promise<unknown> {
    await this.runActive(session, async () => {
      session.keepAlive = false;
      const aborted = transactionAborted(session.transaction);
      session.transaction.abort();
      await aborted;
    });
    this.sessions.delete(command.transactionId);
    return { op: "aborted", transactionId: command.transactionId };
  }

  private async execute(session: Session, request: Command): Promise<unknown> {
    if (request.kind === "createObjectStore") {
      session.database.createObjectStore(request.store, {
        keyPath: request.keyPath,
        autoIncrement: request.autoIncrement,
      });
      session.scope.push(request.store);
      return null;
    }
    if (request.kind === "deleteObjectStore") {
      session.database.deleteObjectStore(request.store);
      session.scope = session.scope.filter((store) => store !== request.store);
      return null;
    }
    if (request.kind === "renameObjectStore") {
      session.transaction.objectStore(request.store).name = request.name;
      session.scope = session.scope.map((store) => store === request.store ? request.name : store);
      return null;
    }
    if (
      request.kind === "cursorContinue" ||
      request.kind === "cursorAdvance" ||
      request.kind === "cursorContinuePrimaryKey"
    ) {
      return await this.continueCursor(session, request);
    }
    if (request.kind === "cursorUpdate") {
      const cursor = this.cursor(session, request.cursorId);
      return encodeIDBKey(await requestResult(cursor.update(valueForStorage(cursorStore(cursor), request.value))));
    }
    if (request.kind === "cursorDelete") {
      const cursor = this.cursor(session, request.cursorId);
      const source = cursor.source;
      const store = "objectStore" in source ? source.objectStore : source;
      await requestResult(store.delete(cursor.primaryKey));
      return null;
    }

    const store = session.transaction.objectStore(request.store);
    if (request.kind === "createIndex") {
      store.createIndex(request.index, request.keyPath, {
        multiEntry: request.multiEntry,
        unique: request.unique,
      });
      return null;
    }
    if (request.kind === "deleteIndex") {
      store.deleteIndex(request.index);
      return null;
    }
    if (request.kind === "renameIndex") {
      store.index(request.index).name = request.name;
      return null;
    }

    const source = request.index === undefined ? store : store.index(request.index);
    const query = deserializeQuery(request.query);
    switch (request.kind) {
      case "add":
      case "put": {
        const operation = request.kind === "add" ? store.add.bind(store) : store.put.bind(store);
        const value = valueForStorage(store, request.value);
        const result = request.key === undefined
          ? await requestResult(operation(value))
          : await requestResult(operation(value, decodeIDBKey(request.key)));
        return encodeIDBKey(result);
      }
      case "delete":
        await requestResult(store.delete(query));
        return null;
      case "clear":
        await requestResult(store.clear());
        return null;
      case "get":
      case "indexGet":
        return await optionalValue(await requestResult(source.get(query)));
      case "getKey":
      case "indexGetKey":
        return optionalKey(await requestResult(source.getKey(query)));
      case "getAll":
      case "indexGetAll":
        return await encodeValues(await collect(source, query, request.direction, request.count, "value"));
      case "getAllKeys":
      case "indexGetAllKeys":
        return (await collect(source, query, request.direction, request.count, "primaryKey")).map(encodeIDBKey);
      case "getAllRecords":
      case "indexGetAllRecords":
        return await encodeRecords(await collectRecords(source, query, request.direction, request.count));
      case "count":
      case "indexCount":
        return await requestResult(source.count(query));
      case "openCursor": {
        const cursorRequest = (
          request.keyOnly
            ? source.openKeyCursor(query, request.direction)
            : source.openCursor(query, request.direction)
        ) as IDBRequest<IDBCursor | null>;
        const cursor = await requestResult(cursorRequest);
        return cursor ? await this.rememberCursor(session, cursor, request.keyOnly) : null;
      }
      default:
        throw new DOMException(request.kind, "NotSupportedError");
    }
  }

  private runActive(session: Session, run: () => Promise<unknown>): Promise<unknown> {
    return new Promise((resolve, reject) => {
      session.pending.push({ run, resolve, reject });
    });
  }

  private async rememberCursor(session: Session, cursor: IDBCursor, keyOnly: boolean): Promise<unknown> {
    const cursorId = `cursor-${this.nextCursor}`;
    this.nextCursor += 1;
    session.cursors.set(cursorId, cursor);
    return await cursorResult(cursorId, cursor, keyOnly);
  }

  private async continueCursor(session: Session, request: Command): Promise<unknown> {
    const cursor = this.cursor(session, request.cursorId);
    const advanced = new Promise<IDBCursor | null>((resolve, reject) => {
      const cursorRequest = cursor.request;
      cursorRequest.onsuccess = () => resolve(cursorRequest.result as IDBCursor | null);
      cursorRequest.onerror = () => reject(cursorRequest.error);
      if (request.kind === "cursorAdvance") cursor.advance(request.count);
      else if (request.kind === "cursorContinuePrimaryKey") {
        cursor.continuePrimaryKey(decodeIDBKey(request.key), decodeIDBKey(request.primaryKey));
      } else if (request.key === undefined) cursor.continue();
      else cursor.continue(decodeIDBKey(request.key));
    });
    const next = await advanced;
    if (!next) {
      session.cursors.delete(request.cursorId);
      return null;
    }
    session.cursors.set(request.cursorId, next);
    return await cursorResult(request.cursorId, next, !("value" in next));
  }

  private cursor(session: Session, cursorId: string): IDBCursor {
    const cursor = session.cursors.get(cursorId);
    if (!cursor) throw new DOMException(cursorId, "InvalidStateError");
    return cursor;
  }

  private nextTransactionId(): string {
    const result = `tx-${this.nextTransaction}`;
    this.nextTransaction += 1;
    return result;
  }
}

function keepTransactionAlive(session: Session): void {
  const storeName = session.keepAliveStore ?? session.scope[0];
  if (!storeName) return;
  const pump = (): void => {
    if (!session.keepAlive) return;
    let request: IDBRequest;
    try {
      request = session.transaction.objectStore(storeName).get("__gestalt_keepalive__");
    } catch {
      return;
    }
    request.onsuccess = pump;
    request.onerror = pump;
    const pending = session.pending.shift();
    if (pending) pending.run().then(pending.resolve, pending.reject);
  };
  pump();
}

function deserializeQuery(value: any): any {
  if (value === null || value === undefined) return undefined;
  if (value.type === "key") return decodeIDBKey(value.key);
  const lower = value.lower === null ? undefined : decodeIDBKey(value.lower);
  const upper = value.upper === null ? undefined : decodeIDBKey(value.upper);
  if (lower === undefined) return BackendKeyRange.upperBound(upper, value.upperOpen);
  if (upper === undefined) return BackendKeyRange.lowerBound(lower, value.lowerOpen);
  return BackendKeyRange.bound(lower, upper, value.lowerOpen, value.upperOpen);
}

async function optionalValue(value: unknown): Promise<unknown> {
  return value === undefined ? { present: false } : { present: true, value: await wireForStoredValue(value) };
}

function optionalKey(value: IDBValidKey | undefined): unknown {
  return value === undefined ? { present: false } : { present: true, value: encodeIDBKey(value) };
}

async function cursorResult(cursorId: string, cursor: IDBCursor, keyOnly: boolean): Promise<unknown> {
  return {
    cursorId,
    key: encodeIDBKey(cursor.key),
    primaryKey: encodeIDBKey(cursor.primaryKey),
    ...(keyOnly ? {} : { value: await wireForStoredValue((cursor as IDBCursorWithValue).value) }),
  };
}

async function encodeValues(values: unknown[]): Promise<unknown[]> {
  return await Promise.all(values.map(wireForStoredValue));
}

async function encodeRecords(records: Array<{ key: IDBValidKey; primaryKey: IDBValidKey; value: unknown }>): Promise<unknown[]> {
  return await Promise.all(records.map(async (record) => ({
    key: encodeIDBKey(record.key),
    primaryKey: encodeIDBKey(record.primaryKey),
    value: await wireForStoredValue(record.value),
  })));
}

function valueForStorage(store: IDBObjectStore, wire: unknown): unknown {
  if (store.keyPath === null && store.indexNames.length === 0) {
    return { [storedWireMarker]: wire };
  }
  return decodeStructuredClone(wire as any);
}

async function wireForStoredValue(value: unknown): Promise<unknown> {
  if (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    Object.keys(value).length === 1 &&
    Object.hasOwn(value, storedWireMarker)
  ) {
    return (value as Record<string, unknown>)[storedWireMarker];
  }
  return await encodeStructuredClone(value);
}

function cursorStore(cursor: IDBCursor): IDBObjectStore {
  return "objectStore" in cursor.source ? cursor.source.objectStore : cursor.source;
}

function collect(
  source: IDBObjectStore | IDBIndex,
  query: IDBValidKey | IDBKeyRange | undefined,
  direction: IDBCursorDirection = "next",
  count: number | undefined,
  kind: "value" | "primaryKey",
): Promise<any[]> {
  return collectRecords(source, query, direction, count).then((records) =>
    records.map((record) => kind === "value" ? record.value : record.primaryKey)
  );
}

function collectRecords(
  source: IDBObjectStore | IDBIndex,
  query: IDBValidKey | IDBKeyRange | undefined,
  direction: IDBCursorDirection = "next",
  count: number | undefined,
): Promise<Array<{ key: IDBValidKey; primaryKey: IDBValidKey; value: unknown }>> {
  return new Promise((resolve, reject) => {
    const records: Array<{ key: IDBValidKey; primaryKey: IDBValidKey; value: unknown }> = [];
    const request = source.openCursor(query, direction);
    request.onerror = (event) => {
      event.preventDefault();
      reject(request.error);
    };
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor || (count !== undefined && records.length >= count)) {
        resolve(records);
        return;
      }
      records.push({ key: cursor.key, primaryKey: cursor.primaryKey, value: cursor.value });
      if (count !== undefined && records.length >= count) resolve(records);
      else cursor.continue();
    };
  });
}

function databaseDefinitions(
  database: IDBDatabase,
  upgradeTransaction?: IDBTransaction,
): Array<Record<string, unknown>> {
  const names = [...database.objectStoreNames].filter((name) => name !== hiddenKeepAliveStore);
  if (names.length === 0) return [];
  const transaction = upgradeTransaction ?? database.transaction(names);
  return names.map((name) => {
    const store = transaction.objectStore(name);
    return {
      name: store.name,
      keyPath: store.keyPath,
      autoIncrement: store.autoIncrement,
      indexes: [...store.indexNames].map((indexName) => {
        const index = store.index(indexName);
        return {
          name: index.name,
          keyPath: index.keyPath,
          multiEntry: index.multiEntry,
          unique: index.unique,
        };
      }),
    };
  });
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = (event) => {
      event.preventDefault();
      reject(request.error);
    };
  });
}

function transactionFinished(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(transaction.error);
    transaction.onerror = () => reject(transaction.error);
  });
}

function transactionAborted(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve) => {
    transaction.onabort = () => resolve();
  });
}

function upgradeTransaction(request: IDBOpenDBRequest): Promise<{
  database: IDBDatabase;
  transaction: IDBTransaction;
}> {
  return new Promise((resolve, reject) => {
    request.onupgradeneeded = () => resolve({
      database: request.result,
      transaction: request.transaction!,
    });
    request.onerror = () => reject(request.error);
  });
}

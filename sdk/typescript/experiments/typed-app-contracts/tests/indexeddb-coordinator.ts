import { IDBFactory, IDBKeyRange as BackendKeyRange } from "fake-indexeddb";

type Command = Record<string, any>;

interface Session {
  transaction: IDBTransaction;
  database: IDBDatabase;
  scope: string[];
  nextSequence: number;
  cursors: Map<string, IDBCursor>;
  keepAlive: boolean;
  pending: Array<{
    run: () => Promise<unknown>;
    resolve: (value: unknown) => void;
    reject: (error: unknown) => void;
  }>;
  upgradeRequest?: IDBOpenDBRequest;
}

export class IndexedDBCoordinator {
  readonly commands: Command[] = [];
  readonly receipts = new Map<string, "committed" | "aborted">();
  loseNextCommitAcknowledgement = false;

  private readonly factory = new IDBFactory();
  private readonly sessions = new Map<string, Session>();
  private database!: IDBDatabase;
  private version = 1;
  private nextTransaction = 1;
  private nextCursor = 1;

  async initialize(): Promise<void> {
    const request = this.factory.open("gestalt-indexeddb", this.version);
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
    this.database = await requestResult(request);
  }

  async invoke(raw: unknown): Promise<unknown> {
    const command = structuredClone(raw) as Command;
    this.commands.push(command);
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
    if (command.op === "commit") {
      await this.runActive(session, async () => {
        session.keepAlive = false;
        const completed = transactionFinished(session.transaction);
        session.transaction.commit();
        await completed;
      });
      this.sessions.delete(command.transactionId);
      this.receipts.set(command.nonce, "committed");
      if (session.upgradeRequest) {
        this.database = await requestResult(session.upgradeRequest);
        this.version = this.database.version;
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
    await this.runActive(session, async () => {
      session.keepAlive = false;
      const aborted = transactionAborted(session.transaction);
      session.transaction.abort();
      await aborted;
    });
    this.sessions.delete(command.transactionId);
    return { op: "aborted", transactionId: command.transactionId };
  }

  private async begin(command: Command): Promise<unknown> {
    const transactionId = `tx-${this.nextTransaction}`;
    this.nextTransaction += 1;
    let database = this.database;
    let transaction: IDBTransaction;
    let upgradeRequest: IDBOpenDBRequest | undefined;

    if (command.mode === "versionchange") {
      this.database.close();
      upgradeRequest = this.factory.open("gestalt-indexeddb", this.version + 1);
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

  private async execute(session: Session, request: Command): Promise<any> {
    if (request.kind === "createObjectStore") {
      session.database.createObjectStore(request.store, {
        keyPath: request.keyPath,
        autoIncrement: request.autoIncrement,
      });
      return null;
    }
    if (request.kind === "deleteObjectStore") {
      session.database.deleteObjectStore(request.store);
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
      return jsonClone(await requestResult(cursor.update(request.value)));
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

    const source = request.index === undefined ? store : store.index(request.index);
    const query = deserializeQuery(request.query);
    switch (request.kind) {
      case "add":
      case "put": {
        const operation = request.kind === "add" ? store.add.bind(store) : store.put.bind(store);
        const result = request.key === undefined
          ? await requestResult(operation(request.value))
          : await requestResult(operation(request.value, request.key));
        return jsonClone(result);
      }
      case "delete":
        await requestResult(store.delete(query));
        return null;
      case "clear":
        await requestResult(store.clear());
        return null;
      case "get":
      case "indexGet":
        return optional(await requestResult(source.get(query)));
      case "getKey":
      case "indexGetKey":
        return optional(await requestResult(source.getKey(query)));
      case "getAll":
      case "indexGetAll":
        return jsonClone(await requestResult(source.getAll(query, request.count)));
      case "getAllKeys":
      case "indexGetAllKeys":
        return jsonClone(await requestResult(source.getAllKeys(query, request.count)));
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
        return cursor ? this.rememberCursor(session, cursor, request.keyOnly) : null;
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

  private rememberCursor(session: Session, cursor: IDBCursor, keyOnly: boolean): unknown {
    const cursorId = `cursor-${this.nextCursor}`;
    this.nextCursor += 1;
    session.cursors.set(cursorId, cursor);
    return cursorResult(cursorId, cursor, keyOnly);
  }

  private async continueCursor(session: Session, request: Command): Promise<unknown> {
    const cursor = this.cursor(session, request.cursorId);
    const advanced = new Promise<IDBCursor | null>((resolve, reject) => {
      const cursorRequest = cursor.request;
      cursorRequest.onsuccess = () => resolve(cursorRequest.result as IDBCursor | null);
      cursorRequest.onerror = () => reject(cursorRequest.error);
      if (request.kind === "cursorAdvance") cursor.advance(request.count);
      else if (request.kind === "cursorContinuePrimaryKey") {
        cursor.continuePrimaryKey(request.key, request.primaryKey);
      } else if (request.key === undefined) cursor.continue();
      else cursor.continue(request.key);
    });
    const next = await advanced;
    if (!next) {
      session.cursors.delete(request.cursorId);
      return null;
    }
    session.cursors.set(request.cursorId, next);
    return cursorResult(request.cursorId, next, !("value" in next));
  }

  private cursor(session: Session, cursorId: string): IDBCursor {
    const cursor = session.cursors.get(cursorId);
    if (!cursor) throw new DOMException(cursorId, "InvalidStateError");
    return cursor;
  }
}

function keepTransactionAlive(session: Session): void {
  const storeName = session.scope[0];
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
  if (value.type === "key") return value.key;
  const lower = value.lower;
  const upper = value.upper;
  if (lower === null) return BackendKeyRange.upperBound(upper, value.upperOpen);
  if (upper === null) return BackendKeyRange.lowerBound(lower, value.lowerOpen);
  return BackendKeyRange.bound(lower, upper, value.lowerOpen, value.upperOpen);
}

function optional(value: unknown): unknown {
  return value === undefined ? { present: false } : { present: true, value: jsonClone(value) };
}

function cursorResult(cursorId: string, cursor: IDBCursor, keyOnly: boolean): unknown {
  return {
    cursorId,
    key: jsonClone(cursor.key),
    primaryKey: jsonClone(cursor.primaryKey),
    ...(keyOnly ? {} : { value: jsonClone((cursor as IDBCursorWithValue).value) }),
  };
}

function jsonClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = (event) => {
      // The remote facade must decide whether the public request error was handled.
      // Keep the backend transaction alive until the facade sends commit or abort.
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

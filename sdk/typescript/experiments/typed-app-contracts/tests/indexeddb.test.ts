import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";

import {
  IDBKeyRange,
  connectRemoteIDBDatabase,
  createRemoteIDBFactory,
  type RemoteObjectStoreDefinition,
} from "../src/indexeddb.ts";
import { FilesystemRegistry, addDependency, initializeProject, readProjectManifest } from "../src/registry.ts";
import { InstallationManager, type InvocationRoute } from "../src/runtime.ts";
import { linkZod, manifest, sdkPath } from "./helpers.ts";
import { IndexedDBCoordinator } from "./indexeddb-coordinator.ts";

const indexedDBPath = join(import.meta.dir, "..", "src", "indexeddb.ts");

describe("literal IDBDatabase facade over unary tools", () => {
  test("R-IDB-01 implements the IDBDatabase properties, transaction return, close, and events", async () => {
    const coordinator = new IndexedDBCoordinator();
    await coordinator.initialize();
    const connection = connectRemoteIDBDatabase(coordinatorInvoke(coordinator), {
      name: "gestalt-indexeddb",
      version: 1,
      objectStores: definitions(),
    });
    const database: IDBDatabase = connection.database;

    expect(database).toBeInstanceOf(EventTarget);
    expect(database.name).toBe("gestalt-indexeddb");
    expect(database.version).toBe(1);
    expect([...database.objectStoreNames]).toEqual(["accounts", "tasks"]);
    expect(database.objectStoreNames.contains("accounts")).toBe(true);
    expect(database.objectStoreNames.item(10)).toBeNull();
    expectDOMException(() => database.transaction([]), "InvalidAccessError");
    expectDOMException(() => database.transaction("missing"), "NotFoundError");
    expectDOMException(() => database.createObjectStore("outside-upgrade"), "InvalidStateError");

    const transaction = database.transaction("accounts");
    expect(transaction).toBeInstanceOf(EventTarget);
    expect(transaction.mode).toBe("readonly");
    expect(transaction.durability).toBe("default");
    expect(transaction.objectStore("accounts").transaction).toBe(transaction);
    const aborted = eventOnce(transaction, "abort");
    transaction.abort();
    await aborted;

    let versionChange: IDBVersionChangeEvent | undefined;
    database.onversionchange = (event) => { versionChange = event; };
    connection.versionChange(1, 2);
    expect(versionChange?.oldVersion).toBe(1);
    expect(versionChange?.newVersion).toBe(2);

    let closed = false;
    database.onclose = () => { closed = true; };
    connection.forceClose();
    expect(closed).toBe(true);
    expectDOMException(() => database.transaction("accounts"), "InvalidStateError");
    expect(database.name).toBe("gestalt-indexeddb");
    expect(database.version).toBe(1);
  });

  test("R-IDB-02 preserves literal IDBRequest and transaction semantics across replicas", async () => {
    const fixture = await publishIndexedDBGraph();
    expect(Object.keys(fixture.datastore.contract.tools)).toEqual(["transaction"]);
    expect(await fixture.installation.invoke("transfer", { amount: 25 })).toEqual({
      fromBalance: 75,
      toBalance: 35,
      requestState: "done",
      transactionMode: "readwrite",
    });
    const replicas = fixture.routes
      .filter((route) => route.app === "acme/indexeddb")
      .map((route) => route.replica);
    expect(new Set(replicas)).toEqual(new Set([0, 1, 2]));
    expect(replicas.length).toBeGreaterThanOrEqual(6);
  });

  test("R-IDB-03 supports index cursors, key ranges, continue, and advance without changing the API", async () => {
    const fixture = await publishIndexedDBGraph();
    expect(await fixture.installation.invoke("scanQueued", {})).toEqual({
      continued: ["task-1", "task-2", "task-3"],
      advanced: ["task-1", "task-3"],
    });
    const cursorCommands = fixture.coordinator.commands.filter(
      (command) => command.op === "request" && String(command.request?.kind).startsWith("cursor"),
    );
    expect(cursorCommands.length).toBeGreaterThanOrEqual(5);
  });

  test("R-IDB-04 supports upgrade-only object-store and index creation through the literal methods", async () => {
    const coordinator = new IndexedDBCoordinator();
    await coordinator.initialize();
    const connection = connectRemoteIDBDatabase(coordinatorInvoke(coordinator), {
      name: "gestalt-indexeddb",
      version: 2,
      objectStores: definitions(),
      upgrade: true,
    });
    const transaction = connection.upgradeTransaction!;
    const completed = eventOnce(transaction, "complete");
    const archive = connection.database.createObjectStore("archive", { keyPath: "id" });
    archive.createIndex("byOwner", "owner", { unique: false });
    archive.createIndex("temporaryIndex", "owner");
    archive.deleteIndex("temporaryIndex");
    connection.database.createObjectStore("temporary");
    connection.database.deleteObjectStore("temporary");
    expect([...connection.database.objectStoreNames]).toEqual(["accounts", "archive", "tasks"]);
    expect([...archive.indexNames]).toEqual(["byOwner"]);
    expect(archive.indexNames.contains("temporaryIndex")).toBe(false);
    transaction.commit();
    await completed;

    const reopened = connectRemoteIDBDatabase(coordinatorInvoke(coordinator), {
      name: "gestalt-indexeddb",
      version: 2,
      objectStores: [
        ...definitions(),
        { name: "archive", keyPath: "id", indexes: [{ name: "byOwner", keyPath: "owner" }] },
      ],
    }).database;
    const write = reopened.transaction("archive", "readwrite", { durability: "strict" });
    const done = eventOnce(write, "complete");
    const key = await requestValue(write.objectStore("archive").add({ id: "a-1", owner: "Ada" }));
    expect(key).toBe("a-1");
    write.commit();
    await done;
  });

  test("R-IDB-05 rolls back aborts and surfaces DOMException-compatible failures", async () => {
    const fixture = await publishIndexedDBGraph();
    expect(await fixture.installation.invoke("abortTransfer", { amount: 25 })).toEqual({
      afterAbort: 100,
      abortEvent: true,
    });
    expect(await fixture.installation.invoke("duplicateTask", {})).toEqual({
      errorName: "ConstraintError",
      abortEvent: true,
      databaseAbortEvent: true,
      databaseErrorEvent: true,
    });
  });

  test("R-IDB-06 supports the store, index, key-range, and mutable cursor objects returned by IDBDatabase", async () => {
    const coordinator = new IndexedDBCoordinator();
    await coordinator.initialize();
    const database = connectRemoteIDBDatabase(coordinatorInvoke(coordinator), {
      name: "gestalt-indexeddb",
      version: 1,
      objectStores: definitions(),
    }).database;

    const write = database.transaction("tasks", "readwrite");
    const writeDone = eventOnce(write, "complete");
    const store = write.objectStore("tasks");
    expect(store.name).toBe("tasks");
    expect(store.keyPath).toBe("id");
    expect(store.autoIncrement).toBe(false);
    expect(store.indexNames.contains("byStatus")).toBe(true);
    expect(store.indexNames.item(0)).toBe("byOwner");
    expect(await requestValue(store.add({ id: "task-5", status: "queued", owner: "Lin" }))).toBe("task-5");
    expect((await requestValue(store.get("task-5"))).owner).toBe("Lin");
    expect(await requestValue(store.put({ id: "task-5", status: "queued", owner: "Grace" }))).toBe("task-5");
    expect(await requestValue(store.getKey("task-5"))).toBe("task-5");
    expect(await requestValue(store.count())).toBe(5);
    expect((await requestValue(store.getAll(null, 2))).map((value) => value.id)).toEqual(["task-1", "task-2"]);
    expect(await requestValue(store.getAllKeys(IDBKeyRange.lowerBound("task-4")))).toEqual([
      "task-4", "task-5",
    ]);

    const byStatus = store.index("byStatus");
    expect(byStatus.name).toBe("byStatus");
    expect(byStatus.objectStore).toBe(store);
    expect(byStatus.keyPath).toBe("status");
    expect(byStatus.multiEntry).toBe(false);
    expect(byStatus.unique).toBe(false);
    expect((await requestValue(byStatus.get("queued"))).id).toBe("task-1");
    expect(await requestValue(byStatus.getKey("queued"))).toBe("task-1");
    expect(await requestValue(byStatus.count("queued"))).toBe(4);
    expect((await requestValue(byStatus.getAll("queued", 2))).map((value) => value.id)).toEqual([
      "task-1", "task-2",
    ]);
    expect(await requestValue(byStatus.getAllKeys("queued", 2))).toEqual(["task-1", "task-2"]);

    const cursorRequest = byStatus.openCursor("queued");
    const cursor = await requestValue(cursorRequest);
    expect(cursor?.primaryKey).toBe("task-1");
    expect(await requestValue(cursor!.update({ id: "task-1", status: "running", owner: "Ada" }))).toBe("task-1");

    const continuedRequest = byStatus.openCursor("queued");
    const continued = await requestValue(continuedRequest);
    expect(continued?.primaryKey).toBe("task-2");
    continued!.continuePrimaryKey("queued", "task-3");
    expect((await requestValue(continuedRequest))?.primaryKey).toBe("task-3");

    const keyCursor = await requestValue(byStatus.openKeyCursor("queued"));
    expect(keyCursor?.primaryKey).toBe("task-2");
    await requestValue(keyCursor!.delete());
    expect(await requestValue(store.count())).toBe(4);

    expect((await requestValue(store.openCursor("task-5")))?.value.owner).toBe("Grace");
    expect((await requestValue(store.openKeyCursor("task-5")))?.primaryKey).toBe("task-5");
    expect(IDBKeyRange.only("task-3").includes("task-3")).toBe(true);
    expect(IDBKeyRange.upperBound("task-3").includes("task-4")).toBe(false);
    expect(IDBKeyRange.bound("task-2", "task-4", true).includes("task-2")).toBe(false);
    expect(IDBKeyRange.bound("task-2", "task-4", true).includes("task-3")).toBe(true);

    await requestValue(store.add({ id: "temporary", status: "queued", owner: "Temp" }));
    await requestValue(store.delete("temporary"));
    expect(await requestValue(store.get("temporary"))).toBeUndefined();
    write.commit();
    await writeDone;

    const rollback = database.transaction("tasks", "readwrite");
    const rollbackDone = eventOnce(rollback, "abort");
    await requestValue(rollback.objectStore("tasks").clear());
    rollback.abort();
    await rollbackDone;
    const verify = database.transaction("tasks");
    expect(await requestValue(verify.objectStore("tasks").count())).toBe(4);
  });

  test("R-IDB-07 implements IDBFactory opening, upgrades, discovery, comparison, and deletion", async () => {
    const coordinator = new IndexedDBCoordinator();
    await coordinator.initialize();
    const factory = createRemoteIDBFactory(coordinatorInvoke(coordinator));

    expect(factory.cmp(-Infinity, new Date(0))).toBe(-1);
    expect(factory.cmp(new Date(0), "a")).toBe(-1);
    expect(factory.cmp("a", new Uint8Array([0]))).toBe(-1);
    expect(factory.cmp(new Uint8Array([0]), [])).toBe(-1);
    expect(factory.cmp(new Uint8Array([1, 2]), new Uint8Array([1, 3]))).toBe(-1);
    expect(factory.cmp([1, "a"], [1, "a"])).toBe(0);
    expectDOMException(() => factory.cmp(false, true), "DataError");
    expectDOMException(() => factory.cmp(NaN, 1), "DataError");
    expect(() => factory.open("invalid", 0)).toThrow(TypeError);

    const existing = await requestValue(factory.open("gestalt-indexeddb"));
    expect(existing.name).toBe("gestalt-indexeddb");
    expect(existing.version).toBe(1);

    const created = await openDatabase(factory, "factory-created", 1, (database) => {
      const draft = database.createObjectStore("draft", { keyPath: "id" });
      draft.name = "records";
      const temporary = draft.createIndex("temporary", "owner");
      temporary.name = "byOwner";
    });
    expect(created.version).toBe(1);
    expect([...created.objectStoreNames]).toEqual(["records"]);
    const metadata = created.transaction("records").objectStore("records");
    expect([...metadata.indexNames]).toEqual(["byOwner"]);

    const upgraded = await openDatabase(factory, "factory-created", 2, (_database, transaction) => {
      const records = transaction.objectStore("records");
      records.name = "renamedRecords";
      records.index("byOwner").name = "renamedOwner";
    });
    expect(upgraded.version).toBe(2);
    expect([...upgraded.objectStoreNames]).toEqual(["renamedRecords"]);
    expect([...upgraded.transaction("renamedRecords").objectStore("renamedRecords").indexNames]).toEqual([
      "renamedOwner",
    ]);

    const listed = await factory.databases();
    expect(listed).toContainEqual({ name: "factory-created", version: 2 });
    await requestValue(factory.deleteDatabase("factory-created"));
    expect(await factory.databases()).not.toContainEqual({ name: "factory-created", version: 2 });

    const fixture = await publishIndexedDBGraph();
    expect(await fixture.installation.invoke("factoryLifecycle", {})).toEqual({
      name: "consumer-factory",
      upgradeSeen: true,
      cyclePreserved: true,
      mapValue: "mapped",
      binaryValue: 3,
      listed: true,
      deleted: true,
      comparison: -1,
    });
    const factoryRoutes = fixture.routes.filter((route) => route.app === "acme/indexeddb");
    expect(new Set(factoryRoutes.map((route) => route.replica))).toEqual(new Set([0, 1, 2]));
  });

  test("R-IDB-08 transports structured-clone graphs and the complete IndexedDB key domain", async () => {
    const coordinator = new IndexedDBCoordinator();
    await coordinator.initialize();
    const factory = createRemoteIDBFactory(coordinatorInvoke(coordinator));
    const database = await openDatabase(factory, "wire-values", 1, (connection) => {
      connection.createObjectStore("values");
    });

    const shared = { label: "shared" };
    const buffer = new Uint8Array([1, 2, 3, 4]).buffer;
    const sparse = new Array(2);
    sparse[1] = "present";
    const graph: any = {
      undefined,
      nan: NaN,
      positiveInfinity: Infinity,
      negativeZero: -0,
      bigint: 9_007_199_254_740_993n,
      date: new Date("2026-08-17T12:00:00.000Z"),
      invalidDate: new Date(NaN),
      regexp: /gestalt/gi,
      map: new Map([[shared, new Set(["one", "two"])]]),
      sharedA: shared,
      sharedB: shared,
      buffer,
      view: new Uint8Array(buffer, 1, 2),
      dataView: new DataView(buffer, 0, 2),
      blob: new Blob(["blob-value"], { type: "text/plain" }),
      file: new File(["file-value"], "value.txt", { type: "text/plain", lastModified: 42 }),
      error: new TypeError("failed", { cause: shared }),
      domException: new DOMException("missing", "NotFoundError"),
      boxedBigInt: Object(7n),
      sparse,
    };
    graph.self = graph;

    const write = database.transaction("values", "readwrite");
    const writeDone = eventOnce(write, "complete");
    await requestValue(write.objectStore("values").put(graph, "graph"));
    for (const [key, value] of [
      [-Infinity, "negative infinity"],
      [Infinity, "positive infinity"],
      [new Date(0), "date"],
      ["string", "string"],
      [new Uint8Array([1, 2, 3]), "binary"],
      [[1, new Date(1), new Uint8Array([2])], "array"],
    ] as Array<[IDBValidKey, string]>) {
      await requestValue(write.objectStore("values").put(value, key));
    }
    write.commit();
    await writeDone;

    const read = database.transaction("values");
    const result = await requestValue(read.objectStore("values").get("graph"));
    expect(result.self).toBe(result);
    expect(result.sharedA).toBe(result.sharedB);
    expect([...result.map.keys()][0]).toBe(result.sharedA);
    expect([...result.map.values()][0]).toEqual(new Set(["one", "two"]));
    expect(result.undefined).toBeUndefined();
    expect(Number.isNaN(result.nan)).toBe(true);
    expect(result.positiveInfinity).toBe(Infinity);
    expect(Object.is(result.negativeZero, -0)).toBe(true);
    expect(result.bigint).toBe(9_007_199_254_740_993n);
    expect(result.date).toEqual(new Date("2026-08-17T12:00:00.000Z"));
    expect(Number.isNaN(result.invalidDate.valueOf())).toBe(true);
    expect(result.regexp).toEqual(/gestalt/gi);
    expect(result.view).toBeInstanceOf(Uint8Array);
    expect(result.view.buffer).toBe(result.buffer);
    expect([...result.view]).toEqual([2, 3]);
    expect(result.dataView.buffer).toBe(result.buffer);
    expect(result.dataView.getUint16(0)).toBe(258);
    expect(await result.blob.text()).toBe("blob-value");
    expect(result.file.name).toBe("value.txt");
    expect(result.file.lastModified).toBe(42);
    expect(await result.file.text()).toBe("file-value");
    expect(result.error).toBeInstanceOf(TypeError);
    expect(result.error.cause).toBe(result.sharedA);
    expect(result.domException).toMatchObject({ name: "NotFoundError", message: "missing" });
    expect(result.boxedBigInt.valueOf()).toBe(7n);
    expect(0 in result.sparse).toBe(false);
    expect(result.sparse[1]).toBe("present");
    expect(await requestValue(read.objectStore("values").get(new Uint8Array([1, 2, 3])))).toBe("binary");
    expect(await requestValue(read.objectStore("values").get(new Date(0)))).toBe("date");

    const rejected = database.transaction("values", "readwrite");
    await expect(requestValue(rejected.objectStore("values").put({ method() {} }, "invalid"))).rejects.toMatchObject({
      name: "DataCloneError",
    });
  });

  test("R-IDB-09 supports getAll options and record snapshots on stores and indexes", async () => {
    const coordinator = new IndexedDBCoordinator();
    await coordinator.initialize();
    const factory = createRemoteIDBFactory(coordinatorInvoke(coordinator));
    const database = await openDatabase(factory, "record-retrieval", 1, (connection) => {
      const records = connection.createObjectStore("records", { keyPath: "id" });
      records.createIndex("byGroup", "group");
    });
    const write = database.transaction("records", "readwrite");
    const done = eventOnce(write, "complete");
    for (const record of [
      { id: 1, group: "a", value: "one" },
      { id: 2, group: "a", value: "two" },
      { id: 3, group: "b", value: "three" },
    ]) await requestValue(write.objectStore("records").put(record));
    write.commit();
    await done;

    const read = database.transaction("records");
    const store = read.objectStore("records");
    expect((await requestValue(store.getAll({ direction: "prev", count: 2 }))).map((value) => value.id)).toEqual([3, 2]);
    expect(await requestValue(store.getAllKeys({ direction: "prev", count: 2 }))).toEqual([3, 2]);
    expect(await requestValue(store.getAllRecords({ direction: "prev", count: 2 }))).toEqual([
      { key: 3, primaryKey: 3, value: { id: 3, group: "b", value: "three" } },
      { key: 2, primaryKey: 2, value: { id: 2, group: "a", value: "two" } },
    ]);
    expect(await requestValue(store.index("byGroup").getAllRecords({
      query: IDBKeyRange.only("a"),
      direction: "prev",
    }))).toEqual([
      { key: "a", primaryKey: 2, value: { id: 2, group: "a", value: "two" } },
      { key: "a", primaryKey: 1, value: { id: 1, group: "a", value: "one" } },
    ]);
  });
});

async function publishIndexedDBGraph(): Promise<{
  coordinator: IndexedDBCoordinator;
  datastore: Awaited<ReturnType<FilesystemRegistry["publish"]>>;
  installation: InstallationManager;
  routes: InvocationRoute[];
}> {
  const coordinator = new IndexedDBCoordinator();
  await coordinator.initialize();
  const coordinatorGlobal = globalThis as unknown as {
    __gestaltExperimentIndexedDBCoordinator?: (input: unknown) => Promise<unknown>;
  };
  coordinatorGlobal.__gestaltExperimentIndexedDBCoordinator = coordinatorInvoke(coordinator);

  const root = await mkdtemp(join(tmpdir(), "gestalt-indexeddb-"));
  await linkZod(root);
  const registry = new FilesystemRegistry(join(root, "registry"));
  const datastoreSource = join(root, "indexeddb.ts");
  await writeFile(datastoreSource, datastoreSourceText());
  const datastore = await registry.publish(datastoreSource, manifest("acme/indexeddb", "1.0.0"));

  const consumerProject = join(root, "consumer");
  await initializeProject(consumerProject, manifest("acme/idb-consumer", "1.0.0"));
  await addDependency({
    registry,
    projectDirectory: consumerProject,
    alias: "indexeddb",
    app: "acme/indexeddb",
    version: "1.0.0",
  });
  const consumerSource = join(consumerProject, "app.ts");
  await writeFile(consumerSource, consumerSourceText());
  await registry.publish(consumerSource, await readProjectManifest(consumerProject));

  const routes: InvocationRoute[] = [];
  const installation = new InstallationManager(registry, {
    replicasPerRelease: 3,
    onRoute: (route) => routes.push(route),
  });
  await installation.activate("acme/idb-consumer", "1.0.0");
  return { coordinator, datastore, installation, routes };
}

function coordinatorInvoke(coordinator: IndexedDBCoordinator): (input: unknown) => Promise<unknown> {
  return async (input) => {
    try {
      return await coordinator.invoke(input);
    } catch (error) {
      return {
        op: "failed",
        errorName: error instanceof DOMException ? error.name : "UnknownError",
        message: error instanceof Error ? error.message : String(error),
      };
    }
  };
}

function definitions(): RemoteObjectStoreDefinition[] {
  return [
    {
      name: "accounts",
      keyPath: "id",
      indexes: [{ name: "byBalance", keyPath: "balance" }],
    },
    {
      name: "tasks",
      keyPath: "id",
      indexes: [
        { name: "byStatus", keyPath: "status" },
        { name: "byOwner", keyPath: "owner" },
      ],
    },
  ];
}

function datastoreSourceText(): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};

    const Input = z.union([
      z.strictObject({
        op: z.literal("factoryOpen"), name: z.string(),
        version: z.number().int().positive().optional(),
      }),
      z.strictObject({ op: z.literal("factoryDelete"), name: z.string() }),
      z.strictObject({ op: z.literal("factoryDatabases") }),
      z.strictObject({
        op: z.literal("begin"), database: z.string(), scope: z.array(z.string()),
        mode: z.enum(["readonly", "readwrite", "versionchange"]),
        durability: z.enum(["default", "strict", "relaxed"]),
      }),
      z.strictObject({
        op: z.literal("request"), transactionId: z.string(),
        sequence: z.number().int().min(0), request: z.json(),
      }),
      z.strictObject({
        op: z.literal("commit"), transactionId: z.string(),
        sequence: z.number().int().min(0), nonce: z.string(),
      }),
      z.strictObject({
        op: z.literal("abort"), transactionId: z.string(), sequence: z.number().int().min(0),
      }),
      z.strictObject({ op: z.literal("status"), transactionId: z.string(), nonce: z.string() }),
    ]);
    const Output = z.union([
      z.strictObject({
        op: z.literal("database"), name: z.string(), version: z.number().int().positive(),
        objectStores: z.json(),
      }),
      z.strictObject({
        op: z.literal("upgrade"), name: z.string(), oldVersion: z.number().int().min(0),
        newVersion: z.number().int().positive(), objectStores: z.json(),
        transactionId: z.string(), nextSequence: z.number().int().min(0),
      }),
      z.strictObject({
        op: z.literal("deleted"), name: z.string(), oldVersion: z.number().int().min(0),
      }),
      z.strictObject({
        op: z.literal("databases"), databases: z.array(z.strictObject({
          name: z.string(), version: z.number().int().min(0),
        })),
      }),
      z.strictObject({
        op: z.literal("opened"), transactionId: z.string(), nextSequence: z.number().int().min(0),
      }),
      z.strictObject({
        op: z.literal("result"), transactionId: z.string(),
        sequence: z.number().int().min(0), result: z.json(),
      }),
      z.strictObject({ op: z.literal("committed"), transactionId: z.string(), nonce: z.string() }),
      z.strictObject({ op: z.literal("aborted"), transactionId: z.string() }),
      z.strictObject({
        op: z.literal("status"), transactionId: z.string(), nonce: z.string(),
        outcome: z.enum(["active", "committed", "aborted", "unknown"]),
      }),
      z.strictObject({ op: z.literal("failed"), errorName: z.string(), message: z.string() }),
    ]);

    declare global {
      var __gestaltExperimentIndexedDBCoordinator:
        | ((input: z.infer<typeof Input>) => Promise<z.infer<typeof Output>>)
        | undefined;
    }

    export default app({ tools: {
      transaction: tool({
        description: "Carry one IndexedDB protocol command.",
        input: Input,
        output: Output,
        handler: async (input) => {
          const invoke = globalThis.__gestaltExperimentIndexedDBCoordinator;
          if (!invoke) throw new Error("IndexedDB coordinator is unavailable");
          return await invoke(input);
        },
      }),
    } });
  `;
}

function consumerSourceText(): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};
    import { connectRemoteIDBDatabase, createRemoteIDBFactory, IDBKeyRange } from ${JSON.stringify(indexedDBPath)};
    import { transaction } from "@gestalt/apps/indexeddb";

    const database = connectRemoteIDBDatabase(transaction, {
      name: "gestalt-indexeddb",
      version: 1,
      objectStores: [
        { name: "accounts", keyPath: "id", indexes: [{ name: "byBalance", keyPath: "balance" }] },
        { name: "tasks", keyPath: "id", indexes: [
          { name: "byStatus", keyPath: "status" },
          { name: "byOwner", keyPath: "owner" },
        ] },
      ],
    }).database;
    const indexedDB = createRemoteIDBFactory(transaction);

    function result<T>(request: IDBRequest<T>): Promise<T> {
      return new Promise((resolve, reject) => {
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
    }

    function terminal(transaction: IDBTransaction, type: "complete" | "abort"): Promise<void> {
      return new Promise((resolve, reject) => {
        if (type === "complete") {
          transaction.oncomplete = () => resolve();
          transaction.onerror = () => reject(transaction.error);
        } else transaction.onabort = () => resolve();
      });
    }

    async function transfer(amount: number, abort: false): Promise<{
      fromBalance: number; toBalance: number; requestState: "done"; transactionMode: "readwrite";
    }>;
    async function transfer(amount: number, abort: true): Promise<{ afterAbort: number; abortEvent: boolean }>;
    async function transfer(amount: number, abort: boolean) {
      return await new Promise<any>((resolve, reject) => {
        const transaction = database.transaction("accounts", "readwrite", { durability: "strict" });
        const store = transaction.objectStore("accounts");
        const fromRequest = store.get("from");
        let from: { id: string; balance: number; owner: string };
        let to: { id: string; balance: number; owner: string };
        transaction.onerror = () => reject(transaction.error);
        transaction.oncomplete = () => resolve({
          fromBalance: from.balance - amount,
          toBalance: to.balance + amount,
          requestState: fromRequest.readyState as "done",
          transactionMode: transaction.mode as "readwrite",
        });
        transaction.onabort = () => {
          if (!abort) return reject(transaction.error);
          const read = database.transaction("accounts");
          const afterAbortRequest = read.objectStore("accounts").get("from");
          afterAbortRequest.onsuccess = () => resolve({
            afterAbort: (afterAbortRequest.result as typeof from).balance,
            abortEvent: true,
          });
          afterAbortRequest.onerror = () => reject(afterAbortRequest.error);
        };
        fromRequest.onerror = () => reject(fromRequest.error);
        fromRequest.onsuccess = () => {
          from = fromRequest.result as typeof from;
          const toRequest = store.get("to");
          toRequest.onerror = () => reject(toRequest.error);
          toRequest.onsuccess = () => {
            to = toRequest.result as typeof to;
            const debit = store.put({ ...from, balance: from.balance - amount });
            debit.onerror = () => reject(debit.error);
            debit.onsuccess = () => {
              if (abort) return transaction.abort();
              const credit = store.put({ ...to, balance: to.balance + amount });
              credit.onerror = () => reject(credit.error);
              credit.onsuccess = () => transaction.commit();
            };
          };
        };
      });
    }

    async function scan(direction: "continue" | "advance") {
      return await new Promise<string[]>((resolve, reject) => {
        const transaction = database.transaction("tasks");
        const ids: string[] = [];
        transaction.oncomplete = () => resolve(ids);
        transaction.onabort = () => reject(transaction.error);
        const request = transaction.objectStore("tasks").index("byStatus").openCursor(
          IDBKeyRange.only("queued"),
        );
        request.onerror = () => reject(request.error);
        request.onsuccess = () => {
          const cursor = request.result;
          if (!cursor) return;
          ids.push((cursor.value as { id: string }).id);
          if (direction === "advance" && ids.length === 1) cursor.advance(2);
          else cursor.continue();
        };
      });
    }

    export default app({ tools: {
      transfer: tool({
        input: z.strictObject({ amount: z.number().int().min(1) }),
        output: z.strictObject({
          fromBalance: z.number().int(), toBalance: z.number().int(),
          requestState: z.literal("done"), transactionMode: z.literal("readwrite"),
        }),
        handler: async ({ amount }) => await transfer(amount, false),
      }),
      abortTransfer: tool({
        input: z.strictObject({ amount: z.number().int().min(1) }),
        output: z.strictObject({ afterAbort: z.number().int(), abortEvent: z.boolean() }),
        handler: async ({ amount }) => await transfer(amount, true),
      }),
      scanQueued: tool({
        input: z.strictObject({}),
        output: z.strictObject({ continued: z.array(z.string()), advanced: z.array(z.string()) }),
        handler: async () => ({ continued: await scan("continue"), advanced: await scan("advance") }),
      }),
      duplicateTask: tool({
        input: z.strictObject({}),
        output: z.strictObject({
          errorName: z.string(), abortEvent: z.boolean(),
          databaseAbortEvent: z.boolean(), databaseErrorEvent: z.boolean(),
        }),
        handler: async () => {
          let databaseAbortEvent = false;
          let databaseErrorEvent = false;
          database.onabort = () => { databaseAbortEvent = true; };
          database.onerror = () => { databaseErrorEvent = true; };
          const transaction = database.transaction("tasks", "readwrite");
          const aborted = terminal(transaction, "abort");
          const request = transaction.objectStore("tasks").add({ id: "task-1", status: "queued", owner: "Ada" });
          const errorName = await new Promise<string>((resolve) => {
            request.onerror = () => resolve(request.error?.name ?? "missing");
          });
          await aborted;
          return { errorName, abortEvent: true, databaseAbortEvent, databaseErrorEvent };
        },
      }),
      factoryLifecycle: tool({
        input: z.strictObject({}),
        output: z.strictObject({
          name: z.string(), upgradeSeen: z.boolean(), cyclePreserved: z.boolean(),
          mapValue: z.string(), binaryValue: z.number().int(), listed: z.boolean(),
          deleted: z.boolean(), comparison: z.number().int(),
        }),
        handler: async () => {
          const open = indexedDB.open("consumer-factory", 1);
          let upgradeSeen = false;
          open.onupgradeneeded = () => {
            upgradeSeen = true;
            open.result.createObjectStore("values");
          };
          const opened = await result(open);
          const shared = { value: "mapped" };
          const graph: any = {
            map: new Map([["key", shared]]),
            binary: new Uint8Array([1, 2, 3]),
          };
          graph.self = graph;
          const write = opened.transaction("values", "readwrite");
          const completed = terminal(write, "complete");
          await result(write.objectStore("values").put(graph, "graph"));
          write.commit();
          await completed;
          const read = opened.transaction("values");
          const restored = await result(read.objectStore("values").get("graph"));
          const listed = (await indexedDB.databases()).some((entry) => entry.name === "consumer-factory");
          opened.close();
          await result(indexedDB.deleteDatabase("consumer-factory"));
          const deleted = !(await indexedDB.databases()).some((entry) => entry.name === "consumer-factory");
          return {
            name: opened.name,
            upgradeSeen,
            cyclePreserved: restored.self === restored,
            mapValue: restored.map.get("key").value,
            binaryValue: restored.binary[2],
            listed,
            deleted,
            comparison: indexedDB.cmp(new Date(0), "a"),
          };
        },
      }),
    } });
  `;
}

function requestValue<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function openDatabase(
  factory: IDBFactory,
  name: string,
  version: number,
  upgrade: (database: IDBDatabase, transaction: IDBTransaction) => void,
): Promise<IDBDatabase> {
  const request = factory.open(name, version);
  return new Promise((resolve, reject) => {
    request.onupgradeneeded = () => upgrade(request.result, request.transaction!);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function eventOnce(target: EventTarget, type: string): Promise<Event> {
  return new Promise((resolve) => target.addEventListener(type, resolve, { once: true }));
}

function expectDOMException(operation: () => unknown, name: string): void {
  try {
    operation();
    throw new Error(`expected ${name}`);
  } catch (error) {
    expect(error).toBeInstanceOf(DOMException);
    expect((error as DOMException).name).toBe(name);
  }
}

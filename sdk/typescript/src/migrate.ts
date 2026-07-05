import { randomUUID } from "node:crypto";

import {
  AlreadyExistsError,
  type ColumnSchema,
  ColumnType,
  type Cursor,
  type GetAllOptions,
  IndexedDB,
  type IndexSchema,
  type Key,
  type KeyRange,
  NotFoundError,
  type ObjectStore,
  type ObjectStoreSchema,
  type OpenCursorOptions,
  type Record as DbRecord,
} from "./providers/indexeddb.ts";

/**
 * Default object store used to record applied migration revisions.
 */
export const DEFAULT_LEDGER_STORE = "_gestalt_migrations";

const LOCK_STORE = "_gestalt_migration_lock";
const LEDGER_COLUMN_REVISION_ID = "revision_id";
const LEDGER_COLUMN_APPLIED_AT = "applied_at";
const LOCK_ROW_ID = "lock";
const LOCK_COLUMN_ID = "id";
const LOCK_COLUMN_HOLDER = "holder";
const LOCK_COLUMN_EXPIRES_AT = "expires_at";
const LOCK_LEASE_MS = 15 * 60_000;
const LOCK_ACQUIRE_TIMEOUT_MS = 5 * 60_000;
const LOCK_POLL_MS = 500;

const LEDGER_SCHEMA: ObjectStoreSchema = {
  columns: [
    { name: LEDGER_COLUMN_REVISION_ID, type: ColumnType.String, primaryKey: true, notNull: true },
    { name: LEDGER_COLUMN_APPLIED_AT, type: ColumnType.String, notNull: true },
  ],
};

const LOCK_SCHEMA: ObjectStoreSchema = {
  columns: [
    { name: LOCK_COLUMN_ID, type: ColumnType.String, primaryKey: true, notNull: true },
    { name: LOCK_COLUMN_HOLDER, type: ColumnType.String, notNull: true },
    { name: LOCK_COLUMN_EXPIRES_AT, type: ColumnType.Int, notNull: true },
  ],
};

/**
 * Desired shape of a single object store in a declarative migration revision.
 */
export interface StoreSchema {
  name: string;
  columns: ColumnSchema[];
  indexes?: IndexSchema[];
}

/**
 * Declarative schema change for a revision: stores to ensure exist and stores
 * to drop. Both directions are idempotent (create-if-absent / drop-if-exists).
 */
export interface SchemaSpec {
  stores?: StoreSchema[];
  drop?: { stores?: string[] };
}

/**
 * Restricted, data-only view of an object store handed to imperative revisions.
 * Exposes reads and idempotent writes (`put` upsert, `delete`, `deleteRange`,
 * `clear`) but no schema DDL and no fail-on-exists `add`.
 */
export interface MigrationStore {
  get(id: string): Promise<DbRecord>;
  getAll(query?: Key | KeyRange, options?: GetAllOptions): Promise<DbRecord[]>;
  getAllKeys(query?: Key | KeyRange, options?: GetAllOptions): Promise<string[]>;
  count(query?: Key | KeyRange): Promise<number>;
  openCursor(options?: OpenCursorOptions): Promise<Cursor | null>;
  put(record: DbRecord): Promise<void>;
  delete(id: string): Promise<void>;
  deleteRange(query: Key | KeyRange): Promise<number>;
  clear(): Promise<void>;
}

/**
 * Handle passed to an imperative revision's `up`/`down`, scoping access to data
 * operations over stores that already exist.
 */
export interface MigrationHandle {
  store(name: string): MigrationStore;
}

/**
 * One ordered migration step, identified by an author-assigned `id`. A revision
 * declares either a `schema` change or an imperative `up` (with an optional
 * dev-only `down`), never both. `up` must be idempotent: it may replay after a
 * crash that occurs before its ledger row is recorded.
 */
export interface Revision {
  id: string;
  schema?: SchemaSpec;
  up?: (handle: MigrationHandle) => Promise<void>;
  down?: (handle: MigrationHandle) => Promise<void>;
}

/**
 * The app's ordered revisions plus where they run: the IndexedDB `dbBinding` and
 * the `ledgerStore` that records applied revision ids.
 */
export interface MigrationSet {
  revisions: Revision[];
  dbBinding?: string;
  ledgerStore?: string;
}

/**
 * Outcome of a migration run: the revision ids applied this run, in order, and
 * the ledger head afterwards.
 */
export interface MigrationResult {
  applied: string[];
  head: string;
}

/**
 * Normalizes the `defineApp` `migrations` option (a plain revision array or a
 * full {@link MigrationSet}) into a {@link MigrationSet}, or `undefined` when no
 * migrations are declared.
 */
export function normalizeMigrations(
  input: MigrationSet | Revision[] | undefined,
): MigrationSet | undefined {
  if (input === undefined) {
    return undefined;
  }
  return Array.isArray(input) ? { revisions: input } : input;
}

/**
 * Opens the IndexedDB binding named by the plan, runs {@link runMigrations}
 * against it, and closes the connection.
 */
export async function runMigrationsForBinding(
  plan: MigrationSet,
): Promise<MigrationResult> {
  const db = new IndexedDB(plan.dbBinding);
  try {
    return await runMigrations(db, plan);
  } finally {
    db.close();
  }
}

/**
 * Brings the database up to the plan's head, applying pending revisions in order
 * under an advisory lock. Idempotent: already-applied revisions are skipped.
 * Fails closed if the ledger contains revisions this build does not define (a
 * detected downgrade). Call it from a provider's configure hook so a failure
 * aborts activation.
 */
export async function runMigrations(
  db: IndexedDB,
  plan: MigrationSet,
): Promise<MigrationResult> {
  const ledgerStore = plan.ledgerStore ?? DEFAULT_LEDGER_STORE;
  validatePlan(plan);

  await ensureStore(db, ledgerStore, LEDGER_SCHEMA);
  await ensureStore(db, LOCK_STORE, LOCK_SCHEMA);

  const holder = await acquireLock(db);
  try {
    const applied = new Set(await db.objectStore(ledgerStore).getAllKeys());
    const defined = new Set(plan.revisions.map((rev) => rev.id));
    const ahead = [...applied].filter((id) => !defined.has(id)).sort();
    if (ahead.length > 0) {
      throw new Error(
        `gestalt migrate: ledger is ahead of code — this binary does not define applied ` +
          `revisions ${JSON.stringify(ahead)}. Roll forward to a binary that defines them, ` +
          `or manually undo their DB changes and delete their ledger rows.`,
      );
    }

    const result: MigrationResult = { applied: [], head: headId(plan.revisions) };
    for (const rev of plan.revisions) {
      if (applied.has(rev.id)) {
        continue;
      }
      try {
        await applyRevision(db, rev);
      } catch (err) {
        throw new Error(`gestalt migrate: revision "${rev.id}" failed: ${errorText(err)}`);
      }
      await db.objectStore(ledgerStore).put({
        [LEDGER_COLUMN_REVISION_ID]: rev.id,
        [LEDGER_COLUMN_APPLIED_AT]: new Date().toISOString(),
      });
      result.applied.push(rev.id);
    }
    return result;
  } finally {
    await releaseLock(db, holder);
  }
}

function validatePlan(plan: MigrationSet): void {
  const seen = new Set<string>();
  plan.revisions.forEach((rev, index) => {
    if (!rev.id) {
      throw new Error(`gestalt migrate: revision at index ${index} has an empty id`);
    }
    if (seen.has(rev.id)) {
      throw new Error(`gestalt migrate: duplicate revision id "${rev.id}"`);
    }
    seen.add(rev.id);
    if ((rev.schema !== undefined) === (rev.up !== undefined)) {
      throw new Error(
        `gestalt migrate: revision "${rev.id}" must define exactly one of "schema" or "up"`,
      );
    }
  });
}

async function applyRevision(db: IndexedDB, rev: Revision): Promise<void> {
  if (rev.schema) {
    await applySchema(db, rev.schema);
    return;
  }
  if (rev.up) {
    await rev.up(migrationHandle(db));
  }
}

async function applySchema(db: IndexedDB, schema: SchemaSpec): Promise<void> {
  for (const store of schema.stores ?? []) {
    await ensureStore(db, store.name, {
      columns: store.columns,
      ...(store.indexes ? { indexes: store.indexes } : {}),
    });
  }
  for (const name of schema.drop?.stores ?? []) {
    await dropStore(db, name);
  }
}

async function ensureStore(
  db: IndexedDB,
  name: string,
  schema: ObjectStoreSchema,
): Promise<void> {
  try {
    await db.createObjectStore(name, schema);
  } catch (err) {
    if (!(err instanceof AlreadyExistsError)) {
      throw err;
    }
  }
}

async function dropStore(db: IndexedDB, name: string): Promise<void> {
  try {
    await db.deleteObjectStore(name);
  } catch (err) {
    if (!(err instanceof NotFoundError)) {
      throw err;
    }
  }
}

function migrationHandle(db: IndexedDB): MigrationHandle {
  return {
    store(name: string): MigrationStore {
      return restrictStore(db.objectStore(name));
    },
  };
}

function restrictStore(store: ObjectStore): MigrationStore {
  return {
    get: (id) => store.get(id),
    getAll: (query, options) => store.getAll(query, options),
    getAllKeys: (query, options) => store.getAllKeys(query, options),
    count: (query) => store.count(query),
    openCursor: (options) => store.openCursor(options),
    put: (record) => store.put(record),
    delete: (id) => store.delete(id),
    deleteRange: (query) => store.deleteRange(query),
    clear: () => store.clear(),
  };
}

async function acquireLock(db: IndexedDB): Promise<string> {
  const holder = randomUUID();
  const store = db.objectStore(LOCK_STORE);
  const deadline = Date.now() + LOCK_ACQUIRE_TIMEOUT_MS;
  for (;;) {
    try {
      await store.add(lockRow(holder));
      return holder;
    } catch (err) {
      if (!(err instanceof AlreadyExistsError)) {
        throw err;
      }
    }
    if (await lockIsStale(store)) {
      await store.put(lockRow(holder));
      return holder;
    }
    if (Date.now() > deadline) {
      throw new Error("gestalt migrate: timed out acquiring the migration lock");
    }
    await sleep(LOCK_POLL_MS);
  }
}

async function releaseLock(db: IndexedDB, holder: string): Promise<void> {
  const store = db.objectStore(LOCK_STORE);
  const existing = await readLock(store);
  if (existing && existing[LOCK_COLUMN_HOLDER] === holder) {
    try {
      await store.delete(LOCK_ROW_ID);
    } catch (err) {
      if (!(err instanceof NotFoundError)) {
        throw err;
      }
    }
  }
}

async function lockIsStale(store: ObjectStore): Promise<boolean> {
  const existing = await readLock(store);
  if (!existing) {
    return true;
  }
  const expiresAt = existing[LOCK_COLUMN_EXPIRES_AT];
  return typeof expiresAt !== "number" || Date.now() > expiresAt;
}

async function readLock(store: ObjectStore): Promise<DbRecord | null> {
  try {
    return await store.get(LOCK_ROW_ID);
  } catch (err) {
    if (err instanceof NotFoundError) {
      return null;
    }
    throw err;
  }
}

function lockRow(holder: string): DbRecord {
  return {
    [LOCK_COLUMN_ID]: LOCK_ROW_ID,
    [LOCK_COLUMN_HOLDER]: holder,
    [LOCK_COLUMN_EXPIRES_AT]: Date.now() + LOCK_LEASE_MS,
  };
}

function headId(revisions: Revision[]): string {
  return revisions.length > 0 ? revisions[revisions.length - 1]!.id : "";
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

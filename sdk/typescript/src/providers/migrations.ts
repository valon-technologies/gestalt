import {
  AlreadyExistsError,
  type ColumnSchema,
  type Cursor,
  type GetAllOptions,
  type IndexedDB,
  type IndexSchema,
  type Index as StoreIndex,
  type Key,
  type KeyRange,
  NotFoundError,
  type ObjectStore,
  type OpenCursorOptions,
  type Record as DBRecord,
  type Transaction,
  type TransactionObjectStore,
} from "./indexeddb.ts";

const DEFAULT_LEDGER_STORE = "_gestalt_migrations";
const LEDGER_KEY_COLUMN = "revision_id";
const LEDGER_APPLIED_COLUMN = "applied_at";
const DEFAULT_LOCK_KEY = "_gestalt_migrations";
const DEFAULT_LOCK_TTL_MS = 30_000;
const DEFAULT_ACQUIRE_TIMEOUT_MS = 10 * 60_000;
const LOCK_POLL_INTERVAL_MS = 500;

export type Query = Key | KeyRange;

/**
 * Declarative schema for one store the SDK ensures exists and matches. Columns
 * are fixed at creation (typically `{ id, payload }`); record-shape evolution
 * lives inside the payload and needs no migration.
 */
export interface StoreDeclaration {
  name: string;
  columns?: ColumnSchema[];
  /** Indexes created together with the store. */
  indexes?: IndexSchema[];
}

export interface AddIndexDeclaration {
  store: string;
  index: IndexSchema;
}

export interface IndexRef {
  store: string;
  name: string;
}

/**
 * A declarative schema revision. Applying it converges the database toward the
 * declared state using only the four closed operations — add/drop a store,
 * add/remove an index — so applying it once or many times yields the same
 * result. Drops must be explicit; they are never inferred from omission.
 */
export interface SchemaDeclaration {
  /** Stores to create if absent (with their initial indexes). */
  stores?: StoreDeclaration[];
  /** Indexes to add to already-existing stores. */
  addIndexes?: AddIndexDeclaration[];
  /** Stores to drop if present. Destructive and irreversible. */
  dropStores?: string[];
  /** Indexes to remove if present. */
  dropIndexes?: IndexRef[];
}

/**
 * The restricted handle passed to an imperative revision's `up`/`down`. It is
 * data-only over stores that already exist: every operation on it is idempotent
 * by construction, and it exposes no schema DDL and no `add` (fail-on-exists).
 */
export interface MigrationHandle {
  store(name: string): MigrationStore;
  /** Optional atomic all-or-nothing backfill over a fixed store scope. */
  transaction(stores: string[]): Promise<MigrationTransaction>;
}

/** Data-only, idempotent view of an object store for imperative revisions. */
export interface MigrationStore {
  get(id: string): Promise<DBRecord>;
  getAll(query?: Query, options?: GetAllOptions): Promise<DBRecord[]>;
  getAllKeys(query?: Query, options?: GetAllOptions): Promise<string[]>;
  count(query?: Query): Promise<number>;
  openCursor(options?: OpenCursorOptions): Promise<Cursor | null>;
  index(name: string): MigrationIndex;
  put(record: DBRecord): Promise<void>;
  delete(id: string): Promise<void>;
  deleteRange(query: Query): Promise<number>;
  clear(): Promise<void>;
}

/** Read-only view of a secondary index for imperative revisions. */
export interface MigrationIndex {
  get(query?: Query): Promise<DBRecord>;
  getKey(query?: Query): Promise<string>;
  getAll(query?: Query, options?: GetAllOptions): Promise<DBRecord[]>;
  getAllKeys(query?: Query, options?: GetAllOptions): Promise<string[]>;
  count(query?: Query): Promise<number>;
  openCursor(options?: OpenCursorOptions): Promise<Cursor | null>;
}

export interface MigrationTransaction {
  store(name: string): MigrationTransactionStore;
  commit(): Promise<void>;
  abort(reason?: string): Promise<void>;
}

export interface MigrationTransactionStore {
  get(id: string): Promise<DBRecord>;
  getAll(query?: Query, options?: GetAllOptions): Promise<DBRecord[]>;
  getAllKeys(query?: Query, options?: GetAllOptions): Promise<string[]>;
  count(query?: Query): Promise<number>;
  put(record: DBRecord): Promise<void>;
  delete(id: string): Promise<void>;
  deleteRange(query: Query): Promise<number>;
  clear(): Promise<void>;
}

/**
 * A declarative schema revision. The SDK diffs desired-vs-current and applies
 * only the delta.
 */
export interface SchemaRevision {
  id: string;
  schema: SchemaDeclaration;
  up?: never;
  down?: never;
}

/**
 * An imperative data revision (escape hatch, for backfills). `up` runs against a
 * restricted, idempotent-only handle.
 */
export interface ImperativeRevision {
  id: string;
  up: (m: MigrationHandle) => Promise<void> | void;
  down?: (m: MigrationHandle) => Promise<void> | void;
  schema?: never;
}

/** One author-declared migration, identified by its stable, ordered `id`. */
export type Revision = SchemaRevision | ImperativeRevision;

export interface MigrationRunOptions {
  revisions: Revision[];
  /** Name of the IndexedDB binding to migrate. */
  dbBinding?: string;
  /** Ledger store name. Defaults to `_gestalt_migrations`. */
  ledgerStore?: string;
  /** Advisory-lock key. Defaults to `_gestalt_migrations`. */
  lockKey?: string;
  /** Lease TTL in milliseconds. Defaults to 30s (renewed while running). */
  lockTtlMs?: number;
  /** Max time to wait to acquire the lease. Defaults to 10 minutes. */
  acquireTimeoutMs?: number;
  /** Stable identifier for this instance. Defaults to a random id. */
  holder?: string;
}

/**
 * Outcome of a migration run: the revision ids applied this run, in order, and
 * the declared head afterwards (empty when no revisions are declared).
 */
export interface MigrationResult {
  applied: string[];
  head: string;
}

/**
 * Raised when migrations cannot proceed. Carries the triage fields the process
 * surfaces on failure: the current (applied head) and attempted (declared head)
 * revision.
 */
export class MigrationError extends Error {
  readonly current: string | undefined;
  readonly attempted: string | undefined;
  readonly cause: unknown;

  constructor(
    message: string,
    options?: {
      current?: string | undefined;
      attempted?: string | undefined;
      cause?: unknown;
    },
  ) {
    super(message);
    this.name = "MigrationError";
    this.current = options?.current;
    this.attempted = options?.attempted;
    this.cause = options?.cause;
  }
}

/**
 * Brings the database up to head, acquiring the migration lease first so only
 * one instance migrates at a time. Idempotent — a no-op when nothing is
 * pending. Releases the lease and never leaves it held on error. Returns the
 * revision ids applied this run and the declared head.
 */
export async function runMigrations(
  db: IndexedDB,
  options: MigrationRunOptions,
): Promise<MigrationResult> {
  const revisions = validateRevisions(options.revisions);
  if (revisions.length === 0) {
    return { applied: [], head: "" };
  }

  const ledgerStore = options.ledgerStore?.trim() || DEFAULT_LEDGER_STORE;
  const lockKey = options.lockKey?.trim() || DEFAULT_LOCK_KEY;
  const ttlMs = options.lockTtlMs ?? DEFAULT_LOCK_TTL_MS;
  const acquireTimeoutMs = options.acquireTimeoutMs ?? DEFAULT_ACQUIRE_TIMEOUT_MS;
  const holder = options.holder?.trim() || randomHolderId();

  await ensureLedgerStore(db, ledgerStore);
  await acquireLease(db, lockKey, holder, ttlMs, acquireTimeoutMs);

  const renew = startLeaseRenewal(db, lockKey, holder, ttlMs);
  try {
    const applied = new Set(await db.objectStore(ledgerStore).getAllKeys());
    assertNotAheadOfCode(revisions, applied);
    assertContiguousPrefix(revisions, applied);

    const attemptedHead = revisions[revisions.length - 1]?.id ?? "";
    let current = latestAppliedId(revisions, applied);
    const appliedNow: string[] = [];
    for (const revision of revisions) {
      if (applied.has(revision.id)) {
        continue;
      }
      const assertHeld = (): void => {
        if (renew.lost()) {
          throw leaseLostError(revision.id, current, attemptedHead);
        }
      };
      assertHeld();
      try {
        await applyRevision(db, revision, assertHeld);
        assertHeld();
        await recordRevision(db, ledgerStore, revision.id);
        appliedNow.push(revision.id);
        current = revision.id;
      } catch (error) {
        if (error instanceof MigrationError) {
          throw error;
        }
        throw new MigrationError(
          `migration ${JSON.stringify(revision.id)} failed: ${errorText(error)}`,
          { current, attempted: attemptedHead, cause: error },
        );
      }
    }
    return { applied: appliedNow, head: attemptedHead };
  } finally {
    renew.stop();
    await releaseQuietly(db, lockKey, holder);
  }
}

function validateRevisions(revisions: Revision[]): Revision[] {
  const seen = new Set<string>();
  for (const revision of revisions) {
    const id = revision.id?.trim();
    if (!id) {
      throw new MigrationError("every revision needs a non-empty id");
    }
    if (seen.has(id)) {
      throw new MigrationError(`duplicate revision id ${JSON.stringify(id)}`);
    }
    seen.add(id);
    const hasSchema = "schema" in revision && revision.schema !== undefined;
    const hasUp = "up" in revision && typeof revision.up === "function";
    if (hasSchema === hasUp) {
      throw new MigrationError(
        `revision ${JSON.stringify(id)} must declare exactly one of "schema" or "up"`,
      );
    }
  }
  return revisions;
}

function assertNotAheadOfCode(
  revisions: Revision[],
  applied: Set<string>,
): void {
  const declared = new Set(revisions.map((revision) => revision.id));
  const unknown = [...applied].filter((id) => !declared.has(id));
  if (unknown.length > 0) {
    const attemptedHead = revisions[revisions.length - 1]?.id;
    throw new MigrationError(
      `ledger is ahead of code: applied revision(s) ${unknown
        .map((id) => JSON.stringify(id))
        .join(", ")} are not declared by this binary. Roll forward to a binary ` +
        `that defines them, or manually undo them and delete their ledger rows.`,
      { current: unknown[unknown.length - 1], attempted: attemptedHead },
    );
  }
}

function assertContiguousPrefix(
  revisions: Revision[],
  applied: Set<string>,
): void {
  let sawUnapplied = false;
  const outOfOrder: string[] = [];
  for (const revision of revisions) {
    if (applied.has(revision.id)) {
      if (sawUnapplied) {
        outOfOrder.push(revision.id);
      }
    } else {
      sawUnapplied = true;
    }
  }
  if (outOfOrder.length > 0) {
    throw new MigrationError(
      `ledger has gaps: revision(s) ${outOfOrder
        .map((id) => JSON.stringify(id))
        .join(", ")} are applied but an earlier declared revision is not. ` +
        `Revisions are an append-only ledger — a new revision must be added after ` +
        `all applied ones, never inserted or reordered before them.`,
      { attempted: revisions[revisions.length - 1]?.id ?? "" },
    );
  }
}

function latestAppliedId(
  revisions: Revision[],
  applied: Set<string>,
): string | undefined {
  let current: string | undefined;
  for (const revision of revisions) {
    if (applied.has(revision.id)) {
      current = revision.id;
    }
  }
  return current;
}

function leaseLostError(
  revisionId: string,
  current: string | undefined,
  attemptedHead: string,
): MigrationError {
  return new MigrationError(
    `lost the migration lease while migrating ${JSON.stringify(revisionId)}; ` +
      `another instance may hold it — aborting to avoid concurrent migration`,
    { current, attempted: attemptedHead },
  );
}

async function ensureLedgerStore(
  db: IndexedDB,
  ledgerStore: string,
): Promise<void> {
  await createStoreIfAbsent(db, {
    name: ledgerStore,
    columns: [
      { name: LEDGER_KEY_COLUMN, primaryKey: true, notNull: true },
      { name: LEDGER_APPLIED_COLUMN, notNull: true },
    ],
  });
}

async function applyRevision(
  db: IndexedDB,
  revision: Revision,
  assertHeld: () => void,
): Promise<void> {
  if (isSchemaRevision(revision)) {
    await applySchema(db, revision.schema, assertHeld);
    return;
  }
  await revision.up(migrationHandle(db, assertHeld));
}

async function applySchema(
  db: IndexedDB,
  schema: SchemaDeclaration,
  assertHeld: () => void,
): Promise<void> {
  for (const store of schema.stores ?? []) {
    assertHeld();
    await createStoreIfAbsent(db, store);
  }
  for (const entry of schema.addIndexes ?? []) {
    assertHeld();
    await createIndexIfAbsent(db, entry.store, entry.index);
  }
  for (const entry of schema.dropIndexes ?? []) {
    assertHeld();
    await dropIndexIfPresent(db, entry.store, entry.name);
  }
  for (const name of schema.dropStores ?? []) {
    assertHeld();
    await dropStoreIfPresent(db, name);
  }
}

async function createStoreIfAbsent(
  db: IndexedDB,
  store: StoreDeclaration,
): Promise<void> {
  try {
    await db.createObjectStore(store.name, {
      ...(store.columns !== undefined ? { columns: store.columns } : {}),
      ...(store.indexes !== undefined ? { indexes: store.indexes } : {}),
    });
  } catch (error) {
    if (!(error instanceof AlreadyExistsError)) {
      throw error;
    }
  }
}

async function createIndexIfAbsent(
  db: IndexedDB,
  store: string,
  index: IndexSchema,
): Promise<void> {
  try {
    await db.createIndex(store, index);
  } catch (error) {
    if (!(error instanceof AlreadyExistsError)) {
      throw error;
    }
  }
}

async function dropStoreIfPresent(db: IndexedDB, name: string): Promise<void> {
  try {
    await db.deleteObjectStore(name);
  } catch (error) {
    if (!(error instanceof NotFoundError)) {
      throw error;
    }
  }
}

async function dropIndexIfPresent(
  db: IndexedDB,
  store: string,
  name: string,
): Promise<void> {
  try {
    await db.deleteIndex(store, name);
  } catch (error) {
    if (!(error instanceof NotFoundError)) {
      throw error;
    }
  }
}

async function recordRevision(
  db: IndexedDB,
  ledgerStore: string,
  id: string,
): Promise<void> {
  await db.objectStore(ledgerStore).put({
    [LEDGER_KEY_COLUMN]: id,
    [LEDGER_APPLIED_COLUMN]: new Date().toISOString(),
  });
}

async function acquireLease(
  db: IndexedDB,
  key: string,
  holder: string,
  ttlMs: number,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const lease = await db.acquireLock(key, holder, ttlMs);
    if (lease.acquired) {
      return;
    }
    if (Date.now() >= deadline) {
      throw new MigrationError(
        `timed out after ${timeoutMs}ms waiting for the migration lease ` +
          `(held by ${JSON.stringify(lease.holder)})`,
      );
    }
    await sleep(LOCK_POLL_INTERVAL_MS);
  }
}

function startLeaseRenewal(
  db: IndexedDB,
  key: string,
  holder: string,
  ttlMs: number,
): { stop: () => void; lost: () => boolean } {
  const interval = Math.max(1, Math.min(Math.floor(ttlMs / 2), ttlMs - 1));
  let lost = false;
  const timer = setInterval(() => {
    void db.acquireLock(key, holder, ttlMs).then(
      (lease) => {
        if (!lease.acquired) {
          lost = true;
        }
      },
      () => {},
    );
  }, interval);
  if (typeof timer === "object" && "unref" in timer) {
    (timer as { unref: () => void }).unref();
  }
  return {
    stop: () => clearInterval(timer),
    lost: () => lost,
  };
}

async function releaseQuietly(
  db: IndexedDB,
  key: string,
  holder: string,
): Promise<void> {
  try {
    await db.releaseLock(key, holder);
  } catch {
    return;
  }
}

function migrationHandle(
  db: IndexedDB,
  assertHeld: () => void,
): MigrationHandle {
  return {
    store: (name) => new MigrationStoreImpl(db.objectStore(name), assertHeld),
    transaction: async (stores) =>
      new MigrationTransactionImpl(
        await db.transaction(stores, "readwrite"),
        assertHeld,
      ),
  };
}

class MigrationStoreImpl implements MigrationStore {
  constructor(
    private readonly store: ObjectStore,
    private readonly assertHeld: () => void,
  ) {}

  get(id: string): Promise<DBRecord> {
    return this.store.get(id);
  }
  getAll(query?: Query, options?: GetAllOptions): Promise<DBRecord[]> {
    return this.store.getAll(query, options);
  }
  getAllKeys(query?: Query, options?: GetAllOptions): Promise<string[]> {
    return this.store.getAllKeys(query, options);
  }
  count(query?: Query): Promise<number> {
    return this.store.count(query);
  }
  openCursor(options?: OpenCursorOptions): Promise<Cursor | null> {
    return this.store.openCursor(options);
  }
  index(name: string): MigrationIndex {
    return new MigrationIndexImpl(this.store.index(name));
  }
  put(record: DBRecord): Promise<void> {
    this.assertHeld();
    return this.store.put(record);
  }
  delete(id: string): Promise<void> {
    this.assertHeld();
    return this.store.delete(id);
  }
  deleteRange(query: Query): Promise<number> {
    this.assertHeld();
    return this.store.deleteRange(query);
  }
  clear(): Promise<void> {
    this.assertHeld();
    return this.store.clear();
  }
}

class MigrationIndexImpl implements MigrationIndex {
  constructor(private readonly index: StoreIndex) {}

  get(query?: Query): Promise<DBRecord> {
    return this.index.get(query);
  }
  getKey(query?: Query): Promise<string> {
    return this.index.getKey(query);
  }
  getAll(query?: Query, options?: GetAllOptions): Promise<DBRecord[]> {
    return this.index.getAll(query, options);
  }
  getAllKeys(query?: Query, options?: GetAllOptions): Promise<string[]> {
    return this.index.getAllKeys(query, options);
  }
  count(query?: Query): Promise<number> {
    return this.index.count(query);
  }
  openCursor(options?: OpenCursorOptions): Promise<Cursor | null> {
    return this.index.openCursor(options);
  }
}

class MigrationTransactionImpl implements MigrationTransaction {
  constructor(
    private readonly tx: Transaction,
    private readonly assertHeld: () => void,
  ) {}

  store(name: string): MigrationTransactionStore {
    return new MigrationTransactionStoreImpl(
      this.tx.objectStore(name),
      this.assertHeld,
    );
  }
  commit(): Promise<void> {
    this.assertHeld();
    return this.tx.commit();
  }
  abort(reason?: string): Promise<void> {
    return this.tx.abort(reason);
  }
}

class MigrationTransactionStoreImpl implements MigrationTransactionStore {
  constructor(
    private readonly store: TransactionObjectStore,
    private readonly assertHeld: () => void,
  ) {}

  get(id: string): Promise<DBRecord> {
    return this.store.get(id);
  }
  getAll(query?: Query, options?: GetAllOptions): Promise<DBRecord[]> {
    return this.store.getAll(query, options);
  }
  getAllKeys(query?: Query, options?: GetAllOptions): Promise<string[]> {
    return this.store.getAllKeys(query, options);
  }
  count(query?: Query): Promise<number> {
    return this.store.count(query);
  }
  put(record: DBRecord): Promise<void> {
    this.assertHeld();
    return this.store.put(record);
  }
  delete(id: string): Promise<void> {
    this.assertHeld();
    return this.store.delete(id);
  }
  deleteRange(query: Query): Promise<number> {
    this.assertHeld();
    return this.store.deleteRange(query);
  }
  clear(): Promise<void> {
    this.assertHeld();
    return this.store.clear();
  }
}

function isSchemaRevision(revision: Revision): revision is SchemaRevision {
  return "schema" in revision && revision.schema !== undefined;
}

function randomHolderId(): string {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) {
    return uuid;
  }
  return `holder-${Math.random().toString(36).slice(2)}-${Date.now()}`;
}

function errorText(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

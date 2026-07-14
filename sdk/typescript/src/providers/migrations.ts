import {
  AlreadyExistsError,
  type ColumnSchema,
  type IndexedDB,
  type IndexSchema,
  type Key,
  type KeyRange,
  NotFoundError,
  type Record as DBRecord,
} from "./indexeddb.ts";

const DEFAULT_LEDGER_STORE = "_gestalt_migrations";
const LEDGER_KEY_COLUMN = "revision_id";
const LEDGER_APPLIED_COLUMN = "applied_at";

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
 * A declarative schema revision. The SDK diffs desired-vs-current and applies
 * only the delta.
 */
export interface SchemaRevision {
  id: string;
  schema: SchemaDeclaration;
  backfill?: never;
}

export interface BackfillTransform {
  from: string;
  into: string;
  value: (row: DBRecord) => DBRecord;
}

export interface BackfillRevision {
  id: string;
  backfill: BackfillTransform;
  schema?: never;
}

/** One author-declared migration, identified by its stable, ordered `id`. */
export type Revision = SchemaRevision | BackfillRevision;

export interface MigrationRunOptions {
  revisions: Revision[];
  /** Name of the IndexedDB binding to migrate. */
  dbBinding?: string;
  /**
   * Ledger store name. When omitted, {@link runMigrations} defaults to
   * `_gestalt_migrations`; provider startup derives a per-provider store from
   * the configured provider name.
   */
  ledgerStore?: string;
}

/**
 * Derive the default migration ledger store for a configured provider name.
 */
export function providerMigrationLedgerStore(providerName: string): string {
  let normalized = providerName.trim();
  const slash = normalized.lastIndexOf("/");
  if (slash >= 0) {
    normalized = normalized.slice(slash + 1);
  }
  if (!normalized) {
    return DEFAULT_LEDGER_STORE;
  }
  const slug = migrationLedgerSlug(normalized);
  if (!slug) {
    return DEFAULT_LEDGER_STORE;
  }
  const snake = slug
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .replace(/-/g, "_")
    .toLowerCase();
  return `${snake}_migrations`;
}

function migrationLedgerSlug(value: string): string {
  let slug = "";
  for (const char of value) {
    if (
      (char >= "A" && char <= "Z") ||
      (char >= "a" && char <= "z") ||
      (char >= "0" && char <= "9") ||
      char === "." ||
      char === "_" ||
      char === "-"
    ) {
      slug += char;
    } else {
      slug += "-";
    }
  }
  return slug.replace(/^-+|-+$/g, "");
}

/** Resolve the IndexedDB binding for migration runs. */
export function resolveMigrationDbBinding(
  options: MigrationRunOptions,
  config: Record<string, unknown>,
): string {
  const explicit = String(options.dbBinding ?? "").trim();
  if (explicit) {
    return explicit;
  }
  const configBinding = config.indexeddb;
  return typeof configBinding === "string" ? configBinding.trim() : "";
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
 * Brings the database up to head. Concurrent instances may run this at once;
 * that is safe because every revision is idempotent by construction, so
 * concurrent runs converge to the same state and the ledger records each
 * revision once. Idempotent — a no-op when nothing is pending. Returns the
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
  await ensureLedgerStore(db, ledgerStore);

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
    try {
      await applyRevision(db, revision);
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
}

function validateRevisions(revisions: Revision[]): Revision[] {
  const seen = new Set<string>();
  const normalized: Revision[] = [];
  for (const revision of revisions) {
    const id = revision.id?.trim();
    if (!id) {
      throw new MigrationError("every revision needs a non-empty id");
    }
    if (seen.has(id)) {
      throw new MigrationError(`duplicate revision id ${JSON.stringify(id)}`);
    }
    seen.add(id);
    const hasSchema = "schema" in revision && revision.schema != null;
    const backfill = revision.backfill;
    const hasBackfill = backfill != null;
    if (hasSchema === hasBackfill) {
      throw new MigrationError(
        `revision ${JSON.stringify(id)} must declare exactly one of "schema" or "backfill"`,
      );
    }
    if (backfill && backfill.from === backfill.into) {
      throw new MigrationError(
        `revision ${JSON.stringify(id)} backfill "from" and "into" must differ: a ` +
          `backfill reads an immutable source and writes a distinct target, so it ` +
          `cannot read its own output and is idempotent by construction`,
      );
    }
    normalized.push({ ...revision, id });
  }
  return normalized;
}

function revisionNamespaces(revisions: Revision[]): {
  prefixes: Set<string>;
  hasFlat: boolean;
} {
  const prefixes = new Set<string>();
  let hasFlat = false;
  for (const revision of revisions) {
    const id = revision.id.trim();
    if (!id) {
      continue;
    }
    const slash = id.lastIndexOf("/");
    if (slash >= 0) {
      prefixes.add(id.slice(0, slash + 1));
      continue;
    }
    hasFlat = true;
  }
  return { prefixes, hasFlat };
}

function ledgerIDDirectoryPrefix(id: string): string {
  const slash = id.lastIndexOf("/");
  if (slash < 0) {
    return "";
  }
  return id.slice(0, slash + 1);
}

function ledgerIDOwnedByProvider(
  id: string,
  prefixes: Set<string>,
  hasFlat: boolean,
): boolean {
  if (id.includes("/")) {
    return prefixes.has(ledgerIDDirectoryPrefix(id));
  }
  return hasFlat && prefixes.size === 0;
}

function assertNotAheadOfCode(
  revisions: Revision[],
  applied: Set<string>,
): void {
  const declared = new Set(revisions.map((revision) => revision.id));
  const { prefixes, hasFlat } = revisionNamespaces(revisions);
  const unknown = [...applied]
    .filter(
      (id) =>
        !declared.has(id) && ledgerIDOwnedByProvider(id, prefixes, hasFlat),
    )
    .sort();
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

async function applyRevision(db: IndexedDB, revision: Revision): Promise<void> {
  if (isSchemaRevision(revision)) {
    await applySchema(db, revision.schema);
    return;
  }
  await applyBackfill(db, revision.backfill);
}

async function applyBackfill(
  db: IndexedDB,
  transform: BackfillTransform,
): Promise<void> {
  const target = db.objectStore(transform.into);
  const cursor = await db.objectStore(transform.from).openCursor();
  if (cursor === null) {
    return;
  }
  try {
    while (await cursor.continue()) {
      const row = cursor.value;
      if (row === undefined) {
        continue;
      }
      await target.put(transform.value(row));
    }
  } finally {
    cursor.close();
  }
}

async function applySchema(
  db: IndexedDB,
  schema: SchemaDeclaration,
): Promise<void> {
  for (const store of schema.stores ?? []) {
    await createStoreIfAbsent(db, store);
  }
  for (const entry of schema.addIndexes ?? []) {
    await createIndexIfAbsent(db, entry.store, entry.index);
  }
  for (const entry of schema.dropIndexes ?? []) {
    await dropIndexIfPresent(db, entry.store, entry.name);
  }
  for (const name of schema.dropStores ?? []) {
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

function isSchemaRevision(revision: Revision): revision is SchemaRevision {
  return "schema" in revision && revision.schema != null;
}

function errorText(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

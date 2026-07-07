import type { IndexedDB } from "../providers/indexeddb.ts";
import {
  applyRevisionForVerification,
  type Revision,
} from "../providers/migrations.ts";
import type { MigrationsOption } from "../providers/app.ts";
import { MemoryIndexedDB } from "./memory-indexeddb.ts";

export type MigrationSeed = (db: IndexedDB) => Promise<void> | void;

export interface VerifyAllOptions {
  seeds?: globalThis.Record<string, MigrationSeed>;
}

export class IdempotencyError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "IdempotencyError";
  }
}

function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value) ?? "null";
  }
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(",")}]`;
  }
  const record = value as globalThis.Record<string, unknown>;
  const entries = Object.keys(record)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableStringify(record[key])}`);
  return `{${entries.join(",")}}`;
}

function assertConverges(
  actual: unknown,
  expected: unknown,
  revisionId: string,
  mode: string,
): void {
  const got = stableStringify(actual);
  const want = stableStringify(expected);
  if (got !== want) {
    throw new IdempotencyError(
      `revision ${JSON.stringify(revisionId)} is not idempotent under ${mode}: ` +
        `state diverged from a single application.\n  once:   ${want}\n  ${mode}: ${got}`,
    );
  }
}

export async function verifyRevisionIdempotent(
  revision: Revision,
  seed?: MigrationSeed,
): Promise<void> {
  const replay = new MemoryIndexedDB();
  if (seed) {
    await seed(replay);
  }
  await applyRevisionForVerification(replay, revision);
  const once = replay.dump();
  await applyRevisionForVerification(replay, revision);
  assertConverges(replay.dump(), once, revision.id, "replay");

  const single = new MemoryIndexedDB();
  if (seed) {
    await seed(single);
  }
  await applyRevisionForVerification(single, revision);
  const expected = single.dump();

  const shared = new MemoryIndexedDB();
  if (seed) {
    await seed(shared);
  }
  await Promise.all([
    applyRevisionForVerification(shared, revision),
    applyRevisionForVerification(shared, revision),
  ]);
  assertConverges(shared.dump(), expected, revision.id, "concurrent application");
}

export async function verifyAllRevisions(
  migrations: MigrationsOption,
  options?: VerifyAllOptions,
): Promise<void> {
  const revisions = Array.isArray(migrations)
    ? migrations
    : migrations.revisions;
  const db = new MemoryIndexedDB();
  for (const revision of revisions) {
    const seed = options?.seeds?.[revision.id];
    if (seed) {
      await seed(db);
    }

    const replay = db.clone();
    await applyRevisionForVerification(replay, revision);
    const once = replay.dump();
    await applyRevisionForVerification(replay, revision);
    assertConverges(replay.dump(), once, revision.id, "replay");

    const concurrent = db.clone();
    await Promise.all([
      applyRevisionForVerification(concurrent, revision),
      applyRevisionForVerification(concurrent, revision),
    ]);
    assertConverges(concurrent.dump(), once, revision.id, "concurrent application");

    await applyRevisionForVerification(db, revision);
  }
}

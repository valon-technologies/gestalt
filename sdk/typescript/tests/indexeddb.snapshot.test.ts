import { expect, test } from "bun:test";

import {
  CursorDirection,
  IndexedDBCursorSnapshot,
  compareIndexedDBValues,
  indexedDBRangeBounds,
} from "../src/index.ts";

test("IndexedDBCursorSnapshot sorts, ranges, and skips duplicate unique index keys", () => {
  const snapshot = new IndexedDBCursorSnapshot({
    direction: CursorDirection.NextUnique,
    index: "by_status",
  });

  snapshot.load(
    [
      { key: ["todo"], primaryKey: "issue-2", primaryKeyValue: "issue-2" },
      { key: ["done"], primaryKey: "issue-3", primaryKeyValue: "issue-3" },
      { key: ["todo"], primaryKey: "issue-1", primaryKeyValue: "issue-1" },
    ],
    { lower: ["done"], upper: ["todo"] },
  );

  expect(snapshot.next()?.primaryKey).toBe("issue-3");
  expect(snapshot.next()?.primaryKey).toBe("issue-1");
  expect(snapshot.next()).toBeUndefined();
});

test("IndexedDBCursorSnapshot advance moves exactly count entries from current position", () => {
  const snapshot = new IndexedDBCursorSnapshot();
  snapshot.load([
    { key: "a", primaryKey: "a" },
    { key: "b", primaryKey: "b" },
    { key: "c", primaryKey: "c" },
  ]);

  expect(snapshot.next()?.primaryKey).toBe("a");
  expect(snapshot.advance(1)?.primaryKey).toBe("b");
  expect(snapshot.advance(1)?.primaryKey).toBe("c");
});

test("IndexedDBCursorSnapshot index ranges accept scalar entry keys", () => {
  const snapshot = new IndexedDBCursorSnapshot({ index: "by_status" });
  snapshot.load(
    [
      { key: "done", primaryKey: "issue-2", primaryKeyValue: "issue-2" },
      { key: "active", primaryKey: "issue-1", primaryKeyValue: "issue-1" },
    ],
    { lower: "active", upper: "active" },
  );

  const first = snapshot.next();
  expect(first?.primaryKey).toBe("issue-1");
  expect(first?.key).toBe("active");
  expect(snapshot.next()).toBeUndefined();
});

test("IndexedDB range bounds normalize scalar index bounds", () => {
  expect(
    indexedDBRangeBounds({ lower: "active", upper: ["done"] }, true),
  ).toEqual([["active"], ["done"]]);
});

test("compareIndexedDBValues compares composite keys lexicographically", () => {
  expect(compareIndexedDBValues(["active", 1], ["active", 2])).toBeLessThan(0);
  expect(compareIndexedDBValues(["active", 2], ["active", 2])).toBe(0);
  expect(compareIndexedDBValues(["active", 3], ["active", 2])).toBeGreaterThan(
    0,
  );
});

test("compareIndexedDBValues compares mixed bigint and number keys exactly", () => {
  expect(compareIndexedDBValues(9007199254740993n, 9007199254740992)).toBeGreaterThan(
    0,
  );
  expect(compareIndexedDBValues(9007199254740992, 9007199254740993n)).toBeLessThan(
    0,
  );
});

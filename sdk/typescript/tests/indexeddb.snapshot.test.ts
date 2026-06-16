import { expect, test } from "bun:test";

import {
  CursorDirection,
  IndexedDBCursorSnapshot,
  bound,
  compareKeys,
  only,
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
    bound(["done"], ["todo"]),
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
    only("active"),
  );

  const first = snapshot.next();
  expect(first?.primaryKey).toBe("issue-1");
  expect(first?.key).toBe("active");
  expect(snapshot.next()).toBeUndefined();
});

test("compareKeys compares composite keys lexicographically", () => {
  expect(compareKeys(["active", 1], ["active", 2])).toBeLessThan(0);
  expect(compareKeys(["active", 2], ["active", 2])).toBe(0);
  expect(compareKeys(["active", 3], ["active", 2])).toBeGreaterThan(0);
});

test("compareKeys compares integer keys exactly", () => {
  expect(compareKeys(42, 41)).toBeGreaterThan(0);
  expect(compareKeys(41, 42)).toBeLessThan(0);
});

import { expect, test } from "bun:test";

import { readFile } from "node:fs/promises";
import { join } from "node:path";

const distClient = join(import.meta.dir, "..", "dist", "client", "index.js");

test("published REST client bundle does not eagerly import connect-node", async () => {
  const bundle = await readFile(distClient, "utf8");
  expect(bundle.includes('from "@connectrpc/connect-node"')).toBe(false);
  expect(bundle.includes('import("@connectrpc/connect-node")')).toBe(true);
});

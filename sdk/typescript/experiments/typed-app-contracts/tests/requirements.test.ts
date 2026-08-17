import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, test } from "bun:test";

test("the design requirements and executable tests have identical traceability IDs", async () => {
  const root = join(import.meta.dir, "..");
  const design = await readFile(join(root, "README.md"), "utf8");
  const documented = new Set([...design.matchAll(/^### (R-[A-Z]+-\d+)/gm)].map((match) => match[1]!));
  const testFiles = (await readdir(import.meta.dir)).filter(
    (name) => name.endsWith(".test.ts") && name !== "requirements.test.ts",
  );
  const source = (
    await Promise.all(testFiles.map((name) => readFile(join(import.meta.dir, name), "utf8")))
  ).join("\n");
  const exercised = new Set([...source.matchAll(/R-[A-Z]+-\d+/g)].map((match) => match[0]));
  expect([...exercised].sort()).toEqual([...documented].sort());
});

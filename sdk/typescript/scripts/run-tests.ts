import { readdir } from "node:fs/promises";
import { join } from "node:path";

const ISOLATED_TESTS = [
  join("tests", "s3.transport.test.ts"),
  join("tests", "indexeddb.transport.test.ts"),
];

const testFiles = (await collectTestFiles("tests"))
  .filter((path) => !ISOLATED_TESTS.includes(path))
  .sort();

for (const path of ISOLATED_TESTS) {
  await runBunTest([path]);
}
await runBunTest(testFiles);

async function collectTestFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const files = await Promise.all(
    entries.map(async (entry) => {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        return await collectTestFiles(path);
      }
      return entry.isFile() && entry.name.endsWith(".test.ts") ? [path] : [];
    }),
  );
  return files.flat();
}

async function runBunTest(files: string[]): Promise<void> {
  if (files.length === 0) {
    return;
  }
  const proc = Bun.spawn(
    ["bun", "test", ...files, "--max-concurrency", "1"],
    {
      stdout: "inherit",
      stderr: "inherit",
    },
  );
  const exitCode = await proc.exited;
  if (exitCode !== 0) {
    process.exit(exitCode);
  }
}

import { readdir } from "node:fs/promises";
import { join } from "node:path";

const S3_TRANSPORT_TEST = join("tests", "s3.transport.test.ts");
const INDEXEDDB_TRANSPORT_TEST = join("tests", "indexeddb.transport.test.ts");

const testFiles = (await collectTestFiles("tests"))
  .filter((path) => path !== S3_TRANSPORT_TEST && path !== INDEXEDDB_TRANSPORT_TEST)
  .sort();

await runBunTest([S3_TRANSPORT_TEST]);
await runBunTest([INDEXEDDB_TRANSPORT_TEST]);
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

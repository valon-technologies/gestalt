import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

export function fixturePath(...segments: string[]): string {
  return resolve(import.meta.dir, "fixtures", ...segments);
}

export function makeTempDir(prefix = "gestalt-typescript-web-test-"): string {
  return mkdtempSync(join(tmpdir(), prefix));
}

export function removeTempDir(path: string): void {
  rmSync(path, {
    recursive: true,
    force: true,
  });
}

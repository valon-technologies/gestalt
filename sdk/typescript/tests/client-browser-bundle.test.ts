import { expect, test } from "bun:test";
import { mkdir, rm } from "node:fs/promises";
import { join } from "node:path";

const outDir = join(import.meta.dir, ".tmp-browser-bundle");

test("documented REST browser import bundles without Node gRPC transport", async () => {
  await rm(outDir, { recursive: true, force: true });
  await mkdir(outDir, { recursive: true });

  const entry = join(import.meta.dir, "browser-bundle-entry.ts");
  const proc = Bun.spawn(
    [
      "bun",
      "build",
      entry,
      "--outdir",
      outDir,
      "--target",
      "browser",
      "--format",
      "esm",
    ],
    {
      cwd: join(import.meta.dir, ".."),
      stdout: "pipe",
      stderr: "pipe",
    },
  );
  const [stdout, stderr, exitCode] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  expect(exitCode).toBe(0);
  expect(stderr).not.toContain("error:");

  const bundle = await Bun.file(join(outDir, "browser-bundle-entry.js")).text();
  expect(bundle.includes("@connectrpc/connect-node")).toBe(false);
  expect(bundle.includes("node:zlib")).toBe(false);
  if (stdout.trim()) {
    expect(stdout).toBeDefined();
  }
});

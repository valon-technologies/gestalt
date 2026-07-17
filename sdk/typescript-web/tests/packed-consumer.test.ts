import { execSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { expect, test } from "bun:test";

import { makeTempDir, removeTempDir } from "./helpers.ts";

const packageRoot = join(import.meta.dir, "..");

function packPackage(): string {
  const tarball = execSync("npm pack --silent", {
    cwd: packageRoot,
    encoding: "utf8",
  }).trim();
  return join(packageRoot, tarball);
}

function installPackedFixture(tarballPath: string): string {
  const fixtureDir = makeTempDir("gestalt-web-packed-");
  writeFileSync(
    join(fixtureDir, "package.json"),
    JSON.stringify(
      {
        name: "gestalt-web-packed-fixture",
        private: true,
        type: "module",
        devDependencies: {
          "@valon-technologies/gestalt-web": `file:${tarballPath}`,
          typescript: "5.9.3",
        },
      },
      null,
      2,
    ),
  );
  writeFileSync(
    join(fixtureDir, "tsconfig.json"),
    JSON.stringify(
      {
        compilerOptions: {
          strict: true,
          module: "NodeNext",
          moduleResolution: "NodeNext",
          noEmit: true,
          skipLibCheck: true,
        },
      },
      null,
      2,
    ),
  );
  writeFileSync(
    join(fixtureDir, "consumer.ts"),
    [
      'import { bearer, session, type Auth, GestaltError, InvokeError } from "@valon-technologies/gestalt-web";',
      "const auth: Auth = session();",
      'void bearer(() => "token");',
      "void auth;",
      "void GestaltError;",
      "void InvokeError;",
      "",
    ].join("\n"),
  );
  execSync("npm install --silent", {
    cwd: fixtureDir,
    stdio: "pipe",
    timeout: 120_000,
  });
  return fixtureDir;
}

test(
  "packed gestalt-web root typechecks under ordinary tsc",
  () => {
    const tarball = packPackage();
    const fixtureDir = installPackedFixture(tarball);
    try {
      execSync("npx tsc --noEmit", {
        cwd: fixtureDir,
        encoding: "utf8",
        stdio: "pipe",
        timeout: 120_000,
      });
    } finally {
      removeTempDir(fixtureDir);
      rmSync(tarball, { force: true });
    }
  },
  { timeout: 120_000 },
);

test("packed gestalt-web exports point at emitted JavaScript", () => {
  const tarball = packPackage();
  const extractedRoot = mkdtempSync(join(tmpdir(), "gestalt-web-tarball-"));
  execSync(`tar -xzf ${tarball}`, { cwd: extractedRoot });
  const packageJson = JSON.parse(
    readFileSync(join(extractedRoot, "package", "package.json"), "utf8"),
  ) as {
    exports: Record<string, { import?: string; types?: string } | string>;
  };
  expect(packageJson.exports["."]).toEqual({
    types: "./dist/index.d.ts",
    import: "./dist/index.js",
  });
  expect(packageJson.exports["./mount"]).toEqual({
    types: "./dist/mount.d.ts",
    import: "./dist/mount.js",
  });
  expect(packageJson.exports["./vite"]).toBeTypeOf("object");
  rmSync(extractedRoot, { recursive: true, force: true });
  rmSync(tarball, { force: true });
});

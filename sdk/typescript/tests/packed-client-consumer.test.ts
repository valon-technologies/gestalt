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
  const fixtureDir = makeTempDir("gestalt-packed-");
  writeFileSync(
    join(fixtureDir, "package.json"),
    JSON.stringify(
      {
        name: "gestalt-packed-fixture",
        private: true,
        type: "module",
        dependencies: {
          "@bufbuild/protobuf": "2.11.0",
          "@connectrpc/connect": "2.1.0",
          "@connectrpc/connect-node": "2.1.0",
          "@valon-technologies/gestalt": `file:${tarballPath}`,
        },
      },
      null,
      2,
    ),
  );
  writeFileSync(
    join(fixtureDir, "import.mjs"),
    `import { bearer, grpc, rest } from "@valon-technologies/gestalt/client";\nconsole.log([bearer(() => "x").kind, rest().kind, grpc().kind].join(","));\n`,
  );
  execSync("npm install --silent", {
    cwd: fixtureDir,
    stdio: "pipe",
    timeout: 120_000,
  });
  return fixtureDir;
}

test(
  "packed gestalt/client loads on Node without type stripping",
  () => {
    const tarball = packPackage();
    const fixtureDir = installPackedFixture(tarball);
    try {
      const output = execSync("node import.mjs", {
        cwd: fixtureDir,
        encoding: "utf8",
      });
      expect(output.trim()).toBe("bearer,rest,grpc");
    } finally {
      removeTempDir(fixtureDir);
      rmSync(tarball, { force: true });
    }
  },
  { timeout: 60_000 },
);

test(
  "packed gestalt/client loads on Bun without type stripping",
  () => {
    const tarball = packPackage();
    const fixtureDir = installPackedFixture(tarball);
    try {
      const output = execSync("bun import.mjs", {
        cwd: fixtureDir,
        encoding: "utf8",
      });
      expect(output.trim()).toBe("bearer,rest,grpc");
    } finally {
      removeTempDir(fixtureDir);
      rmSync(tarball, { force: true });
    }
  },
  { timeout: 60_000 },
);

test("packed gestalt/client export points at emitted JavaScript", () => {
  const tarball = packPackage();
  const extractedRoot = mkdtempSync(join(tmpdir(), "gestalt-tarball-"));
  execSync(`tar -xzf ${tarball}`, { cwd: extractedRoot });
  const packageJson = JSON.parse(
    readFileSync(join(extractedRoot, "package", "package.json"), "utf8"),
  ) as {
    exports: { "./client": { import?: string; types?: string } };
  };
  expect(packageJson.exports["./client"].import).toBe("./dist/client/index.js");
  expect(packageJson.exports["./client"].types).toBe("./dist/client/index.d.ts");
  rmSync(extractedRoot, { recursive: true, force: true });
  rmSync(tarball, { force: true });
});

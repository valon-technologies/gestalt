import { execFile } from "node:child_process";
import { mkdtemp, rm, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve("..");
const publishScript = path.join("scripts", "publish-sdk-api-docs.py");
const packageExistsScript = path.join("..", ".github", "scripts", "check-sdk-package-exists.py");

const tempRoot = await mkdtemp(path.join(tmpdir(), "gestalt-sdk-api-docs-"));

try {
  const docsRoot = path.join(tempRoot, "html");
  await mkdir(path.join(docsRoot, "_static"), { recursive: true });
  await writeFile(path.join(docsRoot, "index.html"), "<!doctype html><title>SDK</title>");
  await writeFile(path.join(docsRoot, "_static", "style.css"), "body { color: black; }");

  const pythonManifest = await manifest({
    language: "python",
    version: "0.0.1a2",
    sourceTag: "sdk/python/v0.0.1-alpha.2",
    sourceDir: docsRoot,
    currentLatestVersion: "0.0.1a1",
  });
  assert(pythonManifest.updatesLatest === true, "newer Python version should update latest");
  assert(
    pythonManifest.cleanupLatestPrefix === "api/python/latest/",
    "latest-updating Python manifest should clean the latest prefix",
  );
  assertKeys(pythonManifest, [
    "api/python/0.0.1a2/",
    "api/python/0.0.1a2/index.html",
    "api/python/0.0.1a2/_static/style.css",
    "api/python/latest/",
    "api/python/latest/index.html",
    "api/python/latest/_static/style.css",
    "api/python",
    "api/python/",
    "api/python/index.html",
    "api/python/latest.json",
  ]);
  assertCache(
    pythonManifest,
    "api/python/0.0.1a2/index.html",
    "public, max-age=31536000, immutable",
  );
  assertCache(pythonManifest, "api/python/latest/index.html", "public, max-age=60, must-revalidate");
  assertCache(pythonManifest, "api/python/index.html", "no-cache");
  assertCache(pythonManifest, "api/python/latest.json", "no-cache");

  const olderManifest = await manifest({
    language: "python",
    version: "0.0.1a1",
    sourceTag: "sdk/python/v0.0.1-alpha.1",
    sourceDir: docsRoot,
    currentLatestVersion: "0.0.1a2",
  });
  assert(olderManifest.updatesLatest === false, "older Python version must not update latest");
  assert(
    olderManifest.cleanupLatestPrefix === null,
    "non-latest Python manifest must not clean the latest prefix",
  );
  assertKeys(olderManifest, [
    "api/python/0.0.1a1/",
    "api/python/0.0.1a1/index.html",
    "api/python/0.0.1a1/_static/style.css",
  ]);

  const typescriptManifest = await manifest({
    language: "typescript",
    version: "0.0.1-alpha.10",
    sourceTag: "sdk/typescript/v0.0.1-alpha.10",
    sourceDir: docsRoot,
    currentLatestVersion: "0.0.1-alpha.9",
  });
  assert(typescriptManifest.updatesLatest === true, "semver prerelease comparison should work");
  assert(
    typescriptManifest.cleanupLatestPrefix === "api/typescript/latest/",
    "latest-updating TypeScript manifest should clean the latest prefix",
  );
  assertKeys(typescriptManifest, [
    "api/typescript/0.0.1-alpha.10/",
    "api/typescript/0.0.1-alpha.10/index.html",
    "api/typescript/0.0.1-alpha.10/_static/style.css",
    "api/typescript/latest/",
    "api/typescript/latest/index.html",
    "api/typescript/latest/_static/style.css",
    "api/typescript",
    "api/typescript/",
    "api/typescript/index.html",
    "api/typescript/latest.json",
  ]);

  for (const [language, sourceTag] of [
    ["go", "sdk/go/v0.0.1-alpha.10"],
    ["rust", "sdk/rust/v0.0.1-alpha.10"],
  ]) {
    const langManifest = await manifest({
      language,
      version: "0.0.1-alpha.10",
      sourceTag,
      sourceDir: docsRoot,
      currentLatestVersion: "0.0.1-alpha.9",
    });
    assert(langManifest.updatesLatest === true, `${language} semver comparison should work`);
    assert(
      langManifest.cleanupLatestPrefix === `api/${language}/latest/`,
      `latest-updating ${language} manifest should clean the latest prefix`,
    );
    assertKeys(langManifest, [
      `api/${language}/0.0.1-alpha.10/`,
      `api/${language}/0.0.1-alpha.10/index.html`,
      `api/${language}/0.0.1-alpha.10/_static/style.css`,
      `api/${language}/latest/`,
      `api/${language}/latest/index.html`,
      `api/${language}/latest/_static/style.css`,
      `api/${language}`,
      `api/${language}/`,
      `api/${language}/index.html`,
      `api/${language}/latest.json`,
    ]);
    assertCache(
      langManifest,
      `api/${language}/0.0.1-alpha.10/index.html`,
      "public, max-age=31536000, immutable",
    );
  }

  const pypiMetadata = path.join(tempRoot, "pypi.json");
  await writeFile(pypiMetadata, JSON.stringify({ releases: { "0.0.1a2": [] } }));
  await assertPackageExists(["pypi", "gestalt-sdk", "0.0.1a2", "--metadata-file", pypiMetadata], true);
  await assertPackageExists(["pypi", "gestalt-sdk", "0.0.1a3", "--metadata-file", pypiMetadata], false);

  const npmMetadata = path.join(tempRoot, "npm.json");
  await writeFile(
    npmMetadata,
    JSON.stringify({ versions: { "0.0.1-alpha.10": { name: "@valon-technologies/gestalt" } } }),
  );
  await assertPackageExists(
    ["npm", "@valon-technologies/gestalt", "0.0.1-alpha.10", "--metadata-file", npmMetadata],
    true,
  );
  await assertPackageExists(
    ["npm", "@valon-technologies/gestalt", "0.0.1-alpha.11", "--metadata-file", npmMetadata],
    false,
  );
} finally {
  await rm(tempRoot, { force: true, recursive: true });
}

async function manifest({
  language,
  version,
  sourceTag,
  sourceDir,
  currentLatestVersion,
}) {
  const { stdout } = await execFileAsync("python3", [
    publishScript,
    "--mode",
    "manifest",
    "--language",
    language,
    "--version",
    version,
    "--source-tag",
    sourceTag,
    "--source-dir",
    sourceDir,
    "--current-latest-version",
    currentLatestVersion,
    "--generated-at",
    "2026-01-01T00:00:00+00:00",
  ], { cwd: path.join(repoRoot, "docs") });
  return JSON.parse(stdout);
}

async function assertPackageExists(args, expected) {
  const { stdout } = await execFileAsync("python3", [packageExistsScript, ...args], {
    cwd: path.join(repoRoot, "docs"),
  });
  const actual = stdout.includes("exists=true");
  assert(actual === expected, `package exists check ${args.join(" ")} returned ${stdout}`);
}

function assertKeys(manifest, expectedKeys) {
  const actualKeys = manifest.objects.map((object) => object.key).sort();
  const expected = [...expectedKeys].sort();
  assert(
    JSON.stringify(actualKeys) === JSON.stringify(expected),
    `manifest keys mismatch:\nactual=${JSON.stringify(actualKeys, null, 2)}\nexpected=${JSON.stringify(expected, null, 2)}`,
  );
}

function assertCache(manifest, key, expectedCacheControl) {
  const object = manifest.objects.find((candidate) => candidate.key === key);
  assert(object, `missing manifest object ${key}`);
  assert(
    object.cacheControl === expectedCacheControl,
    `${key} cacheControl = ${object.cacheControl}, want ${expectedCacheControl}`,
  );
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

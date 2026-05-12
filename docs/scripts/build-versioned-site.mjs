import { spawnSync } from "node:child_process";
import {
  access,
  cp,
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { existsSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const docsRoot = path.resolve(scriptDir, "..");
const repoRoot = path.resolve(docsRoot, "..");
const defaultOut = path.join(docsRoot, "out");

const options = parseArgs(process.argv.slice(2));
const outDir = path.resolve(docsRoot, options.out ?? defaultOut);

main().catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exit(1);
});

async function main() {
  await ensureNodeModules();
  const tags = await selectedTags();
  const latestTag =
    options.latestTag ||
    (options.latestPolicy === "stable"
      ? chooseLatestStable(tags)
      : chooseLatestSupported(tags));
  if (!latestTag) {
    throw new Error(
      "no gestaltd/v* tag found for /latest; create a tag or pass --latest-tag for test builds",
    );
  }
  if (!parseTag(latestTag)) {
    throw new Error(`unsupported gestaltd tag format: ${latestTag}`);
  }
  if (!tags.includes(latestTag)) {
    tags.push(latestTag);
  }
  await assertDocsTrees(tags);
  tags.sort((left, right) => compareTagVersions(right, left));

  await rm(outDir, { force: true, recursive: true });
  await mkdir(outDir, { recursive: true });

  const manifest = {
    latest: manifestEntry(latestTag, "/latest", isStableTag(latestTag), true),
    versions: tags.map((tag) =>
      manifestEntry(tag, `/versions/${versionWithV(tag)}`, isStableTag(tag)),
    ),
  };

  const tempRoot = await mkdtemp(
    path.join(os.tmpdir(), `gestalt-docs-versioned-${process.pid}-`),
  );
  try {
    const rootBuild = await createBuildTree(tempRoot, "root", null);
    await runNextBuild(rootBuild, {
      basePath: "",
      label: "Unversioned",
      repositoryRef: "main",
      tag: "",
    });
    await copyRootOutputs(path.join(rootBuild, "out"), outDir);
    await rm(rootBuild, { force: true, recursive: true });

    const targets = [
      {
        tag: latestTag,
        basePath: "/latest",
        label: `Latest (${versionWithV(latestTag)})`,
      },
      ...tags.map((tag) => ({
        tag,
        basePath: `/versions/${versionWithV(tag)}`,
        label: versionWithV(tag),
      })),
    ];

    for (const target of targets) {
      const name = target.basePath.replace(/\//g, "_").replace(/^_/, "");
      const buildDir = await createBuildTree(tempRoot, name, target.tag);
      await runNextBuild(buildDir, {
        basePath: target.basePath,
        label: target.label,
        repositoryRef: target.tag,
        tag: target.tag,
      });
      const nestedPrefixOut = path.join(buildDir, "out", target.basePath.slice(1));
      const prefixOut = existsSync(nestedPrefixOut)
        ? nestedPrefixOut
        : path.join(buildDir, "out");
      const finalPrefix = path.join(outDir, target.basePath.slice(1));
      await rm(finalPrefix, { force: true, recursive: true });
      await mkdir(path.dirname(finalPrefix), { recursive: true });
      await cp(prefixOut, finalPrefix, { recursive: true });
      await rewriteVersionedPaths(finalPrefix, target.basePath);
      await rm(buildDir, { force: true, recursive: true });
    }

    await writeFile(
      path.join(outDir, "versions.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
    );
    await runPagefind(outDir);
    await assertNoUnversionedDocLinks(outDir);
    await assertFlightPayloadsUseUnprefixedInternalLinks(outDir);
  } finally {
    await rm(tempRoot, { force: true, recursive: true });
  }
}

function parseArgs(args) {
  const parsed = {
    includeAll: false,
    latestPolicy: "supported",
    latestTag: "",
    out: "",
    tags: [],
  };
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    switch (arg) {
      case "--include-all-gestaltd-tags":
        parsed.includeAll = true;
        break;
      case "--latest-policy":
        parsed.latestPolicy = needValue(args, ++index, arg);
        break;
      case "--latest-tag":
        parsed.latestTag = normalizeTag(needValue(args, ++index, arg));
        break;
      case "--out":
        parsed.out = needValue(args, ++index, arg);
        break;
      case "--tag":
        parsed.tags.push(normalizeTag(needValue(args, ++index, arg)));
        break;
      default:
        throw new Error(`unknown option: ${arg}`);
    }
  }
  if (!parsed.includeAll && parsed.tags.length === 0) {
    throw new Error("pass --include-all-gestaltd-tags or at least one --tag");
  }
  if (!["stable", "supported"].includes(parsed.latestPolicy)) {
    throw new Error("--latest-policy must be stable or supported");
  }
  return parsed;
}

function needValue(args, index, flag) {
  const value = args[index];
  if (!value || value.startsWith("--")) {
    throw new Error(`${flag} requires a value`);
  }
  return value;
}

async function selectedTags() {
  const tags = options.includeAll
    ? gitOutput(["tag", "--list", "gestaltd/v*"])
        .split("\n")
        .filter(Boolean)
    : [...options.tags];
  const normalized = [...new Set(tags.map(normalizeTag))];
  normalized.forEach((tag) => {
    if (!parseTag(tag)) {
      throw new Error(`unsupported gestaltd tag format: ${tag}`);
    }
  });
  return normalized;
}

async function assertDocsTrees(tags) {
  const missing = [];
  for (const tag of tags) {
    const result = spawnSync("git", ["cat-file", "-e", `${tag}:docs/content`], {
      cwd: repoRoot,
      encoding: "utf8",
    });
    if (result.status !== 0) {
      missing.push(tag);
    }
  }
  if (missing.length > 0) {
    throw new Error(
      `cannot build exact docs snapshots because these tags have no docs/content tree: ${missing.join(", ")}`,
    );
  }
}

function chooseLatestStable(tags) {
  const stable = tags.filter(isStableTag);
  stable.sort((left, right) => compareTagVersions(right, left));
  return stable[0] ?? "";
}

function chooseLatestSupported(tags) {
  const sorted = [...tags];
  sorted.sort((left, right) => compareTagVersions(right, left));
  return sorted[0] ?? "";
}

function manifestEntry(tag, pathPrefix, stable, latest = false) {
  const version = versionWithV(tag);
  return {
    label: latest ? "Latest" : version,
    pathPrefix,
    stable,
    tag,
    version,
  };
}

function normalizeTag(value) {
  if (value.startsWith("gestaltd/v")) {
    return value;
  }
  if (value.startsWith("v")) {
    return `gestaltd/${value}`;
  }
  return `gestaltd/v${value}`;
}

function versionWithV(tag) {
  return tag.replace(/^gestaltd\//, "");
}

function isStableTag(tag) {
  return parseTag(tag)?.prerelease.length === 0;
}

function parseTag(tag) {
  const match = tag.match(
    /^gestaltd\/v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/,
  );
  if (!match) {
    return null;
  }
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    prerelease: match[4] ? match[4].split(".") : [],
  };
}

function compareTagVersions(left, right) {
  const a = parseTag(left);
  const b = parseTag(right);
  for (const key of ["major", "minor", "patch"]) {
    if (a[key] !== b[key]) {
      return a[key] - b[key];
    }
  }
  if (a.prerelease.length === 0 && b.prerelease.length > 0) {
    return 1;
  }
  if (a.prerelease.length > 0 && b.prerelease.length === 0) {
    return -1;
  }
  const max = Math.max(a.prerelease.length, b.prerelease.length);
  for (let index = 0; index < max; index += 1) {
    const aPart = a.prerelease[index];
    const bPart = b.prerelease[index];
    if (aPart === undefined) {
      return -1;
    }
    if (bPart === undefined) {
      return 1;
    }
    if (aPart === bPart) {
      continue;
    }
    const aNumber = /^\d+$/.test(aPart) ? Number(aPart) : null;
    const bNumber = /^\d+$/.test(bPart) ? Number(bPart) : null;
    if (aNumber !== null && bNumber !== null) {
      return aNumber - bNumber;
    }
    if (aNumber !== null) {
      return -1;
    }
    if (bNumber !== null) {
      return 1;
    }
    return aPart.localeCompare(bPart);
  }
  return 0;
}

async function createBuildTree(tempRoot, name, tag) {
  const buildDir = path.join(tempRoot, name);
  await rm(buildDir, { force: true, recursive: true });
  await cp(docsRoot, buildDir, {
    filter: (source) => {
      const relative = path.relative(docsRoot, source);
      if (!relative) {
        return true;
      }
      const first = relative.split(path.sep)[0];
      return ![".next", "node_modules", "out"].includes(first);
    },
    recursive: true,
  });
  await symlink(
    path.join(docsRoot, "node_modules"),
    path.join(buildDir, "node_modules"),
    "dir",
  );
  if (tag) {
    await overlayTaggedDocs(buildDir, tag);
  }
  return buildDir;
}

async function overlayTaggedDocs(buildDir, tag) {
  const archiveRoot = path.join(buildDir, ".tagged-docs");
  const archivePath = path.join(buildDir, ".tagged-docs.tar");
  run("git", [
    "archive",
    "--format=tar",
    `--output=${archivePath}`,
    tag,
    "docs/content",
    "docs/components",
    "docs/public",
    "docs/globals.css",
  ]);
  await mkdir(archiveRoot, { recursive: true });
  run("tar", ["-xf", archivePath, "-C", archiveRoot]);
  const taggedDocs = path.join(archiveRoot, "docs");
  for (const name of ["content", "components", "public"]) {
    const taggedPath = path.join(taggedDocs, name);
    if (existsSync(taggedPath)) {
      await rm(path.join(buildDir, name), { force: true, recursive: true });
      await cp(taggedPath, path.join(buildDir, name), { recursive: true });
    }
  }
  const taggedGlobals = path.join(taggedDocs, "globals.css");
  if (existsSync(taggedGlobals)) {
    await cp(taggedGlobals, path.join(buildDir, "globals.css"));
  }
  await cp(
    path.join(docsRoot, "components", "VersionPicker.tsx"),
    path.join(buildDir, "components", "VersionPicker.tsx"),
  );
}

async function runNextBuild(buildDir, { basePath, label, repositoryRef, tag }) {
  await rm(path.join(buildDir, ".next"), { force: true, recursive: true });
  await rm(path.join(buildDir, "out"), { force: true, recursive: true });
  run(
    "npm",
    ["run", "build:single"],
    buildDir,
    {
      GESTALT_DOCS_BASE_PATH: basePath,
      GESTALT_DOCS_REPOSITORY_REF: repositoryRef,
      NEXT_PUBLIC_GESTALT_DOCS_BASE_PATH: basePath,
      NEXT_PUBLIC_GESTALT_DOCS_CURRENT_LABEL: label,
      NEXT_PUBLIC_GESTALT_DOCS_CURRENT_TAG: tag,
      NEXT_TELEMETRY_DISABLED: "1",
      NODE_ENV: "production",
    },
  );
}

async function copyRootOutputs(rootOut, finalOut) {
  const allowlist = [
    "404.html",
    "_next",
    "api",
    "favicon.svg",
    "fonts",
    "images",
    "install-gestalt.sh",
    "install-gestaltd.sh",
    "install.sh",
    "registry.html",
  ];
  for (const name of allowlist) {
    const source = path.join(rootOut, name);
    if (existsSync(source)) {
      await cp(source, path.join(finalOut, name), { recursive: true });
    }
  }
}

async function rewriteVersionedPaths(root, prefix) {
  const files = await walk(root);
  for (const file of files.filter((candidate) => candidate.endsWith(".html"))) {
    const original = await readFile(file, "utf8");
    const rewritten = rewriteHtmlDocumentPaths(original, prefix);
    if (rewritten !== original) {
      await writeFile(file, rewritten);
    }
  }
}

function rewriteHtmlDocumentPaths(html, prefix) {
  return rewriteNonScriptHtml(html, (chunk) =>
    chunk.replace(
      /\b(href|src|action)=(["'])\/([^"'\s>]*)/g,
      (match, attr, quote, rest) => {
        const absolute = `/${rest}`;
        return shouldPrefixPath(absolute, prefix)
          ? `${attr}=${quote}${prefix}${absolute}`
          : match;
      },
    ),
  );
}

function rewriteNonScriptHtml(html, rewriteChunk) {
  const scriptPattern = /<script\b[^>]*>[\s\S]*?<\/script>/gi;
  let rewritten = "";
  let lastIndex = 0;
  for (const match of html.matchAll(scriptPattern)) {
    rewritten += rewriteChunk(html.slice(lastIndex, match.index));
    rewritten += match[0];
    lastIndex = match.index + match[0].length;
  }
  rewritten += rewriteChunk(html.slice(lastIndex));
  return rewritten;
}

function shouldPrefixPath(value, prefix) {
  if (value.startsWith("//")) {
    return false;
  }
  let pathname;
  try {
    pathname = new URL(value, "https://gestaltd.ai").pathname;
  } catch {
    return false;
  }
  if (
    pathname === prefix ||
    pathname.startsWith(`${prefix}/`) ||
    pathname === "/latest" ||
    pathname.startsWith("/latest/") ||
    pathname.startsWith("/versions/")
  ) {
    return false;
  }
  const globalPrefixes = [
    "/api/",
    "/admin",
    "/favicon.svg",
    "/fonts/",
    "/install-gestalt.sh",
    "/install-gestaltd.sh",
    "/install.sh",
    "/mcp",
    "/registry",
    "/versions.json",
    "/_pagefind/",
  ];
  return !globalPrefixes.some(
    (globalPrefix) =>
      pathname === globalPrefix || pathname.startsWith(globalPrefix),
  );
}

async function assertNoUnversionedDocLinks(finalOut) {
  const versionRoots = [
    path.join(finalOut, "latest"),
    path.join(finalOut, "versions"),
  ];
  const files = [];
  for (const root of versionRoots) {
    if (existsSync(root)) {
      files.push(...(await walk(root)));
    }
  }
  const bad = [];
  const patterns = [
    /\b(?:href|src|action)=["']\/(?!latest(?:\/|["'])|versions(?:\/|["'])|api\/|admin(?:\/|["'])|favicon\.svg|fonts\/|install(?:-|\.sh)|mcp(?:\/|["'])|registry(?:\/|["'])|versions\.json|_pagefind\/)/g,
  ];
  for (const file of files.filter((candidate) => candidate.endsWith(".html"))) {
    const body = await readFile(file, "utf8");
    const visibleHtml = body.replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, "");
    const matches = patterns.flatMap((pattern) => visibleHtml.match(pattern) ?? []);
    if (matches.length > 0) {
      bad.push(`${path.relative(finalOut, file)}: ${matches.slice(0, 5).join(", ")}`);
    }
  }
  if (bad.length > 0) {
    throw new Error(
      `versioned docs contain unversioned internal links:\n${bad.join("\n")}`,
    );
  }
}

async function assertFlightPayloadsUseUnprefixedInternalLinks(finalOut) {
  const versionedRoots = await versionedOutputRoots(finalOut);
  const bad = [];
  for (const { root, prefix } of versionedRoots) {
    const files = await walk(root);
    for (const file of files.filter((candidate) => /\.(html|txt)$/.test(candidate))) {
      const body = await readFile(file, "utf8");
      const flightText = file.endsWith(".html")
        ? [...body.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/gi)]
            .map((match) => match[1])
            .join("\n")
        : body;
      for (const value of flightHrefValues(flightText)) {
        if (
          (value === prefix || value.startsWith(`${prefix}/`)) &&
          !value.startsWith(`${prefix}/_next/`)
        ) {
          bad.push(`${path.relative(finalOut, file)}: href ${value}`);
          break;
        }
      }
    }
  }
  if (bad.length > 0) {
    throw new Error(
      `versioned docs flight payloads contain base-prefixed internal links:\n${bad.join("\n")}`,
    );
  }
}

function flightHrefValues(text) {
  const values = [];
  const patterns = [
    /"href"\s*:\s*"([^"]+)"/g,
    /\\"href\\":\\"([^"\\]+)\\"/g,
  ];
  for (const pattern of patterns) {
    for (const match of text.matchAll(pattern)) {
      values.push(match[1]);
    }
  }
  return values;
}

async function versionedOutputRoots(finalOut) {
  const roots = [];
  const latest = path.join(finalOut, "latest");
  if (existsSync(latest)) {
    roots.push({ prefix: "/latest", root: latest });
  }
  const versions = path.join(finalOut, "versions");
  if (existsSync(versions)) {
    for (const entry of await readdir(versions, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        roots.push({
          prefix: `/versions/${entry.name}`,
          root: path.join(versions, entry.name),
        });
      }
    }
  }
  return roots;
}

async function runPagefind(finalOut) {
  run("npx", ["pagefind", "--site", finalOut, "--output-subdir", "_pagefind"], docsRoot);
}

async function walk(root) {
  const result = [];
  const entries = await readdir(root, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(root, entry.name);
    if (entry.isDirectory()) {
      result.push(...(await walk(fullPath)));
    } else if (entry.isFile()) {
      result.push(fullPath);
    }
  }
  return result;
}

async function ensureNodeModules() {
  try {
    await access(path.join(docsRoot, "node_modules"));
  } catch {
    throw new Error("docs/node_modules is missing; run npm ci in docs before building versioned docs");
  }
}

function gitOutput(args) {
  const result = spawnSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error(result.stderr || `git ${args.join(" ")} failed`);
  }
  return result.stdout.trim();
}

function run(command, args, cwd = repoRoot, env = {}) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...env },
    stdio: "inherit",
  });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed`);
  }
}

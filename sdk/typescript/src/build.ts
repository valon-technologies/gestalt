#!/usr/bin/env bun

/**
 * Build-time helpers for producing standalone Gestalt provider binaries.
 *
 * `gestaltd provider release` uses this entrypoint for TypeScript source
 * providers. It loads the configured provider target, bundles it with Bun, and
 * writes the executable artifact for the requested target platform.
 *
 * @module build
 */

import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { defaultProviderExportNames } from "./provider-kind.ts";
import { readSourceManifest } from "./manifest.ts";
import { parseProviderTarget, readPackageProviderTarget, resolveProviderModulePath, formatProviderTarget, type ProviderTarget } from "./target.ts";

/**
 * Command-line usage for the bundled build entrypoint.
 */
export const USAGE =
  "usage: bun run build.ts [ROOT PROVIDER_TARGET OUTPUT PROVIDER_NAME GOOS GOARCH]\n" +
  "  (zero args: derive ROOT/PROVIDER_TARGET/OUTPUT/PROVIDER_NAME/GOOS/GOARCH from manifest.yaml + package.json + env)";

/**
 * Parsed arguments for the build entrypoint.
 */
export type BuildArgs = {
  root: string;
  target: string;
  outputPath: string;
  providerName: string;
  goos: string;
  goarch: string;
  libc?: string;
  compileTarget?: string;
};

/**
 * CLI entrypoint that compiles a provider into a standalone Bun executable.
 *
 * With no positional args, derives every field from `manifest.yaml` +
 * `package.json` + the `GESTALT_TARGET_OS`/`GESTALT_TARGET_ARCH` env vars that
 * gestaltd injects. With six positional args, uses the explicit contract.
 */
export async function main(argv: string[] = process.argv.slice(2)): Promise<number> {
  const args = argv.length === 0 ? deriveBuildArgs(process.cwd()) : parseBuildArgs(argv);
  if (!args) {
    console.error(USAGE);
    return 2;
  }
  buildProviderBinary(args);
  return 0;
}

const TARGET_OS_ENV = "GESTALT_TARGET_OS";
const TARGET_ARCH_ENV = "GESTALT_TARGET_ARCH";
const TARGET_LIBC_ENV = "GESTALT_TARGET_LIBC";

/**
 * Derives build args from the source manifest + package metadata + env. Used
 * when the build entrypoint is invoked with zero positional args.
 */
export function deriveBuildArgs(root: string): BuildArgs | undefined {
  try {
    const manifest = readSourceManifest(root);
    const target = formatProviderTarget(readPackageProviderTarget(root));
    const source = manifest.source;
    if (!source) {
      throw new Error("manifest source is required");
    }
    const artifactPath = `.gestaltd/bin/${providerNameFromSource(source)}`;
    return {
      root,
      target,
      outputPath: artifactPath,
      providerName: providerNameFromSource(source),
      goos: targetGoos(),
      goarch: targetGoarch(),
      libc: targetLibc(),
    };
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    return undefined;
  }
}

function providerNameFromSource(source: string): string {
  return source.split("/").pop()!.trim();
}

function targetGoos(): string {
  const env = process.env[TARGET_OS_ENV];
  if (env && env.trim()) {
    return env.trim();
  }
  return process.platform === "win32" ? "windows" : process.platform;
}

const ARCH_ALIASES: Record<string, string> = {
  x64: "amd64",
  aarch64: "arm64",
};

function targetGoarch(): string {
  const env = process.env[TARGET_ARCH_ENV];
  if (env && env.trim()) {
    return env.trim();
  }
  return ARCH_ALIASES[process.arch] ?? process.arch;
}

// Optional libc selector for linux targets. gestaltd sets this to "musl" for the
// shipped artifact (the deploy runtime is musl) and to the build host's libc for
// the host-side catalog build, which must exec on the build machine. Empty/unset
// keeps the musl default.
function targetLibc(): string {
  return (process.env[TARGET_LIBC_ENV] ?? "").trim();
}

/**
 * Parses `gestalt-ts-build` CLI arguments.
 */
export function parseBuildArgs(argv: string[]): BuildArgs | undefined {
  if (argv.length !== 6) {
    return undefined;
  }
  return {
    root: argv[0]!,
    target: argv[1]!,
    outputPath: argv[2]!,
    providerName: argv[3]!,
    goos: argv[4]!,
    goarch: argv[5]!,
  };
}

/**
 * Bundles a provider into a standalone executable for the requested target.
 */
export function buildProviderBinary(args: BuildArgs): void {
  const root = resolve(args.root);
  const outputPath = resolve(args.outputPath);
  const target = parseProviderTarget(args.target);
  const workDir = mkdtempSync(join(tmpdir(), "gestalt-typescript-build-"));

  try {
    const wrapperPath = writeBundledWrapper(workDir, root, target, args.providerName);
    const bunCommand = bunBuildCommand(
      wrapperPath,
      outputPath,
      args.goos,
      args.goarch,
      args.libc ?? "",
      args.compileTarget,
    );
    const result = spawnSync(bunCommand.command, bunCommand.args, {
      cwd: root,
      stdio: "inherit",
    });
    if (result.status !== 0) {
      throw new Error(`bun build failed with status ${result.status ?? "unknown"}`);
    }
  } finally {
    rmSync(workDir, {
      recursive: true,
      force: true,
    });
  }
}

/**
 * Constructs the Bun command used to compile a provider binary.
 */
export function bunBuildCommand(
  wrapperPath: string,
  outputPath: string,
  goos: string,
  goarch: string,
  libc = "",
  compileTarget = bunTarget(goos, goarch, libc),
): { command: string; args: string[] } {
  return {
    command: resolveBunExecutable(),
    args: [
      "build",
      "--compile",
      "--target",
      compileTarget,
      "--outfile",
      outputPath,
      wrapperPath,
    ],
  };
}

/**
 * Maps a Go-style `GOOS` / `GOARCH` target into Bun's compile target format.
 */
export function bunTarget(goos: string, goarch: string, libc = "musl"): string {
  const key = `${goos}/${goarch}`;
  // linux defaults to musl (the deploy runtime); "glibc"/"gnu" selects the
  // glibc target so a binary built for catalog generation can exec on a glibc
  // build host. libc is ignored for non-linux targets.
  const gnu = libc === "glibc" || libc === "gnu";
  switch (key) {
    case "darwin/amd64":
      return "bun-darwin-x64";
    case "darwin/arm64":
      return "bun-darwin-arm64";
    case "linux/amd64":
      return gnu ? "bun-linux-x64" : "bun-linux-x64-musl";
    case "linux/arm64":
      return gnu ? "bun-linux-arm64" : "bun-linux-arm64-musl";
    case "windows/amd64":
      return "bun-windows-x64";
    case "windows/arm64":
      return "bun-windows-arm64";
    default:
      throw new Error(`unsupported Bun target for ${key}`);
  }
}

function writeBundledWrapper(
  workDir: string,
  root: string,
  target: ProviderTarget,
  providerName: string,
): string {
  const wrapperPath = join(workDir, "bundled-runtime.ts");
  const modulePath = JSON.stringify(resolveProviderModulePath(root, target));
  const runtimePath = JSON.stringify(resolve(import.meta.dir, "providers", "runtime.ts"));
  const exportName = target.exportName ? JSON.stringify(target.exportName) : "undefined";
  const source = `
import * as bundledModule from ${modulePath};
import { runBundledProvider } from ${runtimePath};

const candidate = ${
    target.exportName
      ? `bundledModule[${exportName}]`
      : defaultBundledCandidateExpression(target.kind)
  };
await runBundledProvider(candidate, ${JSON.stringify(target.kind)}, ${JSON.stringify(providerName)});
`;
  writeFileSync(wrapperPath, source, "utf8");
  return wrapperPath;
}

function defaultBundledCandidateExpression(kind: ProviderTarget["kind"]): string {
  return [...defaultProviderExportNames(kind), "default"]
    .map((exportName) => `Reflect.get(bundledModule, ${JSON.stringify(exportName)})`)
    .join(" ?? ");
}

function resolveBunExecutable(): string {
  const candidates = [
    process.env.GESTALT_BUN,
    join(homedir(), ".bun", "bin", "bun"),
    "bun",
  ].filter((value): value is string => Boolean(value));

  for (const candidate of candidates) {
    if (candidate === "bun") {
      return candidate;
    }
    if (existsSync(candidate)) {
      return candidate;
    }
  }
  return "bun";
}

if (import.meta.main) {
  void main().then(
    (code) => {
      process.exitCode = code;
    },
    (error: unknown) => {
      console.error(error instanceof Error ? error.stack ?? error.message : String(error));
      process.exitCode = 1;
    },
  );
}

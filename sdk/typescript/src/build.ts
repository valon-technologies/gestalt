import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { parseProviderTarget, resolveProviderModulePath, type ProviderTarget } from "./target.ts";

/**
 * Command-line usage for the bundled build entrypoint.
 */
export const USAGE =
  "usage: bun run build.ts ROOT PROVIDER_TARGET OUTPUT PROVIDER_NAME GOOS GOARCH";

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
};

/**
 * CLI entrypoint that compiles a provider into a standalone Bun executable.
 */
export async function main(argv: string[] = process.argv.slice(2)): Promise<number> {
  const args = parseBuildArgs(argv);
  if (!args) {
    console.error(USAGE);
    return 2;
  }
  buildProviderBinary(args);
  return 0;
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
    const bunCommand = bunBuildCommand(wrapperPath, outputPath, args.goos, args.goarch);
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
 * Backwards-compatible alias for integration provider builds.
 */
export const buildPluginBinary = buildProviderBinary;

/**
 * Constructs the Bun command used to compile a provider binary.
 */
export function bunBuildCommand(
  wrapperPath: string,
  outputPath: string,
  goos: string,
  goarch: string,
): { command: string; args: string[] } {
  return {
    command: resolveBunExecutable(),
    args: [
      "build",
      "--compile",
      "--target",
      bunTarget(goos, goarch),
      "--outfile",
      outputPath,
      wrapperPath,
    ],
  };
}

/**
 * Maps a Go-style `GOOS` / `GOARCH` target into Bun's compile target format.
 */
export function bunTarget(goos: string, goarch: string): string {
  const key = `${goos}/${goarch}`;
  switch (key) {
    case "darwin/amd64":
      return "bun-darwin-x64";
    case "darwin/arm64":
      return "bun-darwin-arm64";
    case "linux/amd64":
      return "bun-linux-x64";
    case "linux/arm64":
      return "bun-linux-arm64";
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
  const runtimePath = JSON.stringify(resolve(import.meta.dir, "runtime.ts"));
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
  switch (kind) {
    case "integration":
      return 'Reflect.get(bundledModule, "provider") ?? Reflect.get(bundledModule, "plugin") ?? bundledModule.default';
    case "auth":
      return 'Reflect.get(bundledModule, "auth") ?? Reflect.get(bundledModule, "provider") ?? bundledModule.default';
    case "cache":
      return 'Reflect.get(bundledModule, "cache") ?? Reflect.get(bundledModule, "provider") ?? bundledModule.default';
    case "secrets":
      return 'Reflect.get(bundledModule, "secrets") ?? Reflect.get(bundledModule, "provider") ?? bundledModule.default';
    case "s3":
      return 'Reflect.get(bundledModule, "s3") ?? Reflect.get(bundledModule, "provider") ?? bundledModule.default';
    case "telemetry":
      return 'Reflect.get(bundledModule, "telemetry") ?? Reflect.get(bundledModule, "provider") ?? bundledModule.default';
  }
  throw new Error(`unsupported provider kind: ${kind satisfies never}`);
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

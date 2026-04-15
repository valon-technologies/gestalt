import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { parseProviderTarget, resolveProviderModulePath, type ProviderTarget } from "./target.ts";

export const USAGE =
  "usage: bun run build.ts ROOT PROVIDER_TARGET OUTPUT PROVIDER_NAME GOOS GOARCH";

export type BuildArgs = {
  root: string;
  target: string;
  outputPath: string;
  providerName: string;
  goos: string;
  goarch: string;
};

export async function main(argv: string[] = process.argv.slice(2)): Promise<number> {
  const args = parseBuildArgs(argv);
  if (!args) {
    console.error(USAGE);
    return 2;
  }
  buildProviderBinary(args);
  return 0;
}

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

export const buildPluginBinary = buildProviderBinary;

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
      return "bundledModule.provider ?? bundledModule.plugin ?? bundledModule.default";
    case "auth":
      return "bundledModule.auth ?? bundledModule.provider ?? bundledModule.default";
    case "cache":
      return "bundledModule.cache ?? bundledModule.provider ?? bundledModule.default";
    case "secrets":
      return "bundledModule.secrets ?? bundledModule.provider ?? bundledModule.default";
    case "s3":
      return "bundledModule.s3 ?? bundledModule.provider ?? bundledModule.default";
    case "telemetry":
      return "bundledModule.telemetry ?? bundledModule.provider ?? bundledModule.default";
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

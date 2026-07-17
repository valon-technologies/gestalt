import { join } from "node:path";

import * as esbuild from "esbuild";

const packageRoot = join(import.meta.dir, "..");
const browserEntry = join(packageRoot, "dist", "index.js");

const NODE_BUILTINS = new Set([
  "assert",
  "async_hooks",
  "buffer",
  "child_process",
  "cluster",
  "crypto",
  "dgram",
  "diagnostics_channel",
  "dns",
  "events",
  "fs",
  "http",
  "http2",
  "https",
  "module",
  "net",
  "os",
  "path",
  "perf_hooks",
  "process",
  "readline",
  "stream",
  "string_decoder",
  "timers",
  "tls",
  "tty",
  "url",
  "util",
  "worker_threads",
  "zlib",
]);

export async function buildBrowserMetafile(
  entryPath: string,
): Promise<esbuild.Metafile> {
  const result = await esbuild.build({
    absWorkingDir: packageRoot,
    entryPoints: [entryPath],
    bundle: true,
    write: false,
    metafile: true,
    platform: "browser",
    format: "esm",
    logLevel: "silent",
  });
  if (!result.metafile) {
    throw new Error("esbuild metafile missing");
  }
  return result.metafile;
}

export function collectBundleViolations(metafile: esbuild.Metafile): string[] {
  const violations: string[] = [];
  const inputs = Object.entries(metafile.inputs);

  if (inputs.length < 20) {
    violations.push(
      `browser bundle graph too small (${inputs.length} modules); re-exports may not be followed`,
    );
  }

  for (const [inputPath, input] of inputs) {
    if (inputPath.includes("connect-node")) {
      violations.push(`forbidden connect-node module: ${inputPath}`);
    }
    if (/\/mount\.(?:js|ts)$/.test(inputPath) || /\/vite\.(?:js|d\.ts)$/.test(inputPath)) {
      violations.push(`browser root bundle must not include mount/vite: ${inputPath}`);
    }

    for (const imp of input.imports) {
      if (imp.path.startsWith("node:")) {
        violations.push(`forbidden node built-in: ${imp.path} (from ${inputPath})`);
        continue;
      }
      if (imp.path.includes("connect-node")) {
        violations.push(`forbidden connect-node import: ${imp.path} (from ${inputPath})`);
        continue;
      }
      const bare = imp.path.split("/")[0] ?? imp.path;
      if (NODE_BUILTINS.has(bare)) {
        violations.push(`forbidden Node built-in: ${imp.path} (from ${inputPath})`);
      }
    }
  }

  return violations;
}

if (import.meta.main) {
  const metafile = await buildBrowserMetafile(browserEntry);
  const violations = collectBundleViolations(metafile);
  if (violations.length > 0) {
    console.error("browser bundle violations:");
    for (const violation of violations) {
      console.error(violation);
    }
    process.exit(1);
  }
  console.log(
    `browser bundle ok (${Object.keys(metafile.inputs).length} modules)`,
  );
}

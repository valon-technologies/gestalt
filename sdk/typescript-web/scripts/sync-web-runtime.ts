import { cpSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

import { extractNativeTypes } from "./extract-native-types.ts";

const gestaltSrcRoot = join(import.meta.dir, "..", "..", "typescript", "src");
const webRuntimeRoot = join(import.meta.dir, "..", "src", "client", "runtime");

const restServiceModules = [
  "app",
  "agent",
  "authorization",
  "identity",
  "workflow",
] as const;

const runtimePaths = [
  "invoke_support.ts",
  "rpc_support.ts",
  "internal/gen",
  ...restServiceModules.map((base) => `internal/codec/${base}.ts`),
  "internal/codec/support.ts",
] as const;

rmSync(webRuntimeRoot, { recursive: true, force: true });
mkdirSync(webRuntimeRoot, { recursive: true });

for (const relativePath of runtimePaths) {
  const sourcePath = join(gestaltSrcRoot, relativePath);
  const targetPath = join(webRuntimeRoot, relativePath);
  if (statSync(sourcePath).isDirectory()) {
    cpSync(sourcePath, targetPath, { recursive: true });
    continue;
  }
  mkdirSync(dirname(targetPath), { recursive: true });
  writeFileSync(targetPath, readFileSync(sourcePath, "utf8"));
}

for (const base of restServiceModules) {
  const sourcePath = join(gestaltSrcRoot, `${base}.ts`);
  const targetPath = join(webRuntimeRoot, `${base}.ts`);
  writeFileSync(
    targetPath,
    extractNativeTypes(readFileSync(sourcePath, "utf8")),
  );
}

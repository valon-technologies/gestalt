import { cpSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

import { extractNativeTypes } from "./extract-native-types.ts";

const gestaltSrcRoot = join(import.meta.dir, "..", "..", "typescript", "src");
const webRuntimeRoot = join(import.meta.dir, "..", "src", "client", "runtime");

const runtimePaths = [
  "invoke_support.ts",
  "rpc_support.ts",
  "internal/codec/app.ts",
  "internal/codec/support.ts",
  "internal/gen",
] as const;

function patchRuntimeImports(source: string): string {
  return source.replaceAll("../../app.ts", "../../native-types.ts");
}

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
  writeFileSync(targetPath, patchRuntimeImports(readFileSync(sourcePath, "utf8")));
}

const appSource = readFileSync(join(gestaltSrcRoot, "app.ts"), "utf8");
writeFileSync(join(webRuntimeRoot, "native-types.ts"), extractNativeTypes(appSource));

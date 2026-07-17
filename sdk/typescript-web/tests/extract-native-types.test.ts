import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, test } from "bun:test";

import { extractNativeTypes } from "../scripts/extract-native-types.ts";

const appSourcePath = join(import.meta.dir, "..", "..", "typescript", "src", "app.ts");

describe("extractNativeTypes", () => {
  test("stops after a single-line export type before a class", () => {
    const source = [
      'import type { JsonObjectInput } from "./rpc_support.ts";',
      "",
      "export type Safe = string;",
      "export class ServerOnly {}",
      "",
    ].join("\n");

    const output = extractNativeTypes(source);

    expect(output).toContain("export type Safe = string;");
    expect(output).not.toContain("ServerOnly");
    expect(output).not.toContain("export class");
  });

  test("stops after export const ... as const on one line", () => {
    const source = [
      "export const Mode = { ON: 1 } as const;",
      "export class ServerOnly {}",
      "",
    ].join("\n");

    const output = extractNativeTypes(source);

    expect(output).toContain("export const Mode = { ON: 1 } as const;");
    expect(output).not.toContain("ServerOnly");
  });

  test("extracts interfaces and skips runtime exports from app.ts", () => {
    const source = readFileSync(appSourcePath, "utf8");
    const output = extractNativeTypes(source);

    expect(output).toContain("export interface OperationResult");
    expect(output).toContain("export type ConnectionMode = number;");
    expect(output).toContain("export const ConnectionMode = {");
    expect(output).not.toContain("export class App");
    expect(output).not.toContain("@connectrpc/connect");
    expect(output).not.toContain("createClient");
  });
});

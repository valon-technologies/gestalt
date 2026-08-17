import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";

import { compileApp } from "../src/compiler.ts";
import { ExperimentError, stableStringify, type AppManifest } from "../src/model.ts";

const fixture = join(import.meta.dir, "fixtures", "valid", "users.ts");
const sdkPath = join(import.meta.dir, "..", "src", "sdk.ts");

describe("canonical TypeScript contract extraction", () => {
  test("R-TYPE-01 lowers explicit public types into a canonical golden contract", async () => {
    const result = await compileApp(fixture, manifest("acme/users", "1.0.0"));
    expect(stableStringify(result.contract)).toMatchSnapshot();
    expect(result.contractDigest).toHaveLength(64);
    expect(result.clientTemplate).toContain("export type GetUserInput");
    expect(result.clientTemplate).toContain("export async function getUser");
  });

  test("R-TYPE-02 is reproducible across source paths and repeated builds", async () => {
    const firstDirectory = await mkdtemp(join(tmpdir(), "contract-first-"));
    const secondDirectory = await mkdtemp(join(tmpdir(), "contract-second-"));
    const source = validSource();
    const firstPath = join(firstDirectory, "app.ts");
    const secondPath = join(secondDirectory, "renamed.ts");
    await Promise.all([writeFile(firstPath, source), writeFile(secondPath, source)]);
    const release = manifest("acme/reproducible", "1.0.0");
    const [first, second] = await Promise.all([
      compileApp(firstPath, release),
      compileApp(secondPath, release),
    ]);
    expect(first.contract).toEqual(second.contract);
    expect(first.contractDigest).toBe(second.contractDigest);
    expect(first.clientTemplate).toBe(second.clientTemplate);
    expect(first.clientDigest).toBe(second.clientDigest);
    expect(first.sourceDigest).toBe(second.sourceDigest);
  });

  test.each([
    ["any", "type Input = { value: any };", "Input", "Promise<{ ok: boolean }>", "UNREPRESENTABLE_TYPE"],
    ["unknown", "type Input = { value: unknown };", "Input", "Promise<{ ok: boolean }>", "UNREPRESENTABLE_TYPE"],
    ["function field", "type Input = { callback: (value: string) => string };", "Input", "Promise<{ ok: boolean }>", "UNREPRESENTABLE_TYPE"],
    ["index signature", "type Input = { [key: string]: string };", "Input", "Promise<{ ok: boolean }>", "UNREPRESENTABLE_TYPE"],
    ["recursive type", "type Input = { next?: Input };", "Input", "Promise<{ ok: boolean }>", "RECURSIVE_TYPE"],
  ])(
    "R-TYPE-03 rejects %s instead of widening it",
    async (_label, declaration, input, output, code) => {
      await expectCompileError(sourceWithTypes(declaration, input, output), code);
    },
  );

  test("R-TYPE-03 rejects unresolved generics", async () => {
    const source = `
      import { app, tool } from ${JSON.stringify(sdkPath)};
      export default app({ tools: { echo: tool({
        handler: async <T>(input: { value: T }): Promise<{ value: T }> => input,
      }) } });
    `;
    await expectCompileError(source, "UNREPRESENTABLE_TYPE");
  });

  test("R-TYPE-04 requires explicit input and output annotations", async () => {
    const source = `
      import { app, tool } from ${JSON.stringify(sdkPath)};
      export default app({ tools: { echo: tool({
        handler: async (input: { value: string }) => ({ value: input.value }),
      }) } });
    `;
    await expectCompileError(source, "PUBLIC_TYPE_MUST_BE_EXPLICIT");
  });
});

function manifest(name: string, version: string): AppManifest {
  return { name, version, dependencies: {} };
}

function validSource(): string {
  return sourceWithTypes(
    "type Input = { value: string; optional?: boolean };",
    "Input",
    'Promise<{ result: string; state: "ready" | "done" }>',
  );
}

function sourceWithTypes(declaration: string, input: string, output: string): string {
  return `
    import { app, tool } from ${JSON.stringify(sdkPath)};
    ${declaration}
    export default app({ tools: { verify: tool({
      handler: async (input: ${input}): ${output} => ({ result: String(input), state: "ready", ok: true } as never),
    }) } });
  `;
}

async function expectCompileError(source: string, code: string): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), "contract-negative-"));
  await mkdir(directory, { recursive: true });
  const path = join(directory, "app.ts");
  await writeFile(path, source);
  try {
    await compileApp(path, manifest("acme/negative", "1.0.0"));
    throw new Error(`expected ${code}`);
  } catch (error) {
    expect(error).toBeInstanceOf(ExperimentError);
    expect((error as ExperimentError).code).toBe(code);
  }
}

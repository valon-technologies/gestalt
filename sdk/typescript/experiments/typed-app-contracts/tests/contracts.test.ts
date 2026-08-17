import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { describe, expect, test } from "bun:test";
import Ajv2020 from "ajv/dist/2020.js";

import { compileApp } from "../src/contracts.ts";
import { ExperimentError, stableStringify, type AppManifest } from "../src/model.ts";
import type { App, Tool } from "../src/sdk.ts";
import { linkZod, sdkPath } from "./helpers.ts";

const fixture = join(import.meta.dir, "fixtures", "valid", "users.ts");

describe("Zod-derived canonical contracts", () => {
  test("R-TYPE-01 lowers Zod contracts into a canonical golden contract and generated client", async () => {
    const result = await compileApp(fixture, manifest("acme/users", "1.0.0"));
    expect(stableStringify(result.contract)).toMatchSnapshot();
    expect(result.contractDigest).toHaveLength(64);
    expect(result.clientTemplate).toContain("export type GetUserInput");
    expect(result.clientTemplate).toContain("export async function getUser");
  });

  test("R-TYPE-01 keeps Zod and canonical JSON Schema validation conformant", async () => {
    const compilation = await compileApp(fixture, manifest("acme/users", "1.0.0"));
    const loaded = (await import(pathToFileURL(fixture).href)) as {
      default: App<Record<string, Tool<any, any>>>;
    };
    const tool = loaded.default.tools.getUser!;
    const ajv = new Ajv2020({ allErrors: true, strict: true });
    const validateInput = ajv.compile(compilation.contract.tools.getUser!.input);
    const validateOutput = ajv.compile(compilation.contract.tools.getUser!.output);
    const inputs = [
      { id: "42" },
      { id: "42", includeHistory: true },
      { id: 42 },
      { id: "42", extra: true },
      {},
    ];
    const outputs = [
      { id: "42", displayName: "Ada", status: "active", labels: [], score: 100 },
      { id: "42", displayName: "Ada", status: "unknown", labels: [], score: 100 },
      { id: "42", displayName: "Ada", status: "active", labels: [1], score: 100 },
      { id: "42", displayName: "Ada", status: "active", labels: [], score: 101 },
    ];
    for (const value of inputs) {
      expect(validateInput(value)).toBe((await tool.input.safeParseAsync(value)).success);
    }
    for (const value of outputs) {
      expect(validateOutput(value)).toBe((await tool.output.safeParseAsync(value)).success);
    }
  });

  test("R-TYPE-01 supports canonical recursive JSON values without accepting arbitrary recursion", async () => {
    const directory = await mkdtemp(join(tmpdir(), "zod-json-contract-"));
    await linkZod(directory);
    const path = join(directory, "app.ts");
    await writeFile(path, `
      import { z } from "zod";
      import { app, tool } from ${JSON.stringify(sdkPath)};
      export default app({ tools: { roundTrip: tool({
        input: z.strictObject({ value: z.json() }),
        output: z.strictObject({ value: z.json() }),
        handler: async (input) => input,
      }) } });
    `);
    const compilation = await compileApp(path, manifest("acme/json", "1.0.0"));
    const schema = compilation.contract.tools.roundTrip!.input;
    const validate = new Ajv2020({ allErrors: true, strict: true }).compile(schema);
    for (const value of [
      null,
      "value",
      42,
      ["nested", { valid: true }],
      { deeply: { nested: [1, 2, 3] } },
    ]) {
      expect(validate({ value })).toBe(true);
    }
    expect(validate({ value: undefined })).toBe(false);
    expect(compilation.clientTemplate).toContain("export type JsonValue");
    expect(compilation.clientTemplate).toContain('"value": JsonValue');
  });

  test("R-TYPE-02 is reproducible across source paths and repeated builds", async () => {
    const firstDirectory = await mkdtemp(join(tmpdir(), "zod-contract-first-"));
    const secondDirectory = await mkdtemp(join(tmpdir(), "zod-contract-second-"));
    await Promise.all([linkZod(firstDirectory), linkZod(secondDirectory)]);
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
    ["any", "z.any()"],
    ["unknown", "z.unknown()"],
    ["non-strict objects", "z.object({ value: z.string() })"],
    ["custom refinements", "z.string().refine((value) => value.length > 1)"],
    ["value overwrites", "z.string().trim()"],
    ["transforms", "z.string().transform((value) => value.length)"],
    ["coercion", "z.coerce.number()"],
    ["defaults", 'z.string().default("value")'],
    ["catch fallbacks", 'z.string().catch("value")'],
    ["dates", "z.date()"],
    ["open records", "z.record(z.string(), z.string())"],
    ["tuples without portable exact-length output", "z.tuple([z.string(), z.number()])"],
  ])("R-TYPE-03 rejects %s outside the public Zod profile", async (_label, input) => {
    await expectCompileError(sourceWithInput(input), "UNSUPPORTED_PUBLIC_SCHEMA");
  });

  test("R-TYPE-04 infers handler inputs and outputs from the Zod contracts", async () => {
    const source = `
      import { z } from "zod";
      import { app, tool } from ${JSON.stringify(sdkPath)};
      export default app({ tools: { broken: tool({
        input: z.strictObject({ id: z.string() }),
        output: z.strictObject({ id: z.string() }),
        handler: async (input) => ({ id: input.id.length }),
      }) } });
    `;
    await expectCompileError(source, "TYPESCRIPT_ERROR");
  });
});

function manifest(name: string, version: string): AppManifest {
  return { name, version, dependencies: {} };
}

function validSource(): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};
    const Input = z.strictObject({ value: z.string(), optional: z.boolean().optional() });
    const Output = z.strictObject({ result: z.string(), state: z.enum(["ready", "done"]) });
    export default app({ tools: { verify: tool({
      input: Input,
      output: Output,
      handler: async (input) => ({ result: input.value, state: "ready" as const }),
    }) } });
  `;
}

function sourceWithInput(input: string): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};
    export default app({ tools: { verify: tool({
      input: ${input},
      output: z.strictObject({ ok: z.boolean() }),
      handler: async () => ({ ok: true }),
    }) } });
  `;
}

async function expectCompileError(source: string, code: string): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), "zod-contract-negative-"));
  await linkZod(directory);
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

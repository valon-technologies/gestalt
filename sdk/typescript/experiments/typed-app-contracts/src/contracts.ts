import { createRequire } from "node:module";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import ts from "typescript";
import { z } from "zod";

import {
  ADAPTER_VERSION,
  ExperimentError,
  FORMAT_VERSION,
  digest,
  stableStringify,
  validateManifest,
  type AppContract,
  type AppManifest,
  type CanonicalJsonSchema,
} from "./model.ts";
import type { App, Tool } from "./sdk.ts";

const require = createRequire(import.meta.url);
const zodVersion = (require("zod/package.json") as { version: string }).version;
const jsonSchemaOptions = {
  target: "draft-2020-12",
  io: "input",
  unrepresentable: "throw",
  cycles: "ref",
  reused: "inline",
} as const;
const canonicalJsonValueSchema = stableStringify(z.toJSONSchema(z.json(), jsonSchemaOptions));

export interface Compilation {
  contract: AppContract;
  contractDigest: string;
  clientTemplate: string;
  clientDigest: string;
  sourceDigest: string;
  importedAliases: string[];
}

export async function compileApp(entrypoint: string, manifest: AppManifest): Promise<Compilation> {
  validateManifest(manifest);
  const sourceText = await readFile(entrypoint, "utf8");
  const sourceDigest = digest(sourceText);
  const source = typecheck(entrypoint);
  const importedAliases = collectAppImports(source);
  verifyDependencyUsage(importedAliases, manifest);

  const moduleUrl = pathToFileURL(entrypoint);
  moduleUrl.searchParams.set("source", sourceDigest);
  const module = (await import(moduleUrl.href)) as { default?: App<Record<string, Tool<any, any>>> };
  if (!module.default?.tools) {
    throw new ExperimentError("APP_SHAPE", "the default export must be app({ tools: ... })");
  }

  const tools: AppContract["tools"] = {};
  for (const [name, tool] of Object.entries(module.default.tools).sort(([a], [b]) => a.localeCompare(b))) {
    if (!/^[$A-Z_a-z][$\w]*$/.test(name)) {
      throw new ExperimentError("UNSUPPORTED_TOOL_NAME", `${name} is not a TypeScript identifier`);
    }
    assertPublicZodSchema(tool.input, `${name}.input`, new Set());
    assertPublicZodSchema(tool.output, `${name}.output`, new Set());
    const input = lowerZodSchema(tool.input, `${name}.input`);
    const output = lowerZodSchema(tool.output, `${name}.output`);
    tools[name] = {
      description: tool.description.trim(),
      input,
      output,
      digest: digest(stableStringify({ input, output })),
    };
  }
  if (Object.keys(tools).length === 0) {
    throw new ExperimentError("EMPTY_APP", "an app must publish at least one tool");
  }

  const contract: AppContract = {
    formatVersion: FORMAT_VERSION,
    app: { name: manifest.name, version: manifest.version },
    compiler: { adapter: ADAPTER_VERSION, typescript: ts.version, zod: zodVersion },
    tools,
  };
  const clientTemplate = generateClientTemplate(contract);
  return {
    contract,
    contractDigest: digest(stableStringify(contract)),
    clientTemplate,
    clientDigest: digest(clientTemplate),
    sourceDigest,
    importedAliases,
  };
}

function typecheck(entrypoint: string): ts.SourceFile {
  const program = ts.createProgram([entrypoint], {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    strict: true,
    noEmit: true,
    allowImportingTsExtensions: true,
    verbatimModuleSyntax: true,
    exactOptionalPropertyTypes: true,
    noUncheckedIndexedAccess: true,
    skipLibCheck: true,
    types: ["bun"],
  });
  const source = program.getSourceFile(entrypoint);
  if (!source) throw new ExperimentError("SOURCE_NOT_FOUND", entrypoint);
  const diagnostics = ts.getPreEmitDiagnostics(program);
  if (diagnostics.length > 0) {
    throw new ExperimentError(
      "TYPESCRIPT_ERROR",
      diagnostics
        .map((diagnostic) => ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"))
        .join("\n"),
    );
  }
  return source;
}

function collectAppImports(source: ts.SourceFile): string[] {
  const aliases = new Set<string>();
  const record = (moduleName: string): void => {
    const match = moduleName.match(/^@gestalt\/apps\/([a-z][a-zA-Z0-9]*)$/);
    if (match?.[1]) aliases.add(match[1]);
  };
  const visit = (node: ts.Node): void => {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      record(node.moduleSpecifier.text);
    } else if (ts.isExportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      record(node.moduleSpecifier.text);
    } else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      throw new ExperimentError(
        "DYNAMIC_APP_IMPORT",
        "dynamic import() is not valid dependency evidence; use a static @gestalt/apps import",
      );
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return [...aliases].sort();
}

function verifyDependencyUsage(importedAliases: string[], manifest: AppManifest): void {
  const imported = new Set(importedAliases);
  for (const alias of imported) {
    if (!manifest.dependencies[alias]) {
      throw new ExperimentError("UNDECLARED_DEPENDENCY", `import ${alias} is absent from the manifest`);
    }
  }
  for (const alias of Object.keys(manifest.dependencies)) {
    if (!imported.has(alias)) {
      throw new ExperimentError("UNUSED_DEPENDENCY", `manifest dependency ${alias} is not imported`);
    }
  }
}

function assertPublicZodSchema(schema: z.ZodType, path: string, ancestors: Set<object>): void {
  const internal = schema as unknown as { _zod?: { def?: Record<string, unknown> } };
  const definition = internal._zod?.def;
  if (!definition || typeof definition.type !== "string") {
    throw new ExperimentError("INVALID_ZOD_SCHEMA", `${path} is not a Zod 4 schema`);
  }
  if (ancestors.has(schema)) {
    throw new ExperimentError("UNSUPPORTED_PUBLIC_SCHEMA", `${path} is recursive`);
  }
  if (definition.coerce === true) {
    throw new ExperimentError("UNSUPPORTED_PUBLIC_SCHEMA", `${path} uses coercion`);
  }
  const checks = Array.isArray(definition.checks) ? definition.checks : [];
  for (const check of checks) {
    const checkDefinition = (check as { _zod?: { def?: Record<string, unknown> } })._zod?.def;
    if (
      checkDefinition?.check === "custom" ||
      checkDefinition?.type === "custom" ||
      checkDefinition?.check === "overwrite" ||
      checkDefinition?.type === "overwrite"
    ) {
      throw new ExperimentError("UNSUPPORTED_PUBLIC_SCHEMA", `${path} uses custom runtime behavior`);
    }
  }

  ancestors.add(schema);
  try {
    switch (definition.type) {
      case "string":
      case "number":
      case "boolean":
      case "null":
      case "literal":
      case "enum":
        return;
      case "array":
        assertPublicZodSchema(definition.element as z.ZodType, `${path}[]`, ancestors);
        return;
      case "optional":
      case "nullable":
        assertPublicZodSchema(definition.innerType as z.ZodType, path, ancestors);
        return;
      case "union":
        for (const [index, option] of (definition.options as z.ZodType[]).entries()) {
          assertPublicZodSchema(option, `${path}.variant[${index}]`, ancestors);
        }
        return;
      case "lazy": {
        const converted = z.toJSONSchema(schema, jsonSchemaOptions);
        if (stableStringify(converted) !== canonicalJsonValueSchema) {
          throw new ExperimentError(
            "UNSUPPORTED_PUBLIC_SCHEMA",
            `${path} uses recursion other than Zod's canonical JSON value schema`,
          );
        }
        return;
      }
      case "object": {
        const catchall = definition.catchall as
          | { _zod?: { def?: Record<string, unknown> } }
          | undefined;
        if (catchall?._zod?.def?.type !== "never") {
          throw new ExperimentError(
            "UNSUPPORTED_PUBLIC_SCHEMA",
            `${path} must use z.strictObject() so unknown fields are rejected`,
          );
        }
        for (const [name, property] of Object.entries(definition.shape as Record<string, z.ZodType>)) {
          assertPublicZodSchema(property, `${path}.${name}`, ancestors);
        }
        return;
      }
      default:
        throw new ExperimentError(
          "UNSUPPORTED_PUBLIC_SCHEMA",
          `${path} uses unsupported Zod type ${definition.type}`,
        );
    }
  } finally {
    ancestors.delete(schema);
  }
}

function lowerZodSchema(schema: z.ZodType, path: string): CanonicalJsonSchema {
  let converted: unknown;
  try {
    converted = z.toJSONSchema(schema, {
      ...jsonSchemaOptions,
    });
  } catch (error) {
    throw new ExperimentError(
      "UNSUPPORTED_PUBLIC_SCHEMA",
      `${path} cannot become JSON Schema: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  const canonical = JSON.parse(JSON.stringify(converted)) as unknown;
  if (typeof canonical !== "object" || Array.isArray(canonical) || canonical === null) {
    throw new ExperimentError("INVALID_JSON_SCHEMA", `${path} did not produce JSON data`);
  }
  return canonical as CanonicalJsonSchema;
}

function generateClientTemplate(contract: AppContract): string {
  const declarations: string[] = [
    "// Generated from the immutable Zod-derived Gestalt contract. Do not edit.",
    "export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };",
    "",
    "declare global {",
    "  var __gestaltExperimentInvoke: ((alias: string, tool: string, input: unknown, digest: string) => Promise<unknown>) | undefined;",
    "}",
    "",
  ];
  for (const [toolName, toolContract] of Object.entries(contract.tools)) {
    const stem = toolName[0]!.toUpperCase() + toolName.slice(1);
    declarations.push(`export type ${stem}Input = ${schemaToTypeScript(toolContract.input)};`);
    declarations.push(`export type ${stem}Output = ${schemaToTypeScript(toolContract.output)};`);
    declarations.push(
      `export async function ${toolName}(input: ${stem}Input): Promise<${stem}Output> {`,
      "  const invoke = globalThis.__gestaltExperimentInvoke;",
      '  if (!invoke) throw new Error("Gestalt invocation transport is not installed");',
      `  return await invoke("__GESTALT_ALIAS__", ${JSON.stringify(toolName)}, input, ${JSON.stringify(toolContract.digest)}) as ${stem}Output;`,
      "}",
      "",
    );
  }
  declarations.push("export {};", "");
  return declarations.join("\n");
}

function schemaToTypeScript(schema: CanonicalJsonSchema): string {
  if (typeof schema.$ref === "string") return "JsonValue";
  if ("const" in schema) return JSON.stringify(schema.const);
  if (Array.isArray(schema.enum)) return schema.enum.map((value) => JSON.stringify(value)).join(" | ");
  if (Array.isArray(schema.anyOf)) {
    return schema.anyOf.map((option) => schemaToTypeScript(option as CanonicalJsonSchema)).join(" | ");
  }
  switch (schema.type) {
    case "string": return "string";
    case "number":
    case "integer": return "number";
    case "boolean": return "boolean";
    case "null": return "null";
    case "array": return `Array<${schemaToTypeScript(schema.items as CanonicalJsonSchema)}>`;
    case "object": {
      if (schema.additionalProperties && typeof schema.additionalProperties === "object") {
        return `{ [key: string]: ${schemaToTypeScript(schema.additionalProperties as CanonicalJsonSchema)} }`;
      }
      const required = new Set(Array.isArray(schema.required) ? schema.required : []);
      const properties = Object.entries(schema.properties ?? {}).map(
        ([name, property]) =>
          `${JSON.stringify(name)}${required.has(name) ? "" : "?"}: ${schemaToTypeScript(property as CanonicalJsonSchema)};`,
      );
      return `{ ${properties.join(" ")} }`;
    }
    default:
      throw new ExperimentError("CLIENT_GENERATION_FAILED", `unsupported schema type ${String(schema.type)}`);
  }
}

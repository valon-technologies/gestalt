import { readFile } from "node:fs/promises";
import ts from "typescript";

import {
  EXTRACTOR_VERSION,
  ExperimentError,
  FORMAT_VERSION,
  digest,
  stableStringify,
  validateManifest,
  type AppContract,
  type AppManifest,
  type WireSchema,
} from "./model.ts";

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
  if (!source) {
    throw new ExperimentError("SOURCE_NOT_FOUND", entrypoint);
  }
  const diagnostics = ts.getPreEmitDiagnostics(program);
  if (diagnostics.length > 0) {
    throw new ExperimentError("TYPESCRIPT_ERROR", formatDiagnostics(diagnostics));
  }

  const importedAliases = collectAppImports(source);
  verifyDependencyUsage(importedAliases, manifest);
  const checker = program.getTypeChecker();
  const toolsNode = findToolsObject(source);
  const tools: AppContract["tools"] = {};

  for (const property of toolsNode.properties) {
    if (!ts.isPropertyAssignment(property)) {
      throw new ExperimentError(
        "UNSUPPORTED_TOOL_DECLARATION",
        "tools must be ordinary named properties",
      );
    }
    const name = propertyName(property.name);
    if (!/^[$A-Z_a-z][$\w]*$/.test(name)) {
      throw new ExperimentError("UNSUPPORTED_TOOL_NAME", `${name} is not a TypeScript identifier`);
    }
    if (!ts.isCallExpression(property.initializer) || callName(property.initializer) !== "tool") {
      throw new ExperimentError("UNSUPPORTED_TOOL_DECLARATION", `${name} must call tool({...})`);
    }
    const definition = property.initializer.arguments[0];
    if (!definition || !ts.isObjectLiteralExpression(definition)) {
      throw new ExperimentError("UNSUPPORTED_TOOL_DECLARATION", `${name} must pass an object literal`);
    }
    const handlerProperty = definition.properties.find(
      (candidate): candidate is ts.PropertyAssignment =>
        ts.isPropertyAssignment(candidate) && propertyName(candidate.name) === "handler",
    );
    if (!handlerProperty || !isFunction(handlerProperty.initializer)) {
      throw new ExperimentError("MISSING_HANDLER", `${name} needs an inline function handler`);
    }
    const handler = handlerProperty.initializer;
    if (handler.parameters.length !== 1 || !handler.parameters[0]?.type) {
      throw new ExperimentError(
        "PUBLIC_TYPE_MUST_BE_EXPLICIT",
        `${name} must explicitly annotate its single input parameter`,
      );
    }
    if (!handler.type) {
      throw new ExperimentError(
        "PUBLIC_TYPE_MUST_BE_EXPLICIT",
        `${name} must explicitly annotate its return type`,
      );
    }
    const signature = checker.getSignatureFromDeclaration(handler);
    if (!signature) {
      throw new ExperimentError("INVALID_HANDLER", `cannot resolve ${name}'s signature`);
    }
    const inputType = checker.getTypeFromTypeNode(handler.parameters[0].type);
    const declaredOutput = checker.getTypeFromTypeNode(handler.type);
    const outputType = checker.getAwaitedType(declaredOutput) ?? declaredOutput;
    const input = lowerType(checker, inputType, `${name}.input`, new Set());
    const output = lowerType(checker, outputType, `${name}.output`, new Set());
    const description = readDescription(definition);
    tools[name] = {
      description,
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
    compiler: { extractor: EXTRACTOR_VERSION, typescript: ts.version },
    tools: Object.fromEntries(Object.entries(tools).sort(([a], [b]) => a.localeCompare(b))),
  };
  const clientTemplate = generateClientTemplate(contract);
  return {
    contract,
    contractDigest: digest(stableStringify(contract)),
    clientTemplate,
    clientDigest: digest(clientTemplate),
    sourceDigest: digest(sourceText),
    importedAliases,
  };
}

function formatDiagnostics(diagnostics: readonly ts.Diagnostic[]): string {
  return diagnostics
    .map((diagnostic) => {
      const message = ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n");
      if (!diagnostic.file || diagnostic.start === undefined) return message;
      const position = diagnostic.file.getLineAndCharacterOfPosition(diagnostic.start);
      return `${diagnostic.file.fileName}:${position.line + 1}:${position.character + 1}: ${message}`;
    })
    .join("\n");
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
    } else if (ts.isImportTypeNode(node) && ts.isLiteralTypeNode(node.argument) && ts.isStringLiteral(node.argument.literal)) {
      record(node.argument.literal.text);
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

function findToolsObject(source: ts.SourceFile): ts.ObjectLiteralExpression {
  const assignment = source.statements.find(
    (statement): statement is ts.ExportAssignment => ts.isExportAssignment(statement),
  );
  if (!assignment || !ts.isCallExpression(assignment.expression) || callName(assignment.expression) !== "app") {
    throw new ExperimentError("APP_SHAPE", "the default export must be app({...})");
  }
  const definition = assignment.expression.arguments[0];
  if (!definition || !ts.isObjectLiteralExpression(definition)) {
    throw new ExperimentError("APP_SHAPE", "app must receive an object literal");
  }
  const tools = definition.properties.find(
    (property): property is ts.PropertyAssignment =>
      ts.isPropertyAssignment(property) && propertyName(property.name) === "tools",
  );
  if (!tools || !ts.isObjectLiteralExpression(tools.initializer)) {
    throw new ExperimentError("APP_SHAPE", "app.tools must be an object literal");
  }
  return tools.initializer;
}

function callName(call: ts.CallExpression): string | undefined {
  return ts.isIdentifier(call.expression) ? call.expression.text : undefined;
}

function propertyName(name: ts.PropertyName): string {
  if (ts.isIdentifier(name) || ts.isStringLiteral(name) || ts.isNumericLiteral(name)) return name.text;
  throw new ExperimentError("UNSUPPORTED_PROPERTY", "computed property names are not deterministic");
}

function isFunction(node: ts.Expression): node is ts.ArrowFunction | ts.FunctionExpression {
  return ts.isArrowFunction(node) || ts.isFunctionExpression(node);
}

function readDescription(definition: ts.ObjectLiteralExpression): string {
  const property = definition.properties.find(
    (candidate): candidate is ts.PropertyAssignment =>
      ts.isPropertyAssignment(candidate) && propertyName(candidate.name) === "description",
  );
  if (!property) return "";
  if (!ts.isStringLiteralLike(property.initializer)) {
    throw new ExperimentError("NON_CONSTANT_METADATA", "tool descriptions must be string literals");
  }
  return property.initializer.text.trim();
}

function lowerType(
  checker: ts.TypeChecker,
  type: ts.Type,
  path: string,
  ancestors: Set<ts.Type>,
  allowUndefined = false,
): WireSchema {
  if (type.flags & (ts.TypeFlags.Any | ts.TypeFlags.Unknown | ts.TypeFlags.Never | ts.TypeFlags.TypeParameter)) {
    throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} contains ${checker.typeToString(type)}`);
  }
  if (type.flags & ts.TypeFlags.Undefined) {
    if (allowUndefined) throw new ExperimentError("INTERNAL", `${path} retained undefined`);
    throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} contains undefined outside an optional property`);
  }
  if (type.isStringLiteral()) return { kind: "literal", value: type.value };
  if (type.isNumberLiteral()) return { kind: "literal", value: type.value };
  if (type.flags & ts.TypeFlags.BooleanLiteral) {
    return { kind: "literal", value: (type as { intrinsicName?: string }).intrinsicName === "true" };
  }
  if (type.flags & ts.TypeFlags.String) return { kind: "string" };
  if (type.flags & ts.TypeFlags.Number) return { kind: "number" };
  if (type.flags & ts.TypeFlags.Boolean) return { kind: "boolean" };
  if (type.flags & ts.TypeFlags.Null) return { kind: "null" };

  if (type.isUnion()) {
    const hasUndefined = type.types.some((variant) => Boolean(variant.flags & ts.TypeFlags.Undefined));
    if (hasUndefined && !allowUndefined) {
      throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} contains undefined outside an optional property`);
    }
    const variants = type.types
      .filter((variant) => !(variant.flags & ts.TypeFlags.Undefined))
      .map((variant) => lowerType(checker, variant, path, ancestors));
    const unique = new Map(variants.map((variant) => [stableStringify(variant), variant]));
    const sorted = [...unique.entries()].sort(([left], [right]) => left.localeCompare(right)).map(([, value]) => value);
    if (sorted.length === 0) {
      throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} has no wire-visible variants`);
    }
    return sorted.length === 1 ? sorted[0]! : { kind: "union", variants: sorted };
  }

  if (checker.isTupleType(type)) {
    const items = checker
      .getTypeArguments(type as ts.TypeReference)
      .map((item, index) => lowerType(checker, item, `${path}[${index}]`, ancestors));
    return { kind: "tuple", items };
  }
  if (checker.isArrayType(type) || type.getSymbol()?.getName() === "ReadonlyArray") {
    const item = checker.getTypeArguments(type as ts.TypeReference)[0];
    if (!item) throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} has an unresolved array item`);
    return { kind: "array", items: lowerType(checker, item, `${path}[]`, ancestors) };
  }

  if (!(type.flags & ts.TypeFlags.Object)) {
    throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} uses ${checker.typeToString(type)}`);
  }
  if (ancestors.has(type)) {
    throw new ExperimentError("RECURSIVE_TYPE", `${path} is recursive`);
  }
  const symbol = type.getSymbol();
  if (symbol?.declarations?.some((declaration) => ts.isClassDeclaration(declaration))) {
    throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} uses class ${symbol.getName()}`);
  }
  if (type.getCallSignatures().length > 0 || type.getConstructSignatures().length > 0) {
    throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} is callable or constructable`);
  }
  if (checker.getIndexInfosOfType(type).length > 0) {
    throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path} has an index signature`);
  }

  ancestors.add(type);
  try {
    const properties: Record<string, { optional: boolean; schema: WireSchema }> = {};
    for (const property of checker.getPropertiesOfType(type).sort((a, b) => a.getName().localeCompare(b.getName()))) {
      const declaration = property.valueDeclaration ?? property.declarations?.[0];
      if (!declaration) {
        throw new ExperimentError("UNREPRESENTABLE_TYPE", `${path}.${property.getName()} is unresolved`);
      }
      const optional = Boolean(property.flags & ts.SymbolFlags.Optional);
      const propertyType = checker.getTypeOfSymbolAtLocation(property, declaration);
      properties[property.getName()] = {
        optional,
        schema: lowerType(checker, propertyType, `${path}.${property.getName()}`, ancestors, optional),
      };
    }
    return { kind: "object", additionalProperties: false, properties };
  } finally {
    ancestors.delete(type);
  }
}

export function generateClientTemplate(contract: AppContract): string {
  const declarations: string[] = [
    "// Generated by the typed-app-contracts experiment. Do not edit.",
    "declare global {",
    "  var __gestaltExperimentInvoke: ((alias: string, tool: string, input: unknown, digest: string) => Promise<unknown>) | undefined;",
    "}",
    "",
  ];
  for (const [toolName, toolContract] of Object.entries(contract.tools)) {
    const stem = upperFirst(toolName);
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

function upperFirst(value: string): string {
  return value[0]!.toUpperCase() + value.slice(1);
}

function schemaToTypeScript(schema: WireSchema): string {
  switch (schema.kind) {
    case "string": return "string";
    case "number": return "number";
    case "boolean": return "boolean";
    case "null": return "null";
    case "literal": return JSON.stringify(schema.value);
    case "array": return `Array<${schemaToTypeScript(schema.items)}>`;
    case "tuple": return `readonly [${schema.items.map(schemaToTypeScript).join(", ")}]`;
    case "union": return schema.variants.map(schemaToTypeScript).join(" | ");
    case "object": {
      const properties = Object.entries(schema.properties).map(
        ([name, property]) =>
          `${JSON.stringify(name)}${property.optional ? "?" : ""}: ${schemaToTypeScript(property.schema)};`,
      );
      return `{ ${properties.join(" ")} }`;
    }
  }
}

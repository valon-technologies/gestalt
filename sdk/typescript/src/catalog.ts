import { writeFileSync } from "node:fs";

import YAML from "yaml";

import type { Schema } from "./schema.ts";

export interface CatalogParameter {
  name: string;
  type: string;
  description?: string;
  required?: boolean;
  default?: unknown;
}

export interface CatalogSchema {
  type: string;
  description?: string;
  default?: unknown;
  properties?: Record<string, CatalogSchema>;
  required?: string[];
  items?: CatalogSchema;
}

export interface CatalogOperation {
  id: string;
  method: string;
  title?: string;
  description?: string;
  parameters?: CatalogParameter[];
  inputSchema?: CatalogSchema;
  outputSchema?: CatalogSchema;
  tags?: string[];
  readOnly?: boolean;
  visible?: boolean;
}

export interface Catalog {
  name?: string;
  displayName?: string;
  description?: string;
  iconSvg?: string;
  operations: CatalogOperation[];
}

export function schemaToParameters(schema: Schema<unknown> | undefined): CatalogParameter[] {
  if (!schema?.fields) {
    return [];
  }
  return Object.entries(schema.fields).map(([name, field]) => {
    const parameter: CatalogParameter = {
      name,
      type: field.catalogType,
    };
    if (field.description) {
      parameter.description = field.description;
    }
    if (field.required) {
      parameter.required = true;
    }
    if (field.defaultValue !== undefined) {
      parameter.default = field.defaultValue;
    }
    return parameter;
  });
}

export function schemaToCatalogSchema(schema: Schema<unknown> | undefined): CatalogSchema | undefined {
  if (!schema) {
    return undefined;
  }
  const output: CatalogSchema = {
    type: schema.catalogType,
  };
  if (schema.description) {
    output.description = schema.description;
  }
  if (schema.defaultValue !== undefined) {
    output.default = schema.defaultValue;
  }
  if (schema.fields) {
    const properties: Record<string, CatalogSchema> = {};
    const required: string[] = [];
    for (const [name, field] of Object.entries(schema.fields)) {
      properties[name] = schemaToCatalogSchema(field)!;
      if (field.required) {
        required.push(name);
      }
    }
    output.properties = properties;
    if (required.length > 0) {
      output.required = required;
    }
  }
  if (schema.item) {
    output.items = schemaToCatalogSchema(schema.item)!;
  }
  return output;
}

export function catalogToJson(catalog: Catalog | Record<string, unknown> | null | undefined): string {
  if (!catalog) {
    return "";
  }
  return JSON.stringify(toCatalogJsonObject(catalog));
}

export function catalogToYaml(catalog: Catalog | Record<string, unknown>): string {
  return YAML.stringify(toCatalogYamlObject(catalog));
}

export function writeCatalogYaml(path: string, catalog: Catalog | Record<string, unknown>): void {
  writeFileSync(path, catalogToYaml(catalog), "utf8");
}

function toCatalogJsonObject(catalog: Catalog | Record<string, unknown>): Record<string, unknown> {
  if (!("operations" in catalog) || !Array.isArray(catalog.operations)) {
    return {
      ...catalog,
    };
  }

  const typedCatalog = catalog as Catalog;
  const output: Record<string, unknown> = {
    operations: typedCatalog.operations.map((operation) => {
      const serialized: Record<string, unknown> = {
        id: operation.id,
        method: operation.method,
      };
      if (operation.title) {
        serialized.title = operation.title;
      }
      if (operation.description) {
        serialized.description = operation.description;
      }
      if (operation.parameters && operation.parameters.length > 0) {
        serialized.parameters = operation.parameters;
      }
      if (operation.inputSchema !== undefined) {
        serialized.inputSchema = operation.inputSchema;
      }
      if (operation.outputSchema !== undefined) {
        serialized.outputSchema = operation.outputSchema;
      }
      if (operation.tags && operation.tags.length > 0) {
        serialized.tags = operation.tags;
      }
      if (operation.readOnly !== undefined) {
        serialized.readOnly = operation.readOnly;
      }
      if (operation.visible !== undefined) {
        serialized.visible = operation.visible;
      }
      return serialized;
    }),
  };

  if (typedCatalog.name) {
    output.name = typedCatalog.name;
  }
  if (typedCatalog.displayName) {
    output.displayName = typedCatalog.displayName;
  }
  if (typedCatalog.description) {
    output.description = typedCatalog.description;
  }
  if (typedCatalog.iconSvg) {
    output.iconSvg = typedCatalog.iconSvg;
  }

  return output;
}

function toCatalogYamlObject(catalog: Catalog | Record<string, unknown>): Record<string, unknown> {
  return snakeCaseKeys(toCatalogJsonObject(catalog)) as Record<string, unknown>;
}

function snakeCaseKeys(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((entry) => snakeCaseKeys(entry));
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  const output: Record<string, unknown> = {};
  for (const [key, entry] of Object.entries(value)) {
    output[toSnakeCase(key)] = snakeCaseKeys(entry);
  }
  return output;
}

function toSnakeCase(input: string): string {
  return input.replace(/[A-Z]/g, (match) => `_${match.toLowerCase()}`);
}

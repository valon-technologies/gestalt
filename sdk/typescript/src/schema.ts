export type CatalogType = "string" | "integer" | "number" | "boolean" | "object" | "array";

export interface Schema<T> {
  readonly catalogType: CatalogType;
  readonly description: string;
  readonly required: boolean;
  readonly defaultValue: T | undefined;
  readonly fields?: Record<string, Schema<unknown>>;
  readonly item?: Schema<unknown>;
  parse(value: unknown, path?: string): T;
}

export type InferSchema<TSchema extends Schema<unknown>> = TSchema extends Schema<infer T>
  ? T
  : never;

export interface SchemaOptions<T> {
  description?: string;
  required?: boolean;
  default?: T;
}

const integerPattern = /^[+-]?\d+$/;
const numberPattern = /^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/;

function withMeta<T>(
  catalogType: CatalogType,
  parser: (value: unknown, path: string) => T,
  options?: SchemaOptions<T>,
): Schema<T> {
  const description = options?.description?.trim() ?? "";
  const hasDefault = options ? Object.prototype.hasOwnProperty.call(options, "default") : false;
  const defaultValue = hasDefault ? cloneValue(options?.default) : undefined;
  const required = options?.required ?? !hasDefault;

  return {
    catalogType,
    description,
    required,
    defaultValue,
    parse(value: unknown, path = "$") {
      if (value === undefined || value === null) {
        if (hasDefault) {
          return cloneValue(defaultValue) as T;
        }
        if (!required) {
          return undefined as T;
        }
        throw new TypeError(`${path} is required`);
      }
      return parser(value, path);
    },
  };
}

export function string(options?: SchemaOptions<string>): Schema<string> {
  return withMeta(
    "string",
    (value, path) => {
      if (typeof value === "string") {
        return value;
      }
      throw new TypeError(`${path} must be a string`);
    },
    options,
  );
}

export function integer(options?: SchemaOptions<number>): Schema<number> {
  return withMeta(
    "integer",
    (value, path) => {
      if (typeof value === "number" && Number.isInteger(value)) {
        return value;
      }
      if (typeof value === "string") {
        const trimmed = value.trim();
        if (integerPattern.test(trimmed)) {
          return Number.parseInt(trimmed, 10);
        }
      }
      throw new TypeError(`${path} must be an integer`);
    },
    options,
  );
}

export function number(options?: SchemaOptions<number>): Schema<number> {
  return withMeta(
    "number",
    (value, path) => {
      if (typeof value === "number" && Number.isFinite(value)) {
        return value;
      }
      if (typeof value === "string") {
        const trimmed = value.trim();
        if (numberPattern.test(trimmed)) {
          const parsed = Number.parseFloat(trimmed);
          if (Number.isFinite(parsed)) {
            return parsed;
          }
        }
      }
      throw new TypeError(`${path} must be a number`);
    },
    options,
  );
}

export function boolean(options?: SchemaOptions<boolean>): Schema<boolean> {
  return withMeta(
    "boolean",
    (value, path) => {
      if (typeof value === "boolean") {
        return value;
      }
      if (typeof value === "string") {
        const normalized = value.trim().toLowerCase();
        if (normalized === "true" || normalized === "1") {
          return true;
        }
        if (normalized === "false" || normalized === "0") {
          return false;
        }
      }
      throw new TypeError(`${path} must be a boolean`);
    },
    options,
  );
}

export function array<T>(item: Schema<T>, options?: SchemaOptions<T[]>): Schema<T[]> {
  const base = withMeta<T[]>(
    "array",
    (value, path) => {
      if (!Array.isArray(value)) {
        throw new TypeError(`${path} must be an array`);
      }
      return value.map((entry, index) => item.parse(entry, `${path}[${index}]`));
    },
    options,
  );

  return {
    ...base,
    item,
  };
}

export function object<T extends Record<string, unknown>>(
  fields: { [K in keyof T]: Schema<T[K]> },
  options?: SchemaOptions<T>,
): Schema<T> {
  const base = withMeta<T>(
    "object",
    (value, path) => {
      if (value === null || typeof value !== "object" || Array.isArray(value)) {
        throw new TypeError(`${path} must be an object`);
      }
      const source = value as Record<string, unknown>;
      const output: Record<string, unknown> = {};
      for (const [key, field] of Object.entries(fields)) {
        const parsed = field.parse(source[key], `${path}.${key}`);
        if (parsed !== undefined) {
          output[key] = parsed;
        }
      }
      return output as T;
    },
    options,
  );

  return {
    ...base,
    fields: fields as Record<string, Schema<unknown>>,
  };
}

export function optional<T>(schema: Schema<T>): Schema<T | undefined> {
  const wrapped: Schema<T | undefined> = {
    catalogType: schema.catalogType,
    description: schema.description,
    required: false,
    defaultValue: schema.defaultValue,
    parse(value, path = "$") {
      if (value === undefined || value === null) {
        if (schema.defaultValue !== undefined) {
          return cloneValue(schema.defaultValue);
        }
        return undefined;
      }
      return schema.parse(value, path);
    },
  };
  const fields = schema.fields;
  const item = schema.item;
  return {
    ...wrapped,
    ...(fields !== undefined ? { fields } : {}),
    ...(item !== undefined ? { item } : {}),
  };
}

export const s = {
  string,
  integer,
  number,
  boolean,
  array,
  object,
  optional,
};

function cloneValue<T>(value: T): T {
  if (value === undefined) {
    return value;
  }
  return JSON.parse(JSON.stringify(value)) as T;
}

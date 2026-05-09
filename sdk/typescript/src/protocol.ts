import {
  clone,
  create,
  fromBinary,
  fromJson,
  fromJsonString,
  toBinary,
  toJson,
  toJsonString,
  type BinaryReadOptions,
  type BinaryWriteOptions,
  type DescMessage,
  type JsonObject,
  type JsonReadOptions,
  type JsonValue,
  type JsonWriteStringOptions,
  type Message,
  type MessageShape,
} from "@bufbuild/protobuf";
import {
  StructSchema,
  TimestampSchema,
  ValueSchema,
  type Struct,
  type Timestamp,
  type Value,
} from "@bufbuild/protobuf/wkt";

const MIN_TIMESTAMP_SECONDS = -62_135_596_800n;
const MAX_TIMESTAMP_SECONDS = 253_402_300_799n;
const MIN_TIMESTAMP_SECONDS_NUMBER = Number(MIN_TIMESTAMP_SECONDS);
const MAX_TIMESTAMP_SECONDS_NUMBER = Number(MAX_TIMESTAMP_SECONDS);

/** Primitive values accepted in protobuf JSON payloads. */
export type JsonPrimitiveInput = null | boolean | number | string;
/** Native value accepted by SDK-owned protocol helpers before runtime validation. */
export type JsonInput = JsonPrimitiveInput | readonly unknown[] | object;
/** Native object accepted by SDK-owned Struct helpers before runtime validation. */
export type JsonObjectInput = object;
/** Common shape implemented by generated protobuf messages. */
export type ProtoMessage = Message;
/** Alias for google.protobuf.Struct at protocol boundaries. */
export type { Struct };
/** Alias for google.protobuf.Value at protocol boundaries. */
export type { Value };
/** Alias for google.protobuf.Timestamp at protocol boundaries. */
export type { Timestamp };
/** Binary parse options accepted by unmarshalProto. */
export type { BinaryReadOptions };
/** Binary serialize options accepted by marshalProtoDeterministic. */
export type { BinaryWriteOptions };
/** JSON parse options accepted by unmarshalProtoJson. */
export type { JsonReadOptions };
/** JSON serialize options accepted by marshalProtoJson. */
export type { JsonWriteStringOptions };

/** Schema for google.protobuf.Struct. */
export { StructSchema };
/** Schema for google.protobuf.Value. */
export { ValueSchema };
/** Schema for google.protobuf.Timestamp. */
export { TimestampSchema };

/** Returns a protobuf Struct-compatible JSON object for generated message fields. */
export function structFromObject(value: JsonObjectInput = {}): JsonObject {
  return normalizeJsonObject(value, "struct", new WeakSet<object>());
}

/** Returns a protobuf Struct-compatible JSON object for generated message fields. */
export function structFromJsonObject(value: JsonObject = {}): JsonObject {
  return structFromObject(value);
}

/** Converts a protobuf Struct-compatible field value back to a JSON object. */
export function jsonObjectFromStruct(value?: JsonObject | undefined): JsonObject {
  return value === undefined ? {} : { ...value };
}

/** Converts a JSON-compatible native value into a protobuf Value message. */
export function valueFromJson(value: JsonInput): Value {
  return fromJson(ValueSchema, jsonFromInput(value));
}

/** Converts a protobuf Value message into its JSON value. */
export function jsonFromValue(value?: Value | undefined): JsonValue {
  if (value === undefined) {
    return null;
  }
  return toJson(ValueSchema, value);
}

/** Converts a valid JavaScript Date into a protobuf Timestamp message. */
export function timestampFromDate(value: Date): Timestamp {
  const millis = value.getTime();
  if (Number.isNaN(millis)) {
    throw new TypeError("timestampFromDate expects a valid Date");
  }
  const seconds = Math.floor(millis / 1000);
  if (
    seconds < MIN_TIMESTAMP_SECONDS_NUMBER ||
    seconds > MAX_TIMESTAMP_SECONDS_NUMBER
  ) {
    throw new RangeError("Date is outside the protobuf Timestamp range");
  }
  const nanos = Math.trunc((millis - (seconds * 1000)) * 1_000_000);
  return create(TimestampSchema, {
    seconds: BigInt(seconds),
    nanos,
  });
}

/** Converts a protobuf Timestamp message into a JavaScript Date. */
export function dateFromTimestamp(value: Timestamp): Date {
  if (value.nanos < 0 || value.nanos >= 1_000_000_000) {
    throw new RangeError("protobuf Timestamp nanos out of range");
  }
  if (
    value.seconds < MIN_TIMESTAMP_SECONDS ||
    value.seconds > MAX_TIMESTAMP_SECONDS
  ) {
    throw new RangeError("protobuf Timestamp seconds out of range");
  }
  const millis =
    (Number(value.seconds) * 1000) + Math.trunc(value.nanos / 1_000_000);
  const date = new Date(millis);
  if (Number.isNaN(date.getTime())) {
    throw new RangeError("protobuf Timestamp seconds out of range");
  }
  return date;
}

/** Returns a protobuf-compatible JSON value from native SDK input. */
export function jsonFromInput(value: JsonInput): JsonValue {
  return normalizeJsonValue(value, "value", new WeakSet<object>());
}

/** Serializes a protobuf message after normalizing map field insertion order. */
export function marshalProtoDeterministic<Desc extends DescMessage>(
  schema: Desc,
  message: MessageShape<Desc>,
  options?: Partial<BinaryWriteOptions>,
): Uint8Array {
  return toBinary(schema, deterministicMessage(schema, message), options);
}

/** Parses protobuf binary wire data with a generated message schema. */
export function unmarshalProto<Desc extends DescMessage>(
  schema: Desc,
  data: Uint8Array,
  options?: Partial<BinaryReadOptions>,
): MessageShape<Desc> {
  return fromBinary(schema, data, options);
}

/** Serializes a protobuf message to protobuf JSON text. */
export function marshalProtoJson<Desc extends DescMessage>(
  schema: Desc,
  message: MessageShape<Desc>,
  options?: Partial<JsonWriteStringOptions>,
): string {
  return toJsonString(schema, message, options);
}

/** Parses protobuf JSON text with a generated message schema. */
export function unmarshalProtoJson<Desc extends DescMessage>(
  schema: Desc,
  data: string,
  options?: Partial<JsonReadOptions>,
): MessageShape<Desc> {
  return fromJsonString(schema, data, options);
}

function normalizeJsonObject(
  value: unknown,
  path: string,
  seen: WeakSet<object>,
): JsonObject {
  if (!isPlainObject(value)) {
    throw new TypeError(`${path} must be a plain JSON object`);
  }
  if (seen.has(value)) {
    throw new TypeError(`${path} contains a cycle`);
  }
  seen.add(value);
  try {
    const output: JsonObject = {};
    for (const key of Reflect.ownKeys(value)) {
      if (typeof key !== "string") {
        throw new TypeError(`${path} keys must be strings`);
      }
    }
    for (const [key, entry] of Object.entries(value)) {
      output[key] = normalizeJsonValue(entry, `${path}.${key}`, seen);
    }
    return output;
  } finally {
    seen.delete(value);
  }
}

function normalizeJsonValue(
  value: unknown,
  path: string,
  seen: WeakSet<object>,
): JsonValue {
  if (value === null || typeof value === "string" || typeof value === "boolean") {
    return value;
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) {
      throw new TypeError(`${path} must be a finite number`);
    }
    return value;
  }
  if (typeof value === "undefined") {
    throw new TypeError(`${path} must not be undefined`);
  }
  if (typeof value === "bigint" || typeof value === "symbol" || typeof value === "function") {
    throw new TypeError(`${path} must be JSON-compatible`);
  }
  if (Array.isArray(value)) {
    if (seen.has(value)) {
      throw new TypeError(`${path} contains a cycle`);
    }
    seen.add(value);
    try {
      return value.map((entry, index) => normalizeJsonValue(entry, `${path}[${index}]`, seen));
    } finally {
      seen.delete(value);
    }
  }
  return normalizeJsonObject(value, path, seen);
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== "object") {
    return false;
  }
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function deterministicMessage<Desc extends DescMessage>(
  schema: Desc,
  message: MessageShape<Desc>,
): MessageShape<Desc> {
  const copy = clone(schema, message);
  sortMapFields(schema, copy);
  return copy;
}

function sortMapFields(schema: DescMessage, message: unknown): void {
  if (!isObjectRecord(message)) {
    return;
  }
  for (const field of schema.fields) {
    if (field.fieldKind === "map") {
      const mapValue = message[field.localName];
      if (!isObjectRecord(mapValue)) {
        continue;
      }
      const sorted: Record<string, unknown> = {};
      for (const [key, value] of Object.entries(mapValue).sort(compareMapEntries)) {
        if (field.mapKind === "message") {
          sortMapFields(field.message, value);
        }
        sorted[key] = value;
      }
      message[field.localName] = sorted;
      continue;
    }
    if (field.fieldKind === "message") {
      const value = field.oneof === undefined
        ? message[field.localName]
        : selectedOneofValue(message, field.oneof.localName, field.localName);
      sortMapFields(field.message, value);
      continue;
    }
    if (field.fieldKind === "list" && field.listKind === "message") {
      const values = message[field.localName];
      if (Array.isArray(values)) {
        for (const value of values) {
          sortMapFields(field.message, value);
        }
      }
    }
  }
}

function selectedOneofValue(
  message: Record<string, unknown>,
  oneofName: string,
  fieldName: string,
): unknown {
  const oneof = message[oneofName];
  if (!isObjectRecord(oneof) || oneof.case !== fieldName) {
    return undefined;
  }
  return oneof.value;
}

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object";
}

function compareMapEntries(
  [left]: [string, unknown],
  [right]: [string, unknown],
): number {
  if (left < right) {
    return -1;
  }
  if (left > right) {
    return 1;
  }
  return 0;
}

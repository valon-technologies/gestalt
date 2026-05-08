import {
  create,
  fromJson,
  toJson,
  type JsonObject,
  type JsonValue,
} from "@bufbuild/protobuf";
import {
  TimestampSchema,
  ValueSchema,
  type Timestamp,
  type Value,
} from "@bufbuild/protobuf/wkt";

const MIN_TIMESTAMP_SECONDS = -62_135_596_800n;
const MAX_TIMESTAMP_SECONDS = 253_402_300_799n;
const MIN_TIMESTAMP_SECONDS_NUMBER = Number(MIN_TIMESTAMP_SECONDS);
const MAX_TIMESTAMP_SECONDS_NUMBER = Number(MAX_TIMESTAMP_SECONDS);

/** Returns a protobuf Struct-compatible JSON object for generated message fields. */
export function structFromJsonObject(value: JsonObject = {}): JsonObject {
  return { ...value };
}

/** Converts a protobuf Struct-compatible field value back to a JSON object. */
export function jsonObjectFromStruct(value?: JsonObject | undefined): JsonObject {
  return value === undefined ? {} : { ...value };
}

/** Converts a JSON value into a protobuf Value message. */
export function valueFromJson(value: JsonValue): Value {
  return fromJson(ValueSchema, value);
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

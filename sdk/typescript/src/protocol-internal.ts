import type { JsonObject } from "@bufbuild/protobuf";

import {
  jsonObjectFromStruct,
  structFromObject,
  type JsonObjectInput,
} from "./protocol.ts";

export function optionalStruct(
  value?: JsonObjectInput | undefined,
): JsonObject | undefined {
  return value === undefined ? undefined : structFromObject(value);
}

export function optionalObjectFromStruct(
  value?: JsonObject | undefined,
): JsonObjectInput | undefined {
  return value === undefined ? undefined : jsonObjectFromStruct(value);
}

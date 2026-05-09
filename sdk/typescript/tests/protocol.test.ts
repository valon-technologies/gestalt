import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { expect, test } from "bun:test";

import {
  dateFromTimestamp,
  timestampFromDate,
} from "../src/protocol.ts";
import {
  jsonFromValue,
  jsonObjectFromStruct,
  structFromObject,
  structFromJsonObject,
  valueFromJson,
} from "../src/index.ts";
import {
  WorkflowEventSchema,
  WorkflowManagerSignalOrStartRunRequestSchema,
} from "../src/internal/gen/v1/workflow_pb.ts";

test("protocol helpers produce generated workflow field shapes", () => {
  const payload = structFromObject({ ok: true, nested: { value: "yes" } });
  const metadata = structFromJsonObject({ source: "test" });
  const extension = valueFromJson(["trace", 1]);
  const timestamp = timestampFromDate(new Date("1969-12-31T23:59:59.999Z"));

  const request = create(WorkflowManagerSignalOrStartRunRequestSchema, {
    workflowKey: "workflow:1",
    signal: {
      name: "signal",
      payload,
      metadata,
      createdAt: timestamp,
    },
  });
  const event = create(WorkflowEventSchema, {
    id: "evt-1",
    type: "example",
    data: payload,
    extensions: { trace: extension },
    time: timestamp,
  });

  expect(request.signal?.payload).toEqual(payload);
  expect(jsonObjectFromStruct(request.signal?.metadata)).toEqual({
    source: "test",
  });
  expect(jsonObjectFromStruct(event.data)).toEqual({
    ok: true,
    nested: { value: "yes" },
  });
  expect(jsonFromValue(event.extensions.trace)).toEqual(["trace", 1]);
  expect(timestamp.seconds).toBe(-1n);
  expect(timestamp.nanos).toBe(999_000_000);
  expect(dateFromTimestamp(timestamp).toISOString()).toBe(
    "1969-12-31T23:59:59.999Z",
  );
});

test("structFromObject rejects non-JSON object values explicitly", () => {
  class CustomObject {
    value = "hidden";
  }
  const cyclic: Record<string, unknown> = {};
  cyclic.self = cyclic;
  const symbolKey = Symbol("hidden");

  expect(() => structFromObject({ when: new Date() } as any)).toThrow(TypeError);
  expect(() => structFromObject({ map: new Map() } as any)).toThrow(TypeError);
  expect(() => structFromObject({ custom: new CustomObject() } as any)).toThrow(TypeError);
  expect(() => structFromObject({ missing: undefined } as any)).toThrow(TypeError);
  expect(() => structFromObject({ id: 1n } as any)).toThrow(TypeError);
  expect(() => structFromObject({ score: Number.NaN } as any)).toThrow(TypeError);
  expect(() => structFromObject({ [symbolKey]: "secret", visible: true } as any)).toThrow(
    TypeError,
  );
  expect(() => structFromObject(cyclic as any)).toThrow(TypeError);
});

test("timestampFromDate rejects invalid dates and dateFromTimestamp validates nanos", () => {
  const minTimestamp = timestampFromDate(new Date("0001-01-01T00:00:00.000Z"));
  const maxTimestamp = timestampFromDate(new Date("9999-12-31T23:59:59.999Z"));
  expect(minTimestamp.seconds).toBe(-62_135_596_800n);
  expect(minTimestamp.nanos).toBe(0);
  expect(maxTimestamp.seconds).toBe(253_402_300_799n);
  expect(maxTimestamp.nanos).toBe(999_000_000);

  const beforeMin = new Date(
    new Date("0001-01-01T00:00:00.000Z").getTime() - 1,
  );
  const afterMax = new Date(
    new Date("9999-12-31T23:59:59.999Z").getTime() + 1,
  );
  expect(() => timestampFromDate(new Date(Number.NaN))).toThrow(TypeError);
  expect(() => timestampFromDate(beforeMin)).toThrow(RangeError);
  expect(() => timestampFromDate(afterMax)).toThrow(RangeError);
  expect(() =>
    dateFromTimestamp({ seconds: 0n, nanos: -1 } as Timestamp),
  ).toThrow(RangeError);
  expect(() =>
    dateFromTimestamp({ seconds: 0n, nanos: 1_000_000_000 } as Timestamp),
  ).toThrow(RangeError);
  expect(() =>
    dateFromTimestamp({
      seconds: -62_135_596_801n,
      nanos: 999_999_999,
    } as Timestamp),
  ).toThrow(RangeError);
  expect(() =>
    dateFromTimestamp({ seconds: 253_402_300_800n, nanos: 0 } as Timestamp),
  ).toThrow(RangeError);
});

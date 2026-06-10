import { expect, test } from "bun:test";

import {
  toWirePresignObjectRequest,
  toWireReadObjectRequest,
  toWireWriteObjectRequest,
} from "../src/internal/codec/s3.ts";

test("toWire zero-fills fields left unset by sparse Init values", () => {
  const wireRequest = toWirePresignObjectRequest({ ref: { key: "object.txt" } });
  expect(wireRequest.ref?.key).toBe("object.txt");
  expect(wireRequest.ref?.versionId).toBe("");
  expect(wireRequest.method).toBe(0);
  expect(wireRequest.expiresSeconds).toBe(0n);
  expect(wireRequest.contentType).toBe("");
  expect(wireRequest.contentDisposition).toBe("");
  expect(wireRequest.headers).toEqual({});

  const read = toWireReadObjectRequest({ ref: { key: "object.txt" } });
  expect(read.ref?.key).toBe("object.txt");
  expect(read.ifMatch).toBe("");
  expect(read.ifNoneMatch).toBe("");
  expect(read.range).toBeUndefined();
});

test("toWire defaults an unset oneof to the empty case", () => {
  const frame = toWireWriteObjectRequest({});
  expect(frame.msg.case).toBeUndefined();

  const open = toWireWriteObjectRequest({
    msg: { case: "open", value: { ref: { key: "object.txt" } } },
  });
  expect(open.msg.case).toBe("open");
  expect(open.msg.case === "open" ? open.msg.value.contentType : undefined).toBe("");
});

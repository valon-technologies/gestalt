import { expect, test } from "bun:test";

import { Authorization } from "../src/index.ts";
import * as AuthorizationMessages from "../src/internal/ts-proto/v1/authorization.ts";

test("ts-proto authorization structs use native object properties", () => {
  const subject: Authorization.Subject = {
    type: "user",
    id: "user-123",
    properties: {
      active: true,
      limit: 42,
      tags: ["admin", "viewer"],
      nested: { tenant: "acme" },
    },
  };

  const decoded = AuthorizationMessages.Subject.decode(
    AuthorizationMessages.Subject.encode(subject).finish(),
  );

  expect(decoded).toEqual(subject);
  expect(decoded.properties?.nested).toEqual({ tenant: "acme" });
});

test("ts-proto authorization timestamps use Date", () => {
  const createdAt = new Date("2026-01-02T03:04:05.006Z");
  const model: Authorization.AuthorizationModelRef = {
    id: "model-1",
    version: "v1",
    createdAt,
  };

  const decoded = AuthorizationMessages.AuthorizationModelRef.decode(
    AuthorizationMessages.AuthorizationModelRef.encode(model).finish(),
  );

  expect(decoded.createdAt).toEqual(createdAt);
});

test("ts-proto relationship targets keep the existing object shape", () => {
  const target: Authorization.RelationshipTarget = {
    subject: {
      type: "user",
      id: "user-123",
      properties: { department: "servicing" },
    },
  };

  const decoded = AuthorizationMessages.RelationshipTarget.decode(
    AuthorizationMessages.RelationshipTarget.encode(target).finish(),
  );

  expect(decoded).toEqual(target);
});

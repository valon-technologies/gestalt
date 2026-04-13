import { expect, test } from "bun:test";

import {
  request,
  requestCredential,
  requestSubject,
  type Request,
} from "../src/index.ts";

test("request context helpers preserve additive request shape", () => {
  const legacy: Request = {
    token: "token-123",
    connectionParams: {
      region: "iad",
    },
  };

  expect(requestSubject(legacy)).toEqual({
    id: "",
    kind: "",
    displayName: "",
    authSource: "",
  });
  expect(requestCredential(legacy)).toEqual({
    mode: "",
    subjectId: "",
    connection: "",
    instance: "",
  });
});

test("request helper stores subject and credential context out of band", () => {
  const built = request(
    "token-123",
    { region: "iad" },
    { id: "user:user-123", kind: "user" },
    { mode: "identity", subjectId: "identity:__identity__" },
  );

  expect(built).toEqual({
    token: "token-123",
    connectionParams: {
      region: "iad",
    },
  });
  expect(requestSubject(built)).toEqual({
    id: "user:user-123",
    kind: "user",
    displayName: "",
    authSource: "",
  });
  expect(requestCredential(built)).toEqual({
    mode: "identity",
    subjectId: "identity:__identity__",
    connection: "",
    instance: "",
  });
});

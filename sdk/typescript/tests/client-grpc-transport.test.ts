import { create } from "@bufbuild/protobuf";
import { expect, test } from "bun:test";

import {
  AppInvokeRequestSchema,
  OperationResultSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import { authToProvider, bearer } from "../src/client/auth.ts";
import { createGrpcUnaryTransport } from "../src/client/grpc_transport.ts";
import { PUBLIC_METHODS } from "../src/client/generated/methods.ts";
import { GestaltErrorCode } from "../src/rpc_support.ts";

const invokeRequest = () =>
  create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

test("gRPC transport dispatches typed App methods and maps errors", async () => {
  const transport = await createGrpcUnaryTransport({
    baseUrl: "https://gestalt.test",
    auth: authToProvider(bearer(() => "token")),
  });

  await expect(
    transport.unary(
      PUBLIC_METHODS.app.invoke,
      invokeRequest(),
      AppInvokeRequestSchema,
      OperationResultSchema,
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Unavailable,
  });

  await transport.close();
});

test("gRPC transport maps bearer provider failures to Unavailable", async () => {
  const transport = await createGrpcUnaryTransport({
    baseUrl: "https://gestalt.test",
    auth: authToProvider(
      bearer(async () => {
        throw new Error("token provider failed");
      }),
    ),
  });

  await expect(
    transport.unary(
      PUBLIC_METHODS.app.invoke,
      invokeRequest(),
      AppInvokeRequestSchema,
      OperationResultSchema,
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Unavailable,
    message: "token provider failed",
  });

  await transport.close();
});

test("gRPC transport timeout interrupts hanging bearer resolution", async () => {
  let tokenCalls = 0;
  const transport = await createGrpcUnaryTransport({
    baseUrl: "https://gestalt.test",
    auth: authToProvider(
      bearer(async () => {
        tokenCalls += 1;
        await new Promise(() => {});
        return "token";
      }),
    ),
  });

  const request = invokeRequest();

  await expect(
    transport.unary(
      PUBLIC_METHODS.app.invoke,
      request,
      AppInvokeRequestSchema,
      OperationResultSchema,
      { timeoutMs: 1 },
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.DeadlineExceeded,
  });
  expect(tokenCalls).toBe(1);

  await transport.close();
});

test("gRPC transport close aborts the owned HTTP/2 session", async () => {
  const transport = await createGrpcUnaryTransport({
    baseUrl: "https://gestalt.test",
    auth: authToProvider(bearer(() => "token")),
  });

  const request = invokeRequest();

  await expect(
    transport.unary(
      PUBLIC_METHODS.app.invoke,
      request,
      AppInvokeRequestSchema,
      OperationResultSchema,
    ),
  ).rejects.toBeDefined();

  await expect(transport.close()).resolves.toBeUndefined();
  await expect(transport.close()).resolves.toBeUndefined();
});

test("gRPC transport forwards call options to Connect", async () => {
  const transport = await createGrpcUnaryTransport({
    baseUrl: "https://gestalt.test",
    auth: authToProvider(bearer(() => "token")),
  });

  const request = create(AppInvokeRequestSchema, {
    app: "example",
    operation: "sync",
    params: {},
    connection: "",
    instance: "",
    idempotencyKey: "",
    credentialMode: "",
  });

  const controller = new AbortController();
  controller.abort();

  await expect(
    transport.unary(
      PUBLIC_METHODS.app.invoke,
      request,
      AppInvokeRequestSchema,
      OperationResultSchema,
      { signal: controller.signal },
    ),
  ).rejects.toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Canceled,
  });

  await transport.close();
});

import { readFileSync } from "node:fs";
import { join } from "node:path";

import type { Message } from "@bufbuild/protobuf";
import { create } from "@bufbuild/protobuf";
import { expect, test } from "bun:test";

import { AppClient } from "../src/client/generated/app_client.ts";
import { PUBLIC_METHODS } from "../src/client/generated/methods.ts";
import type { PublicAppInvokeRequest } from "../src/client/generated/types.ts";
import type { UnaryTransport } from "../src/client/generated/unary_transport.ts";
import {
  OperationResultSchema,
} from "../src/internal/gen/v1/app_pb.ts";
import { GestaltError, GestaltErrorCode } from "../src/rpc_support.ts";

const fixtureRoot = join(import.meta.dir, "..", "..", "testdata", "public_conformance");

type ClientCase = {
  id: string;
  publicRequest: PublicAppInvokeRequest;
  wireRequest: Record<string, unknown>;
  response: {
    operationResult?: { bodyBase64: string };
    gestaltError?: { code: number; message: string };
  };
  expect: {
    result?: unknown;
    gestaltError?: { code: number; message: string };
    calls: number;
  };
};

class RecordingTransport implements UnaryTransport {
  calls = 0;
  err: GestaltError | undefined;
  body = new Uint8Array();
  lastRequest: Message | undefined;

  async unary<Output extends Message>(
    method: (typeof PUBLIC_METHODS)["app"]["invoke"],
    request: Message,
    _inputSchema: unknown,
    _outputSchema: unknown,
  ): Promise<Output> {
    this.calls += 1;
    this.lastRequest = request;
    if (method.method !== "Invoke") {
      throw new GestaltError(GestaltErrorCode.Internal, "unexpected method");
    }
    if (this.err) {
      throw this.err;
    }
    return create(OperationResultSchema, {
      status: 200,
      body: this.body,
    }) as unknown as Output;
  }
}

function projectWire(
  wire: Record<string, unknown>,
  expected: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(expected)) {
    out[key] = wire[key as keyof typeof wire];
  }
  return out;
}

function loadCases(): ClientCase[] {
  return JSON.parse(readFileSync(join(fixtureRoot, "client_cases.json"), "utf8"));
}

test("public app client shared cases", async () => {
  for (const clientCase of loadCases()) {
    const transport = new RecordingTransport();
    if (clientCase.id === "invoke_success") {
      const bodyB64 = clientCase.response.operationResult?.bodyBase64;
      if (!bodyB64) throw new Error("missing body");
      transport.body = Uint8Array.from(atob(bodyB64), (c) => c.charCodeAt(0));
    } else if (clientCase.id === "platform_error") {
      const err = clientCase.response.gestaltError;
      if (!err) throw new Error("missing gestalt error");
      transport.err = new GestaltError(err.code, err.message);
    } else {
      throw new Error(`unknown case ${clientCase.id}`);
    }

    const client = new AppClient(transport);
    const request = clientCase.publicRequest;

    if (clientCase.id === "invoke_success") {
      await expect(client.invoke(request)).resolves.toEqual(clientCase.expect.result);
    } else {
      await expect(client.invoke(request)).rejects.toMatchObject({
        code: clientCase.expect.gestaltError?.code,
        message: clientCase.expect.gestaltError?.message,
      });
    }

    if (!transport.lastRequest) {
      throw new Error("transport did not receive a request");
    }
    const gotWire = projectWire(
      JSON.parse(JSON.stringify(transport.lastRequest)) as Record<string, unknown>,
      clientCase.wireRequest,
    );
    expect(gotWire).toEqual(clientCase.wireRequest);

    expect(transport.calls).toBe(clientCase.expect.calls);
  }
});

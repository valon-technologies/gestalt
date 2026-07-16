import { create, clone, toBinary, type Message } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { expect, test } from "bun:test";

import {
  AgentSessionState,
  AgentSessionSchema,
  AgentToolConfigSchema,
  AgentNoToolsSchema,
  CreateAgentProviderSessionRequestSchema,
  ListAgentProviderSessionsRequestSchema,
} from "../src/internal/gen/v1/agent_pb.ts";
import type { RequestContext } from "../src/internal/gen/v1/app_pb.ts";
import { RequestContextSchema } from "../src/internal/gen/v1/app_pb.ts";
import { CountResponseSchema } from "../src/internal/gen/v1/indexeddb_pb.ts";
import { OperationResultSchema } from "../src/internal/gen/v1/app_pb.ts";

/** Mirrors bound_grpc_transport context injection without a ProtoJSON round-trip. */
function injectBoundContext<Desc extends Parameters<typeof clone>[0]>(
  schema: Desc,
  request: Parameters<typeof clone<Desc>>[1],
  context: ReturnType<typeof create<typeof RequestContextSchema>>,
): ReturnType<typeof clone<Desc>> {
  const wireRequest = clone(schema, request);
  const withContext = wireRequest as Message & { context?: RequestContext };
  if (!withContext.context) {
    withContext.context = context;
  }
  return wireRequest;
}

test("bound context injection preserves enum fields", () => {
  const request = create(ListAgentProviderSessionsRequestSchema, {
    state: AgentSessionState.ACTIVE,
    limit: 5,
  });
  const context = create(RequestContextSchema, {
    subject: { id: "user:ada" },
  });
  const wire = injectBoundContext(
    ListAgentProviderSessionsRequestSchema,
    request,
    context,
  );
  expect(wire.state).toBe(AgentSessionState.ACTIVE);
  expect(() => toBinary(ListAgentProviderSessionsRequestSchema, wire)).not.toThrow();
});

test("bound context injection preserves int64 fields", () => {
  const request = create(CountResponseSchema, {
    count: 42n,
  });
  const context = create(RequestContextSchema, {
    subject: { id: "user:ada" },
  });
  const wire = injectBoundContext(CountResponseSchema, request, context);
  expect(wire.count).toBe(42n);
  expect(() => toBinary(CountResponseSchema, wire)).not.toThrow();
});

test("bound context injection preserves bytes fields", () => {
  const body = new Uint8Array([0xff, 0x00, 0xfe]);
  const request = create(OperationResultSchema, {
    status: 200,
    body,
  });
  const context = create(RequestContextSchema, {
    subject: { id: "user:ada" },
  });
  const wire = injectBoundContext(OperationResultSchema, request, context);
  expect(wire.body).toEqual(body);
  expect(() => toBinary(OperationResultSchema, wire)).not.toThrow();
});

test("bound context injection preserves timestamp fields", () => {
  const when = timestampFromDate(new Date("2026-01-15T12:00:00.000Z"));
  const request = create(AgentSessionSchema, {
    createdAt: when,
  });
  const context = create(RequestContextSchema, {
    subject: { id: "user:ada" },
  });
  const wire = injectBoundContext(AgentSessionSchema, request, context);
  expect(wire.createdAt).toEqual(when);
  expect(() => toBinary(AgentSessionSchema, wire)).not.toThrow();
});

test("bound context injection preserves oneof fields", () => {
  const request = create(CreateAgentProviderSessionRequestSchema, {
    tools: create(AgentToolConfigSchema, {
      source: {
        case: "none",
        value: create(AgentNoToolsSchema, {}),
      },
    }),
  });
  const context = create(RequestContextSchema, {
    subject: { id: "user:ada" },
  });
  const wire = injectBoundContext(
    CreateAgentProviderSessionRequestSchema,
    request,
    context,
  );
  expect(wire.tools?.source.case).toBe("none");
  expect(() => toBinary(CreateAgentProviderSessionRequestSchema, wire)).not.toThrow();
});

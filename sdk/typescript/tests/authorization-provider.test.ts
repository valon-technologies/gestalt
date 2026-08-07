import { expect, test } from "bun:test";

import { create } from "@bufbuild/protobuf";

import {
  createAuthorizationProviderService,
  type AuthorizationProvider,
} from "../src/providers/authorization.ts";
import { CheckAccessRequestSchema } from "../src/internal/gen/v1/authorization_pb.ts";

test("authorization provider encodes matched relations", async () => {
  const provider = {
    checkAccess: async () => ({
      allowed: true,
      modelId: "model-1",
      matchedRelations: ["admin"],
    }),
  } as unknown as AuthorizationProvider;
  const service = createAuthorizationProviderService(provider);

  const response = await service.checkAccess!(
    create(CheckAccessRequestSchema),
    {} as never,
  );

  expect(response.allowed).toBe(true);
  expect(response.modelId).toBe("model-1");
  expect(response.matchedRelations).toEqual(["admin"]);
});

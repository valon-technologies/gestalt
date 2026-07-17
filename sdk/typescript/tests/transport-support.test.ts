import { expect, test } from "bun:test";

import { GestaltErrorCode } from "../src/rpc_support.ts";
import { buildRestPath } from "../src/client/generated/rest_request_mapping.ts";
import {
  raceWithAbort,
  resolveEffectiveAbortSignal,
  toTransportGestaltError,
} from "../src/client/generated/transport_support.ts";

test("caller abort with timeout-like reason maps to Canceled", () => {
  const controller = new AbortController();
  controller.abort(new Error("request timeout from upstream"));

  const signal = resolveEffectiveAbortSignal({ signal: controller.signal });
  const error = toTransportGestaltError(
    { signal: controller.signal },
    controller.signal.reason,
    signal,
  );

  expect(error).toMatchObject({
    name: "GestaltError",
    code: GestaltErrorCode.Canceled,
  });
});

test("buildRestPath substitutes repeated path placeholders", () => {
  const path = buildRestPath(
    {
      verb: "GET",
      path: "/v1/{id}/copies/{id}",
      body: "",
      pathFields: [{ name: "id", jsonName: "id" }],
      queryFields: [],
    },
    { id: "42" },
  );

  expect(path).toBe("/v1/42/copies/42");
});

test("raceWithAbort removes its listener after authentication resolves", async () => {
  const controller = new AbortController();
  const signal = resolveEffectiveAbortSignal({ signal: controller.signal });

  for (let i = 0; i < 3; i += 1) {
    await raceWithAbort(Promise.resolve(`ok-${i}`), signal, {
      removeListener: true,
    });
  }

  controller.abort(new Error("done"));
  await expect(
    raceWithAbort(new Promise(() => {}), signal, { removeListener: true }),
  ).rejects.toThrow("done");
});

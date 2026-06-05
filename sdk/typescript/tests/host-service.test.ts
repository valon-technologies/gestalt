import { expect, test } from "bun:test";

import {
  HOST_SERVICE_RELAY_TOKEN_HEADER,
  INVOCATION_TOKEN_HEADER,
  hostServiceMetadataInterceptors,
} from "../src/host-service.ts";

test("host service metadata interceptor forwards invocation token", async () => {
  const [interceptor] = hostServiceMetadataInterceptors(
    "relay-token",
    "",
    "invoke-token",
  );
  expect(interceptor).toBeDefined();

  const request = { header: new Headers() };
  await interceptor!(async (req) => {
    expect(req.header.get(HOST_SERVICE_RELAY_TOKEN_HEADER)).toBe("relay-token");
    expect(req.header.get(INVOCATION_TOKEN_HEADER)).toBe("invoke-token");
    return {} as never;
  })(request as never);
});

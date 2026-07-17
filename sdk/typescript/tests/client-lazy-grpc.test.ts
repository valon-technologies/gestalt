import { expect, test } from "bun:test";

import { readFile } from "node:fs/promises";
import { join } from "node:path";

const clientDir = join(import.meta.dir, "..", "src", "client");

test("REST-only client entry does not statically import gRPC dependencies", async () => {
  const entrySource = await readFile(join(clientDir, "index.ts"), "utf8");
  const clientSource = await readFile(join(clientDir, "client.ts"), "utf8");

  expect(entrySource.includes("connect-node")).toBe(false);
  expect(entrySource.includes("grpc_transport")).toBe(false);
  expect(clientSource.includes("@connectrpc/connect-node")).toBe(false);
  expect(clientSource.includes('import("./grpc_transport.ts")')).toBe(true);
  expect(clientSource.includes('from "./rest_transport.ts"')).toBe(true);
});

test("gRPC transport loads connect-node via dynamic import", async () => {
  const grpcTransportSource = await readFile(join(clientDir, "grpc_transport.ts"), "utf8");
  expect(grpcTransportSource.includes('import("@connectrpc/connect-node")')).toBe(true);
  expect(grpcTransportSource.includes("Http2SessionManager")).toBe(true);
  expect(grpcTransportSource.includes("sessionManager.abort()")).toBe(true);
});

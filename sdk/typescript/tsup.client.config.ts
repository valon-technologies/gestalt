import { defineConfig } from "tsup";

export default defineConfig({
  entry: ["src/client/index.ts"],
  format: ["esm"],
  dts: true,
  outDir: "dist/client",
  splitting: false,
  sourcemap: true,
  clean: true,
  external: [
    "@bufbuild/protobuf",
    "@connectrpc/connect",
    "@connectrpc/connect-node",
  ],
});

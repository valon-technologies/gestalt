import { defineConfig } from "tsup";

export default defineConfig({
  entry: {
    index: "src/index.ts",
    mount: "src/mount.ts",
  },
  format: ["esm"],
  dts: true,
  outDir: "dist",
  splitting: false,
  sourcemap: true,
  clean: true,
  external: ["@bufbuild/protobuf", "@connectrpc/connect"],
});

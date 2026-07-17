import { expect, test } from "bun:test";

import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "..");

// The docs are the package surface: typedoc must document exactly the
// entrypoints the package exports, with no curated shim layer in between.
test("typedoc entryPoints mirror package.json exports", () => {
  const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
  const typedoc = JSON.parse(readFileSync(join(root, "typedoc.json"), "utf8"));
  const exportTargets = Object.values(
    pkg.exports as Record<
      string,
      string | { default?: string; import?: string; types?: string }
    >,
  )
    .map((target) => {
      const path =
        typeof target === "string"
          ? target
          : (target.import ?? target.default ?? target.types);
      if (!path) {
        throw new Error("export target is missing a resolvable path");
      }
      if (path.startsWith("./dist/client/")) {
        return "src/client/index.ts";
      }
      return path.replace(/^\.\//, "");
    })
    .sort();
  const entryPoints = [...(typedoc.entryPoints as string[])].sort();
  expect(entryPoints).toEqual(exportTargets);
});

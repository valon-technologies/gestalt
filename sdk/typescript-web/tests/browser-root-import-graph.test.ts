import { join } from "node:path";

import { expect, test } from "bun:test";

import {
  buildBrowserMetafile,
  collectBundleViolations,
} from "../scripts/scan-browser-bundle.ts";

const browserEntry = join(import.meta.dir, "..", "dist", "index.js");

test("gestalt-web browser root bundle excludes node-only and mount/vite modules", async () => {
  const metafile = await buildBrowserMetafile(browserEntry);
  const violations = collectBundleViolations(metafile);
  expect(violations).toEqual([]);
  expect(Object.keys(metafile.inputs).length).toBeGreaterThan(20);
});

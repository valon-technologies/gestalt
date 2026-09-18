import { describe, expect, test } from "bun:test";

import { catalogToJson, type Catalog } from "../src/index.ts";

describe("catalog API exposure", () => {
  test("preserves legacy booleans and browserSession discriminator", () => {
    const catalog: Catalog = {
      operations: [
        { id: "public", method: "GET", api: true },
        { id: "private", method: "GET", api: false },
        { id: "browser", method: "GET", api: "browserSession" },
      ],
    };

    const operations = JSON.parse(catalogToJson(catalog)).operations as Array<{
      api: unknown;
    }>;
    expect(operations.map((operation) => operation.api)).toEqual([
      true,
      false,
      "browserSession",
    ]);
  });

  test("rejects unknown API exposure modes instead of defaulting public", () => {
    expect(() =>
      catalogToJson({
        operations: [
          {
            id: "unknown",
            method: "GET",
            api: "futureMode" as never,
          },
        ],
      }),
    ).toThrow('api must be a boolean or "browserSession"');
  });
});

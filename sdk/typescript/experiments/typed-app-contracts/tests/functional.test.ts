import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";

import { ExperimentError } from "../src/model.ts";
import { InstallationManager } from "../src/runtime.ts";
import { expectCode, publishWorkingGraph, sdkPath } from "./helpers.ts";

describe("cross-app publication, installation, and invocation", () => {
  test("R-E2E-01 consumes generated definitions after the producer source is gone", async () => {
    const fixture = await publishWorkingGraph();
    const generatedClient = await readFile(
      join(fixture.greeterProject, "node_modules", "@gestalt", "apps", "users", "index.ts"),
      "utf8",
    );
    expect(generatedClient).toContain("export type GetUserOutput");
    expect(generatedClient).toContain('"displayName": string');

    const installation = new InstallationManager(fixture.registry);
    await expectCode(installation.invoke("greet", { userId: "42" }), "NO_ACTIVE_INSTALLATION");
    await installation.activate("acme/greeter", "1.0.0");
    expect(await installation.invoke("greet", { userId: "42", punctuation: "." })).toEqual({
      message: "Hello, Ada Lovelace.",
    });
  });

  test("R-E2E-02 enforces the same derived contracts on input and output", async () => {
    const fixture = await publishWorkingGraph();
    const installation = new InstallationManager(fixture.registry);
    await installation.activate("acme/greeter", "1.0.0");
    await expectCode(installation.invoke("greet", { userId: 42 }), "VALIDATION_FAILED");
    await expectCode(
      installation.invoke("greet", { userId: "42", unexpected: true }),
      "VALIDATION_FAILED",
    );

    const liarSource = join(fixture.root, "liar.ts");
    await writeFile(
      liarSource,
      `
        import { z } from "zod";
        import { app, tool } from ${JSON.stringify(sdkPath)};
        export default app({ tools: { lie: tool({
          input: z.strictObject({ value: z.string() }),
          output: z.strictObject({ value: z.string() }),
          handler: async (_input) => ({
            value: 42 as unknown as string,
          }),
        }) } });
      `,
    );
    await fixture.registry.publish(liarSource, {
      name: "acme/liar",
      version: "1.0.0",
      dependencies: {},
    });
    await installation.activate("acme/liar", "1.0.0");
    await expectCode(installation.invoke("lie", { value: "truth" }), "VALIDATION_FAILED");
  });

  test("R-E2E-03 returns structured failures from dependency handlers", async () => {
    const fixture = await publishWorkingGraph();
    const source = join(fixture.root, "failure.ts");
    await writeFile(
      source,
      `
        import { z } from "zod";
        import { app, tool } from ${JSON.stringify(sdkPath)};
        export default app({ tools: { fail: tool({
          input: z.strictObject({ value: z.string() }),
          output: z.strictObject({ ok: z.boolean() }),
          handler: async (_input) => {
            throw new Error("intentional provider failure");
          },
        }) } });
      `,
    );
    await fixture.registry.publish(source, {
      name: "acme/failure",
      version: "1.0.0",
      dependencies: {},
    });
    const installation = new InstallationManager(fixture.registry);
    await installation.activate("acme/failure", "1.0.0");
    try {
      await installation.invoke("fail", { value: "x" });
      throw new Error("expected handler failure");
    } catch (error) {
      expect(error).toBeInstanceOf(ExperimentError);
      expect((error as ExperimentError).code).toBe("HANDLER_FAILED");
      expect((error as Error).message).toContain("intentional provider failure");
    }
  });
});

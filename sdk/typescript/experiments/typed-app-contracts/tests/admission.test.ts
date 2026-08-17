import { appendFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";

import { digest, stableStringify, type PublishedRelease } from "../src/model.ts";
import { FilesystemRegistry } from "../src/registry.ts";
import { InstallationManager } from "../src/runtime.ts";
import { expectCode, manifest, publishWorkingGraph, sdkPath } from "./helpers.ts";

describe("recursive admission and non-disruptive activation", () => {
  test("R-ADM-01 validates the exact recursive graph and rejects cycles before promotion", async () => {
    const fixture = await publishWorkingGraph();
    const installation = new InstallationManager(fixture.registry);
    await installation.activate("acme/greeter", "1.0.0");
    const stable = installation.activeIdentity;

    const cycleSource = join(fixture.root, "cycle.ts");
    await writeFile(
      cycleSource,
      `import { app, tool } from ${JSON.stringify(sdkPath)};
       export default app({ tools: { ping: tool({ handler: async (input: { value: string }): Promise<{ value: string }> => input }) } });`,
    );
    const a = await fixture.registry.publish(cycleSource, manifest("acme/cycle-a", "1.0.0"));
    const b = await fixture.registry.publish(cycleSource, manifest("acme/cycle-b", "1.0.0"));
    await rewriteDependencies(fixture.registry, a, {
      b: { app: "acme/cycle-b", version: "1.0.0", contractDigest: b.contractDigest },
    });
    await rewriteDependencies(fixture.registry, b, {
      a: { app: "acme/cycle-a", version: "1.0.0", contractDigest: a.contractDigest },
    });

    await expectCode(installation.activate("acme/cycle-a", "1.0.0"), "DEPENDENCY_CYCLE");
    expect(installation.activeIdentity).toBe(stable);
    expect(await installation.invoke("greet", { userId: "42" })).toEqual({
      message: "Hello, Ada Lovelace!",
    });
  });

  test("R-ADM-02 leaves the stable activation serving when candidate dependencies disappear", async () => {
    const fixture = await publishWorkingGraph();
    const installation = new InstallationManager(fixture.registry);
    await installation.activate("acme/greeter", "1.0.0");
    const stable = installation.activeIdentity;
    const release = await fixture.registry.get("acme/greeter", "1.0.0");
    await rewriteDependencies(fixture.registry, release, {
      users: {
        app: "acme/users",
        version: "9.9.9",
        contractDigest: release.manifest.dependencies.users!.contractDigest,
      },
    });
    await expectCode(installation.activate("acme/greeter", "1.0.0"), "MISSING_RELEASE");
    expect(installation.activeIdentity).toBe(stable);
    expect(await installation.invoke("greet", { userId: "42" })).toEqual({
      message: "Hello, Ada Lovelace!",
    });
  });

  test("R-ADM-03 rejects contract, client, manifest, and artifact tampering", async () => {
    for (const tamper of ["build", "contract", "client", "manifest", "artifact"] as const) {
      const fixture = await publishWorkingGraph();
      const release = await fixture.registry.get("acme/greeter", "1.0.0");
      const releasePath = fixture.registry.releasePath("acme/greeter", "1.0.0");
      if (tamper === "build") {
        release.build.bun = "0.0.0-tampered";
        await writeFile(releasePath, stableStringify(release));
      } else if (tamper === "contract") {
        release.contract.tools.greet!.description = "tampered";
        await writeFile(releasePath, stableStringify(release));
      } else if (tamper === "client") {
        release.clientTemplate += "// tampered\n";
        await writeFile(releasePath, stableStringify(release));
      } else if (tamper === "manifest") {
        release.manifest.dependencies.ghost = {
          app: "acme/ghost",
          version: "1.0.0",
          contractDigest: "0".repeat(64),
        };
        await writeFile(releasePath, stableStringify(release));
      } else {
        await appendFile(fixture.registry.artifactPath("acme/greeter", "1.0.0"), "\n// tampered\n");
      }
      const expected = {
        build: "BUILD_TAMPERED",
        contract: "CONTRACT_TAMPERED",
        client: "CLIENT_TAMPERED",
        manifest: "MANIFEST_TAMPERED",
        artifact: "ARTIFACT_TAMPERED",
      }[tamper];
      await expectCode(
        new InstallationManager(fixture.registry).activate("acme/greeter", "1.0.0"),
        expected,
      );
    }
  });
});

async function rewriteDependencies(
  registry: FilesystemRegistry,
  release: PublishedRelease,
  dependencies: PublishedRelease["manifest"]["dependencies"],
): Promise<void> {
  const rewritten: PublishedRelease = structuredClone(release);
  rewritten.manifest.dependencies = dependencies;
  rewritten.manifestDigest = digest(stableStringify(rewritten.manifest));
  await writeFile(
    registry.releasePath(rewritten.manifest.name, rewritten.manifest.version),
    stableStringify(rewritten),
  );
}

import { appendFile, mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";

import type { AppManifest } from "../src/model.ts";
import {
  FilesystemRegistry,
  addDependency,
  initializeProject,
  readProjectManifest,
} from "../src/registry.ts";
import { InstallationManager } from "../src/runtime.ts";
import {
  greeterSourceText,
  expectCode,
  linkZod,
  manifest,
  publishWorkingGraph,
  sdkPath,
  usersSourceText,
} from "./helpers.ts";

describe("immutable publication and exact dependency locks", () => {
  test("R-PUB-01 emits a complete immutable release and rejects coordinate reuse", async () => {
    const fixture = await publishWorkingGraph();
    const release = fixture.usersV1;
    expect(release.contractDigest).toHaveLength(64);
    expect(release.manifestDigest).toHaveLength(64);
    expect(release.clientDigest).toHaveLength(64);
    expect(release.artifactDigest).toHaveLength(64);
    expect(release.sourceDigest).toHaveLength(64);
    expect(release.build.digest).toHaveLength(64);
    expect(await readFile(fixture.registry.artifactPath("acme/users", "1.0.0"))).not.toHaveLength(0);

    const replacement = join(fixture.root, "replacement.ts");
    await writeFile(replacement, usersSourceText("Grace Hopper"));
    await expectCode(
      fixture.registry.publish(replacement, manifest("acme/users", "1.0.0")),
      "IMMUTABLE_RELEASE",
    );
  });

  test("R-PUB-02 reproduces contracts, generated clients, and executable artifacts", async () => {
    const root = await mkdtemp(join(tmpdir(), "gestalt-reproduction-"));
    await linkZod(root);
    const firstSource = join(root, "one.ts");
    const secondSource = join(root, "nested", "two.ts");
    await mkdir(join(root, "nested"));
    await writeFile(firstSource, usersSourceText("Ada Lovelace"));
    await writeFile(secondSource, usersSourceText("Ada Lovelace"));
    const first = await new FilesystemRegistry(join(root, "registry-one")).publish(
      firstSource,
      manifest("acme/users", "1.0.0"),
    );
    const second = await new FilesystemRegistry(join(root, "registry-two")).publish(
      secondSource,
      manifest("acme/users", "1.0.0"),
    );
    expect(first.contractDigest).toBe(second.contractDigest);
    expect(first.clientDigest).toBe(second.clientDigest);
    expect(first.sourceDigest).toBe(second.sourceDigest);
    expect(first.artifactDigest).toBe(second.artifactDigest);
  });

  test("R-DEP-01 rejects ranges, missing releases, and stale contract locks", async () => {
    const root = await mkdtemp(join(tmpdir(), "gestalt-locks-"));
    await linkZod(root);
    const registry = new FilesystemRegistry(join(root, "registry"));
    const source = join(root, "app.ts");
    await writeFile(source, usersSourceText("Ada Lovelace"));

    const ranged: AppManifest = {
      name: "acme/consumer",
      version: "1.0.0",
      dependencies: {
        users: { app: "acme/users", version: "^1.0.0", contractDigest: "0".repeat(64) },
      },
    };
    await expectCode(registry.publish(source, ranged), "DEPENDENCY_NOT_EXACT");

    const missing: AppManifest = {
      ...ranged,
      dependencies: {
        users: { app: "acme/users", version: "1.0.0", contractDigest: "0".repeat(64) },
      },
    };
    await expectCode(registry.publish(source, missing), "MISSING_RELEASE");

    const v1 = await registry.publish(source, manifest("acme/users", "1.0.0"));
    const v2Source = join(root, "users-v2.ts");
    await writeFile(v2Source, usersSourceText("Grace Hopper"));
    await registry.publish(v2Source, manifest("acme/users", "2.0.0"));
    const stale: AppManifest = {
      name: "acme/consumer",
      version: "1.0.0",
      dependencies: {
        users: { app: "acme/users", version: "2.0.0", contractDigest: v1.contractDigest },
      },
    };
    await expectCode(registry.publish(source, stale), "INCOMPATIBLE_DEPENDENCY");
  });

  test("R-DEP-02 rejects undeclared, unused, and statically mistyped app imports", async () => {
    const root = await mkdtemp(join(tmpdir(), "gestalt-usage-"));
    await linkZod(root);
    const registry = new FilesystemRegistry(join(root, "registry"));
    const usersSource = join(root, "users.ts");
    await writeFile(usersSource, usersSourceText("Ada Lovelace"));
    await registry.publish(usersSource, manifest("acme/users", "1.0.0"));

    const project = join(root, "consumer");
    await initializeProject(project, manifest("acme/consumer", "1.0.0"));
    await addDependency({ registry, projectDirectory: project, alias: "users", app: "acme/users", version: "1.0.0" });
    const source = join(project, "app.ts");

    await writeFile(
      source,
      `import { z } from "zod";
       import { app, tool } from ${JSON.stringify(sdkPath)};
       const Message = z.strictObject({ id: z.string() });
       export default app({ tools: { local: tool({ input: Message, output: Message, handler: async (input) => input }) } });`,
    );
    await expectCode(
      registry.publish(source, await readProjectManifest(project)),
      "UNUSED_DEPENDENCY",
    );

    await writeFile(source, greeterSourceText());
    await expectCode(
      registry.publish(source, manifest("acme/undeclared", "1.0.0")),
      "UNDECLARED_DEPENDENCY",
    );

    await writeFile(source, greeterSourceText("42"));
    await expectCode(
      registry.publish(source, {
        ...(await readProjectManifest(project)),
        name: "acme/mistyped",
      }),
      "TYPESCRIPT_ERROR",
    );

    await writeFile(
      source,
      `import { z } from "zod";
       import { app, tool } from ${JSON.stringify(sdkPath)};
       export default app({ tools: { dynamic: tool({
         input: z.strictObject({ id: z.string() }),
         output: z.strictObject({ name: z.string() }),
         handler: async (input) => {
         const { getUser } = await import("@gestalt/apps/users");
         const user = await getUser(input);
         return { name: user.displayName };
       } }) } });`,
    );
    await expectCode(
      registry.publish(source, {
        ...(await readProjectManifest(project)),
        name: "acme/dynamic",
      }),
      "DYNAMIC_APP_IMPORT",
    );

    await writeFile(source, greeterSourceText());
    await appendFile(
      join(project, "node_modules", "@gestalt", "apps", "users", "index.ts"),
      "\n// stale local edit\n",
    );
    await expectCode(
      registry.publish(source, {
        ...(await readProjectManifest(project)),
        name: "acme/stale-client",
      }),
      "STALE_GENERATED_CLIENT",
    );
  });

  test("R-DEP-03 keeps an installed consumer pinned when a newer producer is published", async () => {
    const fixture = await publishWorkingGraph();
    const v2Source = join(fixture.root, "users-v2.ts");
    await writeFile(v2Source, usersSourceText("Grace Hopper"));
    await fixture.registry.publish(v2Source, manifest("acme/users", "2.0.0"));

    const installation = new InstallationManager(fixture.registry);
    await installation.activate("acme/greeter", "1.0.0");
    expect(await installation.invoke("greet", { userId: "42" })).toEqual({
      message: "Hello, Ada Lovelace!",
    });
  });
});

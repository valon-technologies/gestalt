import { cp, mkdir, mkdtemp, unlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect } from "bun:test";

import { ExperimentError, type AppManifest, type PublishedRelease } from "../src/model.ts";
import {
  FilesystemRegistry,
  addDependency,
  initializeProject,
  readProjectManifest,
} from "../src/registry.ts";

export const sdkPath = join(import.meta.dir, "..", "src", "sdk.ts");
const zodPath = join(import.meta.dir, "..", "node_modules", "zod");

export interface PublishedFixture {
  root: string;
  registry: FilesystemRegistry;
  usersV1: PublishedRelease;
  greeter: PublishedRelease;
  greeterProject: string;
}

export async function publishWorkingGraph(): Promise<PublishedFixture> {
  const root = await mkdtemp(join(tmpdir(), "gestalt-functional-"));
  await linkZod(root);
  const registry = new FilesystemRegistry(join(root, "registry"));
  const usersSource = join(root, "users.ts");
  await writeFile(usersSource, usersSourceText("Ada Lovelace"));
  const usersV1 = await registry.publish(usersSource, manifest("acme/users", "1.0.0"));

  // This deletion is the key independence check: consumers and installation only see the release.
  await unlink(usersSource);

  const greeterProject = join(root, "greeter");
  await initializeProject(greeterProject, manifest("acme/greeter", "1.0.0"));
  await addDependency({
    registry,
    projectDirectory: greeterProject,
    alias: "users",
    app: "acme/users",
    version: "1.0.0",
  });
  const greeterSource = join(greeterProject, "app.ts");
  await writeFile(greeterSource, greeterSourceText());
  const greeter = await registry.publish(greeterSource, await readProjectManifest(greeterProject));
  return { root, registry, usersV1, greeter, greeterProject };
}

export function manifest(name: string, version: string): AppManifest {
  return { name, version, dependencies: {} };
}

export function usersSourceText(displayName: string): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};

    const GetUserInput = z.strictObject({ id: z.string() });
    const GetUserOutput = z.strictObject({
      id: z.string(),
      displayName: z.string(),
      status: z.enum(["active", "disabled"]),
    });

    export default app({ tools: {
      getUser: tool({
        description: "Fetch one user.",
        input: GetUserInput,
        output: GetUserOutput,
        handler: async (input) => ({
          id: input.id,
          displayName: ${JSON.stringify(displayName)},
          status: "active" as const,
        }),
      }),
    } });
  `;
}

export function greeterSourceText(idExpression = "input.userId"): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};
    import { getUser } from "@gestalt/apps/users";

    const GreetInput = z.strictObject({
      userId: z.string(),
      punctuation: z.enum(["!", "."]).optional(),
    });
    const GreetOutput = z.strictObject({ message: z.string() });

    export default app({ tools: {
      greet: tool({
        description: "Greet a user owned by another installed app.",
        input: GreetInput,
        output: GreetOutput,
        handler: async (input) => {
          const user = await getUser({ id: ${idExpression} });
          return { message: \`Hello, \${user.displayName}\${input.punctuation ?? "!"}\` };
        },
      }),
    } });
  `;
}

export async function linkZod(projectDirectory: string): Promise<void> {
  const nodeModules = join(projectDirectory, "node_modules");
  await mkdir(nodeModules, { recursive: true });
  await cp(zodPath, join(nodeModules, "zod"), { recursive: true });
}

export async function expectCode(promise: Promise<unknown>, code: string): Promise<void> {
  try {
    await promise;
    throw new Error(`expected ${code}`);
  } catch (error) {
    expect(error).toBeInstanceOf(ExperimentError);
    expect((error as ExperimentError).code).toBe(code);
  }
}

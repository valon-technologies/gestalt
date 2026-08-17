import { AsyncLocalStorage } from "node:async_hooks";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

import {
  ExperimentError,
  digest,
  stableStringify,
  type AppManifest,
  type PublishedRelease,
} from "./model.ts";
import { FilesystemRegistry } from "./registry.ts";
import type { App, Tool } from "./sdk.ts";

interface InstalledRelease {
  key: string;
  release: PublishedRelease;
  app: App<Record<string, Tool<any, any>>>;
}

interface InstalledGraph {
  root: string;
  releases: Map<string, InstalledRelease>;
}

export class InstallationManager {
  private active: InstalledGraph | undefined;
  private readonly caller = new AsyncLocalStorage<string>();

  constructor(private readonly registry: FilesystemRegistry) {
    globalThis.__gestaltExperimentInvoke = async (alias, tool, input, contractDigest) => {
      const sourceKey = this.caller.getStore();
      if (!sourceKey || !this.active) {
        throw new ExperimentError("NO_INVOCATION_CONTEXT", "generated clients only work inside an app tool");
      }
      const source = this.active.releases.get(sourceKey);
      const dependency = source?.release.manifest.dependencies[alias];
      if (!source || !dependency) {
        throw new ExperimentError("UNDECLARED_DEPENDENCY", `${sourceKey} cannot invoke alias ${alias}`);
      }
      const targetKey = coordinate(dependency.app, dependency.version);
      const target = this.active.releases.get(targetKey);
      const expected = target?.release.contract.tools[tool];
      if (!target || !expected) {
        throw new ExperimentError("INVALID_TOOL_REFERENCE", `${alias}.${tool} does not exist`);
      }
      if (expected.digest !== contractDigest) {
        throw new ExperimentError("INCOMPATIBLE_TOOL_CONTRACT", `${alias}.${tool} changed after generation`);
      }
      return await this.invokeRelease(target, tool, input);
    };
  }

  get activeIdentity(): string | undefined {
    return this.active?.root;
  }

  async activate(app: string, version: string): Promise<void> {
    const root = coordinate(app, version);
    const releases = new Map<string, InstalledRelease>();
    await this.loadCandidate(app, version, [], releases);
    this.active = { root, releases };
  }

  async invoke(tool: string, input: unknown): Promise<unknown> {
    if (!this.active) {
      throw new ExperimentError("NO_ACTIVE_INSTALLATION", "install an app before routing traffic");
    }
    const root = this.active.releases.get(this.active.root);
    if (!root) throw new ExperimentError("INVALID_SNAPSHOT", "active root is absent");
    return await this.invokeRelease(root, tool, input);
  }

  private async loadCandidate(
    app: string,
    version: string,
    stack: string[],
    releases: Map<string, InstalledRelease>,
  ): Promise<void> {
    const key = coordinate(app, version);
    if (stack.includes(key)) {
      throw new ExperimentError("DEPENDENCY_CYCLE", [...stack, key].join(" -> "));
    }
    if (releases.has(key)) return;
    const release = await this.registry.get(app, version);
    await this.verifyRelease(key, release);
    for (const dependency of Object.values(release.manifest.dependencies)) {
      const dependencyRelease = await this.registry.get(dependency.app, dependency.version);
      if (dependencyRelease.contractDigest !== dependency.contractDigest) {
        throw new ExperimentError(
          "INCOMPATIBLE_DEPENDENCY",
          `${key} locked ${dependency.app}@${dependency.version} to a different contract`,
        );
      }
      await this.loadCandidate(dependency.app, dependency.version, [...stack, key], releases);
    }
    const moduleUrl = pathToFileURL(this.registry.artifactPath(app, version));
    moduleUrl.searchParams.set("digest", release.artifactDigest);
    const module = (await import(moduleUrl.href)) as {
      default?: App<Record<string, Tool<any, any>>>;
    };
    if (!module.default || typeof module.default !== "object" || !module.default.tools) {
      throw new ExperimentError("INVALID_ARTIFACT", `${key} has no default app export`);
    }
    releases.set(key, { key, release, app: module.default });
  }

  private async verifyRelease(key: string, release: PublishedRelease): Promise<void> {
    if (coordinate(release.manifest.name, release.manifest.version) !== key) {
      throw new ExperimentError("INVALID_RELEASE", `${key} contains another coordinate`);
    }
    const { digest: buildDigest, ...buildInputs } = release.build;
    if (digest(stableStringify(buildInputs)) !== buildDigest) {
      throw new ExperimentError("BUILD_TAMPERED", `${key}'s build identity digest is invalid`);
    }
    if (digest(stableStringify(release.manifest)) !== release.manifestDigest) {
      throw new ExperimentError("MANIFEST_TAMPERED", `${key}'s manifest digest is invalid`);
    }
    if (digest(stableStringify(release.contract)) !== release.contractDigest) {
      throw new ExperimentError("CONTRACT_TAMPERED", `${key}'s contract digest is invalid`);
    }
    if (digest(release.clientTemplate) !== release.clientDigest) {
      throw new ExperimentError("CLIENT_TAMPERED", `${key}'s client digest is invalid`);
    }
    const artifact = await readFile(this.registry.artifactPath(release.manifest.name, release.manifest.version));
    if (digest(artifact) !== release.artifactDigest) {
      throw new ExperimentError("ARTIFACT_TAMPERED", `${key}'s provider artifact digest is invalid`);
    }
  }

  private async invokeRelease(
    installed: InstalledRelease,
    toolName: string,
    rawInput: unknown,
  ): Promise<unknown> {
    const contract = installed.release.contract.tools[toolName];
    const tool = installed.app.tools[toolName];
    if (!contract || !tool) {
      throw new ExperimentError("INVALID_TOOL_REFERENCE", `${installed.key}.${toolName} does not exist`);
    }
    const parsedInput = await tool.input.safeParseAsync(rawInput);
    if (!parsedInput.success) {
      throw validationError("$.input", parsedInput.error.issues);
    }
    let output: unknown;
    try {
      output = await this.caller.run(
        installed.key,
        async () => await tool.handler(parsedInput.data),
      );
    } catch (error) {
      if (error instanceof ExperimentError) throw error;
      throw new ExperimentError(
        "HANDLER_FAILED",
        error instanceof Error ? error.message : String(error),
      );
    }
    const parsedOutput = await tool.output.safeParseAsync(output);
    if (!parsedOutput.success) {
      throw validationError("$.output", parsedOutput.error.issues);
    }
    return parsedOutput.data;
  }
}

function coordinate(app: string, version: string): string {
  return `${app}@${version}`;
}

function validationError(
  root: string,
  issues: ReadonlyArray<{ code: string; path: PropertyKey[]; message: string }>,
): ExperimentError {
  const details = issues.map((issue) => ({
    code: issue.code,
    path: [root, ...issue.path.map(String)],
    message: issue.message,
  }));
  return new ExperimentError("VALIDATION_FAILED", JSON.stringify(details));
}

declare global {
  // The generated client deliberately has no transport dependency. Installation owns this hook.
  // eslint-disable-next-line no-var
  var __gestaltExperimentInvoke:
    | ((alias: string, tool: string, input: unknown, digest: string) => Promise<unknown>)
    | undefined;
}

export function manifestOf(release: PublishedRelease): AppManifest {
  return release.manifest;
}

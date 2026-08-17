import {
  access,
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

import { compileApp } from "./contracts.ts";
import {
  ExperimentError,
  FORMAT_VERSION,
  digest,
  stableStringify,
  validateManifest,
  type AppManifest,
  type PublishedRelease,
} from "./model.ts";

export class FilesystemRegistry {
  constructor(readonly root: string) {}

  async publish(entrypoint: string, manifest: AppManifest): Promise<PublishedRelease> {
    validateManifest(manifest);
    await this.verifyDeclaredDependencies(manifest);
    await this.verifyMaterializedClients(entrypoint, manifest);
    const releaseDirectory = this.releaseDirectory(manifest.name, manifest.version);
    if (await exists(releaseDirectory)) {
      throw new ExperimentError(
        "IMMUTABLE_RELEASE",
        `${manifest.name}@${manifest.version} is already published`,
      );
    }

    const compilation = await compileApp(entrypoint, manifest);
    const buildDirectory = await mkdtemp(join(tmpdir(), "gestalt-typed-contract-"));
    try {
      const result = await Bun.build({
        entrypoints: [entrypoint],
        outdir: buildDirectory,
        naming: "provider.js",
        target: "bun",
        format: "esm",
        sourcemap: "none",
        // Bun's readable output embeds entrypoint paths and basename-derived symbols.
        // Minification removes that non-semantic build-host input, making this toy artifact reproducible.
        minify: true,
      });
      if (!result.success) {
        const messages = result.logs.map((log) => log.message).join("\n");
        throw new ExperimentError("BUNDLE_FAILED", messages);
      }
      const artifactPath = join(buildDirectory, "provider.js");
      const artifact = await readFile(artifactPath);
      const build = {
        adapter: compilation.contract.compiler.adapter,
        typescript: compilation.contract.compiler.typescript,
        zod: compilation.contract.compiler.zod,
        bun: Bun.version,
      };
      const release: PublishedRelease = {
        formatVersion: FORMAT_VERSION,
        build: { ...build, digest: digest(stableStringify(build)) },
        manifest,
        manifestDigest: digest(stableStringify(manifest)),
        contract: compilation.contract,
        contractDigest: compilation.contractDigest,
        clientTemplate: compilation.clientTemplate,
        clientDigest: compilation.clientDigest,
        artifactFile: "provider.js",
        artifactDigest: digest(artifact),
        sourceDigest: compilation.sourceDigest,
      };

      await mkdir(dirname(releaseDirectory), { recursive: true });
      await mkdir(releaseDirectory);
      await copyFile(artifactPath, join(releaseDirectory, release.artifactFile));
      await writeFile(join(releaseDirectory, "contract.json"), stableStringify(release.contract));
      await writeFile(join(releaseDirectory, "client.template.ts"), release.clientTemplate);
      await writeFile(join(releaseDirectory, "release.json"), stableStringify(release));
      return release;
    } finally {
      await rm(buildDirectory, { recursive: true, force: true });
    }
  }

  async get(app: string, version: string): Promise<PublishedRelease> {
    const path = join(this.releaseDirectory(app, version), "release.json");
    try {
      return JSON.parse(await readFile(path, "utf8")) as PublishedRelease;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") {
        throw new ExperimentError("MISSING_RELEASE", `${app}@${version} is not published`);
      }
      throw error;
    }
  }

  artifactPath(app: string, version: string): string {
    return join(this.releaseDirectory(app, version), "provider.js");
  }

  releasePath(app: string, version: string): string {
    return join(this.releaseDirectory(app, version), "release.json");
  }

  private releaseDirectory(app: string, version: string): string {
    return join(this.root, "apps", ...app.split("/"), version);
  }

  private async verifyDeclaredDependencies(manifest: AppManifest): Promise<void> {
    for (const dependency of Object.values(manifest.dependencies)) {
      const release = await this.get(dependency.app, dependency.version);
      if (release.contractDigest !== dependency.contractDigest) {
        throw new ExperimentError(
          "INCOMPATIBLE_DEPENDENCY",
          `${dependency.app}@${dependency.version} does not match the locked contract digest`,
        );
      }
    }
  }

  private async verifyMaterializedClients(entrypoint: string, manifest: AppManifest): Promise<void> {
    for (const [alias, dependency] of Object.entries(manifest.dependencies)) {
      const release = await this.get(dependency.app, dependency.version);
      const path = join(dirname(entrypoint), "node_modules", "@gestalt", "apps", alias, "index.ts");
      let actual: string;
      try {
        actual = await readFile(path, "utf8");
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code === "ENOENT") {
          throw new ExperimentError(
            "STALE_GENERATED_CLIENT",
            `${alias} is locked but its generated client is not materialized beside the app`,
          );
        }
        throw error;
      }
      const expected = release.clientTemplate.replaceAll("__GESTALT_ALIAS__", alias);
      if (digest(actual) !== digest(expected)) {
        throw new ExperimentError(
          "STALE_GENERATED_CLIENT",
          `${alias}'s generated client does not match ${dependency.app}@${dependency.version}`,
        );
      }
    }
  }
}

export async function initializeProject(projectDirectory: string, manifest: AppManifest): Promise<void> {
  validateManifest(manifest);
  await mkdir(projectDirectory, { recursive: true });
  await writeFile(join(projectDirectory, "gestalt.json"), stableStringify(manifest));
}

export async function readProjectManifest(projectDirectory: string): Promise<AppManifest> {
  return JSON.parse(await readFile(join(projectDirectory, "gestalt.json"), "utf8")) as AppManifest;
}

export async function addDependency(options: {
  registry: FilesystemRegistry;
  projectDirectory: string;
  alias: string;
  app: string;
  version: string;
}): Promise<AppManifest> {
  const { registry, projectDirectory, alias, app, version } = options;
  if (!/^[a-z][a-zA-Z0-9]*$/.test(alias)) {
    throw new ExperimentError("INVALID_ALIAS", alias);
  }
  const release = await registry.get(app, version);
  const manifest = await readProjectManifest(projectDirectory);
  manifest.dependencies[alias] = { app, version, contractDigest: release.contractDigest };
  validateManifest(manifest);
  await writeFile(join(projectDirectory, "gestalt.json"), stableStringify(manifest));

  const packageDirectory = join(projectDirectory, "node_modules", "@gestalt", "apps", alias);
  await mkdir(packageDirectory, { recursive: true });
  const client = release.clientTemplate.replaceAll("__GESTALT_ALIAS__", alias);
  await writeFile(join(packageDirectory, "index.ts"), client);
  await writeFile(
    join(packageDirectory, "package.json"),
    stableStringify({
      name: `@gestalt/apps/${alias}`,
      version: release.manifest.version,
      type: "module",
      exports: "./index.ts",
    }),
  );
  return manifest;
}

async function exists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

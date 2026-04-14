import { expect, test } from "bun:test";

import {
  defaultProviderName,
  formatProviderTarget,
  parseModuleTarget,
  parseProviderTarget,
  readPackageConfig,
  readPackagePluginTarget,
  readPackageProviderTarget,
  resolveProviderModulePath,
} from "../src/target.ts";
import { fixturePath } from "./helpers.ts";

test("module target parsing validates relative module paths", () => {
  expect(parseModuleTarget("./provider.ts#plugin")).toEqual({
    modulePath: "./provider.ts",
    exportName: "plugin",
  });
  expect(parseModuleTarget("./provider.ts")).toEqual({
    modulePath: "./provider.ts",
  });

  expect(() => parseModuleTarget("provider.ts")).toThrow(
    "gestalt provider target module path must be relative",
  );
  expect(() => parseModuleTarget("./provider.ts#not-valid!")).toThrow(
    "gestalt provider target export must be a valid JavaScript identifier",
  );
});

test("provider target parsing supports plugin defaults and kind prefixes", () => {
  expect(parseProviderTarget("./provider.ts#plugin")).toEqual({
    kind: "integration",
    modulePath: "./provider.ts",
    exportName: "plugin",
  });
  expect(parseProviderTarget("plugin:./provider.ts#plugin")).toEqual({
    kind: "integration",
    modulePath: "./provider.ts",
    exportName: "plugin",
  });
  expect(parseProviderTarget("integration:./provider.ts#plugin")).toEqual({
    kind: "integration",
    modulePath: "./provider.ts",
    exportName: "plugin",
  });
  expect(parseProviderTarget("auth:./auth.ts#provider")).toEqual({
    kind: "auth",
    modulePath: "./auth.ts",
    exportName: "provider",
  });
  expect(parseProviderTarget("cache:./cache.ts#provider")).toEqual({
    kind: "cache",
    modulePath: "./cache.ts",
    exportName: "provider",
  });
});

test("package config reads legacy plugin targets and provider targets", () => {
  const pluginRoot = fixturePath("basic-provider");
  expect(readPackageConfig(pluginRoot)).toEqual({
    name: "@fixtures/basic-provider",
    providerTarget: {
      kind: "integration",
      modulePath: "./provider.ts",
      exportName: "plugin",
    },
  });
  expect(formatProviderTarget(readPackageProviderTarget(pluginRoot))).toBe(
    "plugin:./provider.ts#plugin",
  );
  expect(readPackagePluginTarget(pluginRoot)).toBe("./provider.ts#plugin");
  expect(defaultProviderName(pluginRoot)).toBe("basic-provider");
  expect(
    resolveProviderModulePath(pluginRoot, readPackageProviderTarget(pluginRoot)),
  ).toContain("/provider.ts");

  const authRoot = fixturePath("auth-provider");
  expect(readPackageConfig(authRoot)).toEqual({
    name: "@fixtures/auth-provider",
    providerTarget: {
      kind: "auth",
      modulePath: "./auth.ts",
      exportName: "provider",
    },
  });
  expect(formatProviderTarget(readPackageProviderTarget(authRoot))).toBe(
    "auth:./auth.ts#provider",
  );

  const cacheRoot = fixturePath("cache-provider");
  expect(readPackageConfig(cacheRoot)).toEqual({
    name: "@fixtures/cache-provider",
    providerTarget: {
      kind: "cache",
      modulePath: "./cache.ts",
      exportName: "provider",
    },
  });
  expect(formatProviderTarget(readPackageProviderTarget(cacheRoot))).toBe(
    "cache:./cache.ts#provider",
  );
});

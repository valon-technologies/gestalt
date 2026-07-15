import { afterEach, describe, expect, test } from "bun:test";

import { appApiBase, appName, base } from "../src/mount.ts";

type BaseElement = {
  tagName: string;
  href: string;
  getAttribute?: (name: string) => string | null;
};

type MetaElement = {
  tagName: string;
  content: string;
  getAttribute?: (name: string) => string | null;
};

function installDocument(
  baseEl: BaseElement | null,
  appMeta: MetaElement | null = null,
): void {
  (globalThis as {
    document?: {
      querySelector(selector: string): BaseElement | MetaElement | null;
    };
  }).document = {
    querySelector(selector: string) {
      if (selector === "base[href]") {
        return baseEl;
      }
      if (selector === 'meta[name="gestalt-app-name"]') {
        return appMeta;
      }
      return null;
    },
  };
}

describe("mount", () => {
  afterEach(() => {
    delete (globalThis as { document?: unknown }).document;
  });

  test("base throws without a browser document", () => {
    expect(() => base()).toThrow("base() requires a browser document");
  });

  test("base throws without a base href element", () => {
    installDocument(null);
    expect(() => base()).toThrow("base() requires <base href> to be present in the document");
  });

  test("base returns the mount prefix without a trailing slash", () => {
    installDocument({
      tagName: "BASE",
      href: "http://example.com/example-app/",
    });
    expect(base()).toBe("/example-app");
  });

  test("base resolves a relative base href from HTMLBaseElement.href", () => {
    installDocument({
      tagName: "BASE",
      href: "http://example.com/mounted-app/",
      getAttribute(name: string) {
        return name === "href" ? "./" : null;
      },
    });
    expect(base()).toBe("/mounted-app");
  });

  test("base returns empty string for a root mount", () => {
    installDocument({
      tagName: "BASE",
      href: "http://example.com/",
    });
    expect(base()).toBe("");
  });

  test("appName throws without a browser document", () => {
    expect(() => appName()).toThrow("appName() requires a browser document");
  });

  test("appName throws without gestalt app metadata", () => {
    installDocument(null, null);
    expect(() => appName()).toThrow("gestalt-app-name");
  });

  test("appName throws for empty metadata content", () => {
    installDocument(null, { tagName: "META", content: "   " });
    expect(() => appName()).toThrow("gestalt-app-name");
  });

  test("appName and appApiBase use trimmed registered app key", () => {
    installDocument(null, { tagName: "META", content: "  ci_cd  " });
    expect(appName()).toBe("ci_cd");
    expect(appApiBase()).toBe("/api/v1/ci_cd");
  });

  test("appApiBase uri-encodes app keys with reserved characters", () => {
    installDocument(null, { tagName: "META", content: "app/with space" });
    expect(appApiBase()).toBe("/api/v1/app%2Fwith%20space");
  });

  test("appApiBase does not derive identity from base href mount", () => {
    installDocument(
      {
        tagName: "BASE",
        href: "http://example.com/ci-cd/",
      },
      { tagName: "META", content: "ci_cd" },
    );
    expect(appApiBase()).toBe("/api/v1/ci_cd");
  });
});

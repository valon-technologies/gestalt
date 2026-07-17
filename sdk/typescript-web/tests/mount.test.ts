import { afterEach, describe, expect, test } from "bun:test";

import { base } from "../src/mount.ts";

type BaseElement = {
  tagName: string;
  href: string;
  getAttribute?: (name: string) => string | null;
};

function installDocument(baseEl: BaseElement | null): void {
  (globalThis as { document?: { querySelector(selector: string): BaseElement | null } }).document = {
    querySelector(selector: string) {
      if (selector === "base[href]") {
        return baseEl;
      }
      return null;
    },
  };
}

describe("base", () => {
  afterEach(() => {
    delete (globalThis as { document?: unknown }).document;
  });

  test("throws without a browser document", () => {
    expect(() => base()).toThrow("base() requires a browser document");
  });

  test("throws without a base href element", () => {
    installDocument(null);
    expect(() => base()).toThrow("base() requires <base href> to be present in the document");
  });

  test("returns the mount prefix without a trailing slash", () => {
    installDocument({
      tagName: "BASE",
      href: "http://example.com/example-app/",
    });
    expect(base()).toBe("/example-app");
  });

  test("resolves a relative base href from HTMLBaseElement.href", () => {
    installDocument({
      tagName: "BASE",
      href: "http://example.com/mounted-app/",
      getAttribute(name: string) {
        return name === "href" ? "./" : null;
      },
    });
    expect(base()).toBe("/mounted-app");
  });

  test("returns empty string for a root mount", () => {
    installDocument({
      tagName: "BASE",
      href: "http://example.com/",
    });
    expect(base()).toBe("");
  });
});

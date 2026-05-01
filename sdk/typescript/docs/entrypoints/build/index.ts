/**
 * Build-time helpers for producing standalone Gestalt provider binaries.
 *
 * `gestaltd provider release` uses this entrypoint for TypeScript source
 * providers. It loads the configured provider target, bundles it with Bun, and
 * writes the executable artifact for the requested target platform.
 *
 * @module
 */
export * from "../../../src/build.ts";

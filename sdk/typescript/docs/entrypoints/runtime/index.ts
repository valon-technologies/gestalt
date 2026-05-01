/**
 * Runtime helpers for loading and serving TypeScript providers locally.
 *
 * The runtime entrypoint reads the configured provider target, starts the
 * matching gRPC provider server, and connects back to the `gestaltd` host over
 * the socket supplied by the provider process environment.
 *
 * @module
 */
export * from "../../../src/runtime.ts";

/**
 * Public API surface for executable Gestalt providers.
 *
 * Use this package to define provider runtimes, operation schemas, handlers,
 * host-service clients, and provider-owned telemetry. The provider manifest
 * still owns static identity, connections, hosted HTTP routes, passthrough
 * surfaces, and release metadata.
 *
 * @example
 * ```ts
 * import { definePlugin, ok, operation, s } from "@valon-technologies/gestalt";
 *
 * export const plugin = definePlugin({
 *   displayName: "Example Provider",
 *   operations: [
 *     operation({
 *       id: "hello",
 *       input: s.object({ name: s.string({ default: "World" }) }),
 *       output: s.object({ message: s.string() }),
 *       async handler(input) {
 *         return ok({ message: `Hello, ${input.name}` });
 *       },
 *     }),
 *   ],
 * });
 * ```
 *
 * @module
 */
export * from "../../../../src/index.ts";

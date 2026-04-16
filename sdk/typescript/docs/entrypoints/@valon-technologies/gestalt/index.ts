/**
 * Public API surface for `@valon-technologies/gestalt`.
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

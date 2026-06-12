/**
 * Provider-serving runtime: the gRPC services that expose authored providers
 * to `gestaltd`, and the provider-protocol bridges for code that receives
 * Gestalt requests through its own protocol surface.
 *
 * Import from `@valon-technologies/gestalt/runtime`. The provider-loading
 * internals that share this module at runtime are intentionally unexported.
 *
 * @module runtime
 */
export { nativeRequestContext, serve } from "./runtime.ts";

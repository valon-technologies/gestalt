/**
 * OpenTelemetry helpers for provider-authored GenAI instrumentation.
 *
 * `gestaltd` owns exporter configuration. Providers use these helpers around
 * model calls, agent invocations, and tool execution that happen inside their
 * own process.
 *
 * @module
 */
export * from "../../../src/telemetry.ts";

# Gestalt TypeScript SDK

Use the TypeScript SDK to build Bun-based executable Gestalt providers with
explicit runtime schemas.

The package is published as `@valon-technologies/gestalt`.

```sh
bun add @valon-technologies/gestalt@0.0.1-alpha.16
```

## API sections

The TypeDoc reference is organized around the provider-authoring workflow. All
symbols are imported from `@valon-technologies/gestalt` unless the section
explicitly names a submodule.

| Section | Start with | Use it for |
| --- | --- | --- |
| Provider authoring | `defineApp`, `operation`, `ok` | Executable app providers, typed request handlers, and operation results. |
| Runtime schemas | `s`, `object`, `string` | Runtime validation and generated catalog metadata for operation inputs and outputs. |
| Provider runtimes | `defineIdentityProvider`, `defineCacheProvider`, `defineS3Provider`, `defineWorkflowProvider`, `defineAgentProvider` | Host-service backends implemented as TypeScript providers. |
| Workflow and agent models | `WorkflowProvider`, `Workflow`, `AgentProvider`, `Agent` | Native workflow values, agent sessions, turns, messages, tools, and host-service access. |
| Host-service clients | `Cache`, `IndexedDB`, `S3`, `App` | Calling sibling services exposed to a provider process by `gestaltd`. |
| Telemetry | `withModelOperation`, `withToolExecution`, `withAgentInvocation` | Provider-authored GenAI spans and metrics inside a running provider process. |

```ts
import { defineApp, ok, operation, s } from "@valon-technologies/gestalt";

export const app = defineApp({
  displayName: "Search",
  operations: [
    operation({
      id: "search",
      method: "GET",
      readOnly: true,
      input: s.object({
        query: s.string({ description: "Search query" }),
        limit: s.integer({ description: "Maximum results", default: 10 }),
      }),
      output: s.object({
        results: s.array(s.string()),
      }),
      async handler(input) {
        return ok({ results: [input.query].slice(0, input.limit) });
      },
    }),
  ],
});
```

## Provider target

Point the package at the provider module with a top-level `gestalt.provider`
property in `package.json`.

```json
{
  "name": "my-provider",
  "version": "0.0.1",
  "private": true,
  "type": "module",
  "dependencies": {
    "@valon-technologies/gestalt": "0.0.1-alpha.16"
  },
  "gestalt": {
    "provider": {
      "kind": "app",
      "target": "./provider.ts#app"
    }
  }
}
```

The target is a relative file path with an optional export suffix. If the suffix
is omitted, the runtime looks for `provider`, then `app`, then the default
export.

Use `"app"` as the kind token for executable app providers. Use an object
target with an explicit kind for authentication, cache, IndexedDB, S3,
secrets, workflow, agent, and hosted-runtime providers.

## Public surface

The root package exports provider definition helpers:

- `defineApp` for integration operations and session catalogs.
- `defineIdentityProvider` for authentication surfaces.
- `defineCacheProvider`, `defineIndexedDBProvider`, `defineS3Provider`, and
  `defineSecretsProvider` for host-service backends.
- `defineWorkflowProvider`, `defineAgentProvider`, and
  `defineRuntimeProvider` for workflow, agent, and hosted-runtime
  backends.
- `s` schema builders for operation input, output, and catalog metadata.
- Host-service clients for cache, IndexedDB, S3, workflows, agents,
  invocations, and telemetry.

`defineAgentProvider` handlers receive and return plain TypeScript objects such
as `CreateAgentProviderTurnRequest`, `AgentSession`, `AgentTurn`, and
`AgentTurnEvent`. Structured payload fields accept JSON values and timestamp
fields use native `Date`; the SDK runtime owns transport serialization at the
transport boundary.
Workflow helpers such as `boundWorkflowTarget`, `workflowSignal`, and
`boundWorkflowRun` accept plain JSON objects
and native `Date` values. Copy helpers such as `boundWorkflowTargetFromTarget`
preserve request shape without requiring provider code to assemble transport
objects. Provider-facing APIs should accept native TypeScript values and keep
transport serialization inside SDK adapters.

TypeScript types are not enough to describe runtime payloads. Use the schema
builders for every operation input and output that should appear in the
Gestalt catalog.

## App invocation migration

`request.app()` returns an app client whose `invoke<T>()` and
`invokeGraphQL<T>()` methods decode JSON responses and unwrap only Gestalt
success envelopes shaped as `{ status: "success", data: ... }`.

```ts
const app = await request.app();

// before
const raw = await app.invokeRaw("github", "get_issue", params);
const issue = raw.json<Issue>();

// after
const issue = await app.invoke<Issue>("github", "get_issue", params);

// escape hatch
const raw = await app.invokeRaw("github", "get_issue", params);
```

Typed GraphQL invocation throws `InvokeError` when the decoded top-level
payload has a non-empty `errors` array. Use `invokeGraphQLRaw()` for callers
that need to classify GraphQL errors themselves.

## Runtime and build entrypoints

Source-mode runtime:

```sh
bun run gestalt run ROOT app:./provider.ts#app
```

Release build:

```sh
bun run gestalt build ROOT app:./provider.ts#app OUTPUT PROVIDER_NAME GOOS GOARCH
```

With no positional args, the build entrypoint derives every field from
`manifest.yaml` plus `package.json` and the `GESTALT_TARGET_OS` /
`GESTALT_TARGET_ARCH` env vars that gestaltd injects:

```sh
bun run gestalt build
```

The derivation reads the provider target from `package.json#gestalt.provider`,
the output path from `manifest.yaml#entrypoint.artifactPath`, the provider
name from the last segment of `manifest.yaml#source`, and the target platform
from env (falling back to the host platform). This lets a provider declare a
fixed `build.command: [bun, run, bun run gestalt build]` with no per-provider
wrapper script.

The build entrypoint compiles a standalone executable with Bun and bundles the
provider source into the result.

## API reference

The TypeDoc reference is generated from the authored public surface plus
entrypoint shims under `docs/entrypoints`.

```sh
bun run docs:build
```

## Local checks

From `sdk/typescript`:

```sh
export PATH="$HOME/.bun/bin:$PATH"
bun install
bun run build:proto
bun run check
bun run docs:check
```

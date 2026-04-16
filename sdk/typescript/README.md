# Gestalt TypeScript SDK

This package provides the TypeScript authoring surface for executable Gestalt
providers.

It is intended for source providers discovered through `package.json` and for
packaged providers built from that same source tree.

## Provider target

Point the package at the provider module with a top-level `gestalt.provider`
property in `package.json`:

```json
{
  "name": "my-provider",
  "version": "0.0.1-alpha.1",
  "dependencies": {
    "@valon-technologies/gestalt": "0.0.1-alpha.1"
  },
  "gestalt": {
    "provider": {
      "kind": "plugin",
      "target": "./provider.ts#plugin"
    }
  }
}
```

The target is a relative file path with an optional export suffix. The runtime
accepts:

- `gestalt.provider` as `{ "kind": "...", "target": "./file.ts#export" }`
- `gestalt.provider` as a string like `"plugin:./provider.ts#plugin"` or `"cache:./cache.ts#provider"`

Use `"plugin"` as the kind token for executable plugin providers.

If the export suffix is omitted, the runtime looks for `provider`, then
`plugin`, then the default export.

## Authoring

Use explicit runtime schemas to define plugin operation inputs and outputs:

```ts
import {
  definePlugin,
  ok,
  operation,
  response,
  s,
} from "@valon-technologies/gestalt";

export const plugin = definePlugin({
  displayName: "Example Provider",
  description: "A provider implemented with the Gestalt TypeScript SDK",
  configure(name, config) {
    console.log("configured", name, config);
  },
  sessionCatalog(request) {
    return {
      name: "example-session",
      operations: [
        {
          id: "session-ping",
          method: "GET",
        },
      ],
    };
  },
  operations: [
    operation({
      id: "greet",
      method: "GET",
      readOnly: true,
      input: s.object({
        name: s.string({ description: "Name to greet", default: "World" }),
        excited: s.optional(s.boolean()),
      }),
      output: s.object({
        message: s.string(),
      }),
      async handler(input) {
        return ok({
          message: `Hello, ${input.name}${input.excited ? "!" : "."}`,
        });
      },
    }),
  ],
});
```

Use `ok(body)` for normal responses and `response(status, body)` when a handler
needs to set a non-200 status. Plain objects with `status` and `body` fields
are treated as user data.

Auth providers, cache providers, and secrets providers use dedicated helpers:

```ts
import {
  Cache,
  defineAuthProvider,
  defineCacheProvider,
  defineSecretsProvider,
} from "@valon-technologies/gestalt";
```

## Runtime and build entrypoints

Source-mode runtime:

```sh
gestalt-ts-runtime ROOT plugin:./provider.ts#plugin
```

Release build:

```sh
gestalt-ts-build ROOT cache:./cache.ts#provider OUTPUT PROVIDER_NAME GOOS GOARCH
```

The build entrypoint compiles a standalone executable with Bun and bundles the
provider source into the result.

## Regenerating protobuf code

From the repo root:

```sh
buf generate --template sdk/proto/buf.typescript.gen.yaml sdk/proto
```

## Local checks

From `sdk/typescript`:

```sh
export PATH="$HOME/.bun/bin:$PATH"
bun install
bun run build:proto
bun run check
```

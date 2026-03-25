# @gestalt/plugin

TypeScript SDK for writing Gestalt provider and runtime plugins. Wraps the gRPC
plugin protocol with ergonomic async/await helpers so you can focus on your
integration logic.

## Installation

```bash
npm install @gestalt/plugin
```

## Provider plugin

A provider plugin exposes operations that Gestalt can invoke on behalf of users.

```typescript
import { serveProvider, ConnectionMode, type ProviderDefinition } from "@gestalt/plugin";

const definition: ProviderDefinition = {
  metadata: {
    name: "my-provider",
    displayName: "My Provider",
    description: "Does something useful",
    connectionMode: ConnectionMode.USER,
    authTypes: ["oauth"],
    connectionParams: {},
    staticCatalogJson: "",
    supportsSessionCatalog: false,
    supportsPostConnect: false,
  },
  operations: [
    {
      name: "get-items",
      description: "Fetch items from the API",
      method: "GET",
      parameters: [],
    },
  ],
  async execute(request) {
    // request.operation, request.params, request.token are available
    const items = await fetchItems(request.token);
    return { status: 200, body: JSON.stringify(items) };
  },
  auth: {
    async authorizationURL(request) {
      return `https://provider.com/authorize?state=${request.state}`;
    },
    async exchangeCode(request) {
      const tokens = await exchangeOAuthCode(request.code);
      return {
        accessToken: tokens.access_token,
        refreshToken: tokens.refresh_token,
        expiresIn: tokens.expires_in,
        tokenType: "Bearer",
      };
    },
    async refreshToken(request) {
      const tokens = await refreshOAuthToken(request.refreshToken);
      return {
        accessToken: tokens.access_token,
        refreshToken: tokens.refresh_token,
        expiresIn: tokens.expires_in,
        tokenType: "Bearer",
      };
    },
  },
};

serveProvider(definition);
```

## Runtime plugin

A runtime plugin implements lifecycle hooks and can invoke other providers
through the host.

```typescript
import {
  serveRuntime,
  type RuntimeDefinition,
  type RuntimeHostClient,
} from "@gestalt/plugin";

let host: RuntimeHostClient;

const definition: RuntimeDefinition = {
  async start(request, hostClient) {
    host = hostClient;
    const caps = await host.listCapabilities();
    console.log(`Started with ${caps.capabilities.length} capabilities`);
  },
  async stop() {
    console.log("Stopping runtime");
  },
};

serveRuntime(definition);
```

## How it works

The SDK reads the `GESTALT_PLUGIN_SOCKET` environment variable (set by the
Gestalt host) and starts a gRPC server on that Unix socket. For runtime plugins,
`GESTALT_RUNTIME_HOST_SOCKET` provides a back-channel to invoke operations on
other providers.

The plugin process is managed by Gestalt and receives `SIGTERM` for graceful
shutdown.

## Protocol reference

The gRPC service definitions are in [`v1/plugin.proto`](../../pluginapi/v1/plugin.proto).

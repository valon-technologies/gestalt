# Gestalt

Gestalt is a platform for self-hostable, configurable integrations and tooling, with authentication and execution out-of-the-box.

## How does Gestalt work?

Gestalt works in three stages:

### Configure

Define the integrations you need declaratively: which providers to enable, how users authenticate, and where secrets and tokens are stored.

### Connect

Gestalt handles OAuth flows, token storage, and automatic refresh for every configured provider. Users authenticate once, and Gestalt manages their credentials from that point forward.

### Invoke

Once connected, every provider is available through a unified gateway. The same authorization rules apply regardless of how a provider is reached.

## Why Gestalt?

### Self-hosted and private

Gestalt runs in your infrastructure. Credentials and data never leave your network.

### One config, many integrations

A single YAML file replaces per-integration glue code, token management scripts, and bespoke OAuth callback servers. Cloud agents, local coding assistants, and other harnesses all share the same gateway and authorization platform, so you only configure your integrations once.

### Works with AI tooling

Gestalt exposes every configured integration via an optionally enabled, self-hosted MCP server, so AI agents and coding assistants can use your integrations directly. Gestalt's CLI ships with progressive disclosure, so non-technical users can work with integrations directly and effectively.

### Extensible

Write your own plugins or point Gestalt at any OpenAPI spec, MCP server, or GraphQL endpoint to add a new provider in minutes.

## Project layout

| Path | Description |
| --- | --- |
| [`gestaltd`](./gestaltd) | The Go server daemon. Loads config, serves the HTTP API, MCP surface, and embedded web UI. |
| [`gestalt`](./gestalt) | The Rust CLI client. Connects to a running `gestaltd` instance for authentication and operations. |
| [`sdk`](./sdk) | Shared SDKs and plugin manifest definitions. |
| [`docs`](./docs) | The documentation site. |

## Getting started

See the [Getting Started](https://docs.valon.tools/getting-started) guide.

## Documentation

[docs.valon.tools](https://docs.valon.tools)

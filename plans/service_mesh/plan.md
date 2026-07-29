# Service Mesh Design

## Overview

The platform builds, distributes, installs, and runs services in a shared service mesh. A service package may include:

- An MCP server and its operations
- User interface (single-page app or SPA's)
- Workflows
- Dependency and migration metadata
- Runtime and authorization requirements

Organizations operate a deployment backed by one private registry and, optionally, additional public registries. The deployment exposes a catalog of installed services, operations, interfaces, workflows, and dependencies.

## Architecture

The runtime consists of:

- **Control plane (`istiod`)**: manages mesh configuration and service routing.
- **Ingress data plane**: receives traffic from the load balancer.
- **Service data plane (`gestaltd`)**: runs alongside each service and mediates mesh traffic.
- **Service runtime**: runs the service's server, interfaces, and workflows.

Terraform deploys the GCP infrastructure, including the Valon Tools cluster, GitHub Actions cluster, networking, container infrastructure, and identity services.

## Service Packages

When first published, a registry assigns the service a UUID unique within that registry and associates it with a human-readable slug. Service references include both the registry and UUID. Each package version consists of a built OCI image and a service manifest for a specific platform. The service manifest contains:

- The immutable OCI image reference and digest
- Instructions for starting the MCP server and user interfaces
- The MCP endpoint, operations, and input/output schemas
- Included workflows and interfaces
- Service and operation dependencies
- Ordered pre-upgrade and rollback commands
- Service-account and authorization requirements
- Health and readiness endpoints
- Configuration and secret requirements
- Image signature and build provenance

Upgrade commands may perform schema migrations, workflow changes, or other dependency updates. They should be idempotent and reversible where possible.

## Registries

Registries store versioned service packages and control who can discover or download them. A deployment may use private, organization-specific, and public registries, provided each implements the registry interface.

Each registry exposes a catalog manifest listing its services. Each entry includes the registry-scoped service UUID, slug, available versions and platforms, service manifest location, and immutable OCI image reference and digest. The OCI image is the deployable artifact; a Dockerfile may be retained as optional source provenance but is not part of the runtime contract.

A registry can be connected to a GitHub repository and configured to build from `main`, pull requests, or label-based rules. Service developers can also build and push packages directly.

## Build and Publish

1. `vt build <dir>` builds a service package from a source directory and its manifest.
2. The build produces a platform-specific OCI image with an immutable digest.
3. The package is published through the configured GitHub integration or with `--push <registry-url>`.
4. The registry publishes the catalog and service manifests and points them to the built OCI image.

`vt dev <url> <dir> <token>` injects a local service into an existing mesh for development. A development service runs with the developer's identity.

## Installation

Publishing a service does not install it. On first installation, an organization administrator selects a version and configures:

- The identity used by workflows
- The identity used by service operations
- Whether an operation forwards the caller's identity
- The service's authorized operations

A service identity cannot initially exceed the installer’s authorization. Its relationships are assigned using the authorization model below.

Installing or removing a service updates the runtime catalog without redeploying the service mesh.

## Upgrades

Before an upgrade, the platform verifies that required operations exist, dependencies are ready and not being upgraded, and the package defines valid pre-upgrade and rollback commands.

The upgrade sequence is:

1. Pull the new OCI image.
2. Run the service's pre-upgrade commands in order.
3. Start the new service version.
4. Validate startup and the `/ready` endpoint.
5. Shift traffic to the new version.

The old version continues serving until the traffic shift. If an upgrade step fails, the platform stops the upgrade and records the failed command and error. An authorized administrator then resolves the failure and manually runs the declared rollback commands.

After traffic shifts, traffic may return to the old version only when the package declares that rollback safe. Automatic rollback can be added later using the same commands and safety declaration.

Workflows should continue running during an upgrade unless they call the service being upgraded. Service operations and interfaces may be unavailable during the traffic transition. The target is less than ten seconds of downtime.

## Administration and Discovery

An organization deployment provides:

- A service catalog
- An operation catalog
- A user-interface catalog
- A dependency graph
- Service details, versions, workflows, identities, and authorization settings
- API-key management
- A development sandbox

Organization administrators can connect registries and install, upgrade, or remove services. Service developers can build, publish, test, and invoke services. Service users see only the catalogs and tools available to them.

Representative endpoints:

- Deployment: `https://vt.valon.tools`
- Private registry: `https://vt.valon.tools/registry`
- Administration: `https://vt.valon.tools/admin`
- Service administration: `https://vt.valon.tools/services/<service>/admin`

## Invocation

Authorized operations are available over the deployment endpoint and through the CLI:

```sh
vt invoke <url> <service> <operation> <args> <token>
```

Operation invocation uses MCP Streamable HTTP. Clients and services must support JSON-RPC over POST and the protocol's JSON and SSE response forms.

## Authentication and Authorization

- Users authenticate through an external identity provider, with organization-level SSO support.
- Users can create API keys that act as their identity.
- Users can create service accounts with a subset of their permissions.
- Every installed service has an assigned identity.
- Every operation can enforce authorization.

Authorization uses a Zanzibar-style graph of relationship tuples. Each relationship definition declares whether it supports delegated assignments, persistent grants, both, or neither. The default is neither.

- **Delegated assignment (`delegatable`)**: a relationship holder can create a derived assignment that cannot exceed or outlive its parent assignment.
- **Persistent grant (`grantable`)**: a principal with the relationship-specific grant permission can create an independent assignment that remains until explicitly revoked.

Assignments record their issuer, assignment mode, creation time, optional expiration, and optional parent assignment. Revoking a parent invalidates its delegated descendants, while an independent assignment through another path preserves access. Persistent grants require an explicit Zanzibar permission such as `can_grant_<relationship>`.

## Mesh Deployment

The mesh is redeployed only when changing mesh infrastructure, including:

- The `istiod` snapshot
- The `gestaltd` snapshot
- Ingress
- Egress

Installing, upgrading, or removing services and adding or removing registries are runtime changes and do not require a mesh redeployment.

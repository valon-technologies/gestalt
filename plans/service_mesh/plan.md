# Service Mesh Design

## Contents

- [Overview](#overview)
- [Registries](#registries)
- [Publishing and Distribution](#publishing-and-distribution)
- [Package Revisions](#package-revisions)
- [Runtime and Access](#runtime-and-access)
- [Platform Architecture](#platform-architecture)
- [Representative Request Lifecycles](#representative-request-lifecycles)
- [Glossary](#glossary)

## Overview

The platform builds, distributes, installs, and runs packages in a shared mesh. A release may include:

- An MCP server and its operations
- One or more single-page user interfaces
- Durable workflow definitions
- Dependency and migration metadata
- Per-operation authorization relationship or role requirements

### Organization Administrator

An organization administrator:

- Operates a deployment with one private registry and administration surfaces such as `https://vt.valon.tools`, `/registry`, and `/admin`.
- Imports trusted releases by canonical package ID and version, supplying upstream read credentials per import.
- Browses installed apps as a graph of operations, UIs, workflows, dependencies, versions, identities, and authorization.
- Installs, updates, and removes apps.

### Package Publisher

A publisher:

- Defines package metadata, operations, dependencies, workflows, UIs, and migrations in `package.yaml`.
- Runs `vt build <dir>` to validate and build locally, with `--push` to publish.
- Runs `vt dev --deployment <url> <dir>` in an isolated user sandbox.

### Package Administrator

A package administrator:

- Inspects app deployment details.
- Manages desired release versions within package-scoped authority.
- Promotes releases manually or through deployment rules.
- Performs the first installation.
- Selects organization-managed identities for runtimes, migrations, and workflows.
- Chooses workload-only or delegated caller authority per operation.
- Binds permissions as durable grants or delegated authority.

A durable grant cannot exceed its human issuer's authority and lasts until revocation, expiration, or invalidation by policy migration. Delegated authority tracks the subject's current authority.

Publishing and package administration are independent roles. Publishing, installation, and promotion are separate actions.

### Package User

A package user:

- Browses authorized apps, operations, UIs, and workflows.
- Launches app UIs and invokes operations through MCP Streamable HTTP or `vt invoke`.
- Manages API grants and authorized development sandboxes.
- Has no organization or app administration access.

## Registries

The registry protocol is open and has no central authority.

### Deployment Registry

Each deployment has one private registry as its sole catalog and artifact source. Installation, upgrade, rollback, and recovery never fetch externally.

Releases enter through:

- **Push**: an authorized publisher uploads a release. Credentials are scoped to permitted package and release actions.
- **Pull-through import**: an administrator supplies a canonical package ID, version, and upstream read credentials. The registry resolves, verifies, and stores the complete release before exposure.

### Build

`vt build <dir>` creates a release from a source directory:

```text
g-issues/
├── package.yaml
├── runtime/
│   ├── package.json
│   ├── bun.lock
│   └── src/
├── migrations/
│   ├── v2.3.0/
│   └── v2.3.1/
└── ui/
    └── admin-console/
```

`package.yaml` defines package metadata, operations, dependencies, workflows, UIs, migrations, and authorization. It references workflow definitions and operation schemas; no fixed directories are required. Runtime and migration code must be Bun applications; packages cannot supply images or Dockerfiles. `vt build` validates the package, installs from the frozen lockfile, builds digest-addressed artifacts, and writes the release bundle.

### Example Release Bundle

```text
g-issues@2.3.1
├── release_manifest.json
├── runtime
│   └── bun-app.tar.gz
├── migrations
│   ├── v2.3.0-bun-app.tar.gz
│   └── v2.3.1-bun-app.tar.gz
├── workflows
│   └── definitions.json
├── schemas
│   └── operations.json
└── ui
    └── admin-console.tar.gz
```

UI-only or workflow-only packages omit the runtime.

```sh
vt build <dir>
vt build <dir> --push
```

Without `--push`, the build is local and needs no deployment credentials. `--push` uploads the completed bundle.

### CLI Authentication

For `--push`, the CLI resolves credentials in this order:

1. `VT_API_KEY` environment variable
2. A session stored in the OS keychain from a prior `vt auth login`
3. Interactive `vt auth login` if no credential is available

`--push` is idempotent and resumes interrupted uploads.

### Stored Content and Backing Services

Cloud Storage holds immutable release bundles and detached signatures by SHA-256 digest. Generation-match preconditions prevent overwrite; retention and versioning support recovery.

The registry service tracks catalog and control-plane metadata separately from Cloud Storage:

- Canonical package IDs, slugs, version indexes, and tombstones
- Publication and import attempts, including pending and failed state
- Catalog indexes that map `(package, version)` to `releaseDigest`
- Import history, including source registry identity, release provenance, and whether the copy completed

### Identity and Trust

Each registry controls discovery, download, and publication. Pull-through imports use per-operation read-only credentials.

Each registry has a stable `registryId` derived from its root public key.

### Pull-through Imports

The canonical package ID identifies the source registry, so no standing upstream configuration is required. The registry uses per-import credentials, verifies the complete release, then atomically exposes it. Failed imports remain hidden. Installation separately enforces organization trust policy. Imported releases retain source identity, signatures, and digest.

## Publishing and Distribution

### Package Releases

`release_manifest.json` must declare:

- Canonical registry and package IDs, slug, and `releaseVersion`
- Digest references for every artifact produced by `vt build`
- MCP operations with input and output schema digests
- Workflow definition digests
- Required and optional package and operation dependencies
- Ordered migration jobs
- Required authorization relationships or roles for each operation
- Supported MCP protocol versions
- Required Bun version, entry point, and probe contract for releases that include a runtime
- Optional source metadata (e.g. PR number)

#### Publication

1. `vt build` produces the release artifacts; each artifact gets a SHA-256 digest.
2. `vt build` writes `release_manifest.json` listing those digests and the release metadata above.
3. `vt build` computes `releaseDigest` as the SHA-256 hash of the canonical manifest bytes. It is not embedded in the manifest.
4. The publisher signs `registryId`, `packageId`, `releaseVersion`, `releaseDigest`, and every referenced artifact digest. The signature is stored separately from the manifest.
5. `--push` uploads the artifacts, manifest, and signature to the deployment registry.

#### Verification

On push, the registry authenticates the publisher, verifies signatures, digests, and contracts, and rejects conflicting `releaseVersion` republications. `releaseVersion` follows a documented grammar and permanently binds to one `releaseDigest`. Exposure follows successful verification.

Import validates before copying. Installation re-verifies the selected `releaseDigest`.

### First Installation

First installation admits a cataloged release:

1. **Select a release.** A package administrator chooses the app and an immutable `releaseDigest` from the registry catalog.
2. **Review the contract.** The platform shows declared operations, dependencies, workflows, UIs, migrations, and required authorization relationships.
3. **Bind identities.** The administrator selects organization-managed service accounts for the runtime, migration jobs, and workflows when the release includes them.
4. **Configure operation authority.** For each operation, the administrator decides whether it runs only as the workload identity or may exercise delegated caller authority, and binds permissions as durable grants or delegated authority.
5. **Approve admission.** The administrator confirms the bindings. This requires `service_account.assign` for each identity and `authorization.grant` for each relationship or role being granted.
6. **Verify and prepare.** The control plane re-verifies the `releaseDigest`, resolves dependencies, reserves the package slot, binds identities, provisions waypoint routes and mesh policies, and then runs prepare migrations.
7. **Roll out.** The revision controller creates runtime workloads when present, stages UI and workflow artifacts, and promotes after readiness converges.

## Package Revisions

### Durable Deployment State

The control plane persists:

- **Package slots**: organization-scoped slots keyed by canonical package identity, reserved before installation ID allocation to fence concurrent first installs
- **Revision requests**: append-only records of install, upgrade, rollback, and removal intent, including the actor, previous promoted revision, target release or tombstone, deployment-registry catalog generation and applicable trust-policy revision, approved authorization bindings, resolved dependencies, and timestamps
- **Installation head**: last successfully promoted revision and current admitted candidate
- **Revision operation**: one generation-fenced state machine per installation with phase, candidate resources, deadlines, stability timestamp, and routing generation
- **Migration outcomes**: immutable status keyed by installation, `releaseDigest`, and migration ID
- **Routing and catalog generations**: independent operation-routing, package, operation, UI, and workflow generations committed for the promoted revision
- **Terminal outcomes**: completion or failure phase, duration, and reason

### Installation Reconciliation

An installer selects an immutable `releaseDigest` and supplies:

- An organization-managed Kubernetes service account when the release has a runtime or migration
- Organization-managed workflow identities
- Package-declared authorization relationships or roles for each operation
- Caller-delegation policy per operation
- Initial replica, resource, and availability settings when the release has a runtime

Installation uses a reconciled saga:

1. Verify registry completeness, source identity, trust policy, signatures, digests, platform support, runtime compatibility, and schema versions.
2. Resolve exact dependency revisions and operation-contract digests.
3. Reserve the organization package slot and persist the revision request, approved authorization snapshot, and fenced revision operation.
4. Bind identities, authorization, ambient and waypoint enrollment, L4 bypass policy, and waypoint `AuthorizationPolicy` attachments.
5. Hand the fenced operation to the revision controller, which follows the canonical phases in [Revision Rollouts](#revision-rollouts).

Failures preserve phase and diagnostics. The controller retries within policy and cleans up unpromoted resources after cancellation.

### Dependencies and Contracts

Dependencies specify package identity, version range, requirement level, and operation-contract digests. Admission records resolved revisions and digests.

Admission validates:

- Every required dependency has a promoted compatible revision
- Required operations exist
- Required input and output contracts match
- The candidate remains compatible with existing reverse dependents
- No conflicting revision operation is active in the affected dependency subgraph
- Workflow calls reference compatible operation contracts

Schemas share one JSON Schema version and are normalized before hashing. Initially, compatibility requires equal hashes.

### Revision Rollouts

Runtime upgrades use blue/green rollouts. Each revision has an immutable Deployment and Service; serving Deployments are never mutated. Promotion atomically advances the promoted head and catalog generations after routing, UI, and workflow checks pass for a stability window. Pre-commit failure restores routing without advancing generations. Checks cover workload health, Gateway API route status, waypoint configuration distribution, and ambient enrollment.

Phases:

1. **Admitted**: validate release, policy, compatibility, and dependencies.
2. **Preparing**: run fenced, backward-compatible prepare migrations.
3. **Starting**: create runtime resources and stage candidate UI and workflow artifacts.
4. **Ready**: require every applicable check to pass for the stability window.
5. **Canarying**: send limited traffic only for contracts identical to the promoted revision, without catalog exposure.
6. **Promoting**: execute the convergence and atomic commit protocol above.
7. **Draining**: enforce the pinned-session and hard-deadline protocol below.
8. **Observing**: monitor production traffic for the configured period.
9. **Finalizing**: run separately approved irreversible migrations.
10. **Complete**: record convergence and timing.

Existing MCP SSE connections remain pinned until closure or deadline. Stateful follow-ups carry a signed session token and revision header; `ext_authz` validates the token before the waypoint routes by header. New sessions follow controller-owned weights. Pre-promotion failure retains the prior revision. Post-promotion failure marks the new revision degraded and requires retry, approved rollback, or forward fix.

### Migrations and Rollback

Each digest-pinned migration declares:

- Migration ID and `releaseDigest`
- Phase—the revision lifecycle stage in which the job runs
  - **Prepare** — backward-compatible changes before promotion.
  - **Finalize** — potentially irreversible changes after promotion; destructive jobs require separate approval.
  - **Compensating** — data reversal or repair during approved rollback.
- Dedicated workload identity
- Timeout, resource limits, retry policy, and idempotency key
- Compatibility with the prior and candidate package revisions

Expand changes remain compatible with the serving revision. Contract changes remove obsolete schema only after promotion.

## Runtime and Access

### Authentication and Authorization

The authorization service is the system of record for organization policy, stored as Zanzibar-style tuples for management, user, workflow, and package calls.

- Users authenticate through an external identity provider with organization-level SSO, issuer and tenant allowlists, and session revocation.
- API keys are credentials for named, expiring, scoped grants that act for an owner. Only hashes are stored; plaintext is returned once. Empty scope grants nothing.
- `service_account.create` permits creating an organization-managed service account but grants it no authority.
- `service_account.assign` permits binding a service account to a package runtime, workflow definition, or workflow execution but grants it no new authority.
- `authorization.grant` is human-only and cannot exceed the issuer's authority, resource scope, conditions, or expiration.
- Every installed package runtime uses an organization-owned workload identity. Workflow definitions are bound to organization-owned workflow identities that the workflow service uses for execution.
- Every invocation is centrally authorized before dispatch; package-specific resource checks may further restrict it.
- Production fails closed when identity or authorization is unavailable.

### Authorization Model and Evaluation

Authorization checks include:

- A canonical **subject** being evaluated, such as the effective subject, immediate caller workload, or management actor
- An **action**, such as a package operation or administrative permission
- A canonical **resource**, identified by a reserved platform type or a package-namespaced type

The platform reserves registry, package, release, installation, revision, workflow, identity, and authorization-grant types. Packages register namespaced types at admission.

### Package Access Policy

Release manifests declare required relationships or roles per operation. MCP endpoints are mesh-internal. Every invocation passes through the destination waypoint. Ambient L4 policy and `NetworkPolicy` block waypoint bypass; the Bun wrapper rejects requests without signed internal caller context. External calls use deployment ingress and the same authorization service. Deployment policy controls default-deny egress.

### Workflows

The controller registers immutable workflow definitions. Executions remain pinned to definition and contract digests until completion, migration, or retirement; new executions use the promoted catalog generation.

### User Interfaces

UI staging, serving, authorization, version pinning, and rollback behavior remain to be specified.

### Catalogs and Administration

Representative endpoints:

- Deployment: `https://vt.valon.tools`
- Private registry: `https://vt.valon.tools/registry`
- Administration: `https://vt.valon.tools/admin`
- Package administration: `https://vt.valon.tools/packages/<package>/admin`

Management APIs require dedicated administrative permissions and a private listener or equivalent network controls.

### External Connections

Users connect a package to an external service through the CLI:

```sh
vt connect <package>
```

The CLI opens browser login, then stores granted credentials in the secret-management service.

TODO: Define provider discovery, requested scopes and consent, callback handling, credential storage and rotation, revocation, and package access to connected credentials.

### Invocation

Authorized operations are available over the deployment endpoint and through the CLI:

```sh
vt invoke --deployment <url> <package> <operation> --args <json>
```

Authentication comes from a session, keychain, environment, or standard input. Positional arguments never carry secrets.

MCP Streamable HTTP requests carry reserved installation and operation headers; stateful requests also carry a revision header and signed session token. Ingress authenticates external callers, strips caller-supplied internal-context headers, validates routing headers, and issues a short-lived caller token bound to that metadata. Workloads obtain equivalent tokens internally.

The waypoint calls `ext_authz`, then routes by header to a revision Service. Authorization validates tokens, operation access, and quota, then returns signed internal caller context. Invalid requests never reach the runtime.

The Bun wrapper verifies context, operation/body agreement, and schema before dispatch. Ingress enforces external limits; waypoint policy controls transport; the wrapper enforces payload and argument-dependent authorization.

### Development Sandbox

`vt dev` connects a local build to an enabled per-user sandbox:

```sh
vt dev --deployment <url> <dir>
```

The platform assigns a short-lived development workload identity. Routes are scoped to the developer or test session and cannot claim production slugs.

Development packages:

- Receive traffic only from their developer or named test sessions and never become the production default
- Cannot run migrations
- Cannot receive or forward raw caller credentials
- Do not inherit the production workload's permissions
- Use sandbox-scoped authorization
- Expire automatically and are audited upon connection and disconnection

Production runs only on Kubernetes with self-managed Istio ambient. Pinned Helm releases install base CRDs, `istiod`, ambient CNI, and `ztunnel`; managed Google Cloud Service Mesh lacks the required data plane. There is no laptop or VM production profile. Development sandboxes use the connected deployment.

## Platform Architecture

### Deployment Tiers

Deployment has three trust tiers:

1. **Bootstrap substrate**: Kubernetes, Istio ambient, artifact storage, state store, revision controllers, certificate authority, and recovery tooling. Terraform provisions these before package APIs.
2. **Protected system packages**: ingress authentication, identity, authorization, secrets, registry verification, audit, and workflows. After bootstrap, controllers deploy signed releases through normal revision machinery.
3. **Organization packages**: administrator-installed packages constrained by organization policy.

Bootstrap and protected services are inside the platform availability boundary. Their failure is platform downtime for affected capabilities. Organization package failure affects only that package unless caused or spread by the platform.

### Trusted Platform Services

- **Identity service**: authenticates sessions and API keys, resolves principals, manages service accounts, and issues signed caller and session tokens.
- **Authorization service**: stores immutable policy models, grants, assignments, and revocations; evaluates provider-independent and Envoy `ext_authz` checks; and signs the internal caller contexts forwarded to package runtimes.
- **Secret-management service**: stores and rotates credentials and releases them only to authorized workload identities.
- **Registry-verification service**: verifies manifests, digests, signatures, compatibility, trust policy, and artifact completeness before exposure and admission.
- **Audit sink**: writes append-only security, authorization, administration, and runtime events to storage that packages cannot control.
- **Workflow service**: registers promoted workflows and runs pinned executions outside package runtimes.
- **Control-plane state store**: stores registry state, import provenance, package slots, revision state, generations, migrations, and recovery metadata.

### Runtime Architecture

Production uses self-managed Istio ambient with shared destination waypoints and no Pod sidecars. Runtimes and migrations use a platform-owned, unprivileged Bun image. The runtime wrapper owns MCP and health listeners, verifies caller context and operation/body agreement, validates schemas, dispatches package code, reports health, emits telemetry, and drains. Migrations use a non-serving entry point.

The platform runtime includes `istiod`, node-level `ztunnel`, ingress, destination waypoints, Bun containers, protected services, state store, and revision controllers. Controllers reconcile durable state into workloads, Services, enrollment, routes, and policies. `istiod` distributes endpoints, routes, identities, certificates, and policies but does not own revisions, grants, workflows, or catalogs.

**`ztunnel` configures (L4):**

- mTLS and identity-based L4 policy, including direct Pod and revision Service bypass prevention
- Workload enrollment and readiness gating on policy generation
- L4 telemetry and audit for connection, byte, policy-denial, and bypass events

`ztunnel` cannot enforce L7 policy, select revision backends, or run `ext_authz` authorization checks.

**Destination waypoints configure and enforce L7 behavior:**

- Gateway API routing by installation, operation, and optional session revision to revision-specific Services
- Provider-independent operation authorization through `CUSTOM` `AuthorizationPolicy` and `ext_authz`
- Weighted routing and signed session-route validation
- L7 telemetry and transport policy
- Rate or quota enforcement through `ext_authz`

Waypoints do not decode MCP or use custom Envoy, Wasm, or `TrafficExtension` filters. They trust metadata approved by `ext_authz`; the Bun wrapper enforces metadata/body agreement.

Ingress authenticates and mints caller tokens; waypoints authorize and route; the wrapper verifies internal context and body agreement. Health probes use a protected, non-public port. `NetworkPolicy` supplements ambient policy. Authorization and required security-event delivery or buffering fail closed. Without `istiod`, existing traffic uses cached configuration while configuration-dependent revision operations stop.

### Infrastructure and Service Rollouts

Mesh redeployment is limited to:

- `istiod`, ambient CNI, and `ztunnel` versions
- Destination waypoint and optional Istio ingress or egress gateway versions
- Istio certificate-authority and trust-domain configuration
- Mesh-wide ambient-enrollment and L4 policy configuration

Protected and organization packages roll independently. Package revisions, registry trust, identity, and authorization changes do not redeploy the mesh.

Bootstrap changes use infrastructure rollouts and redeploy the mesh only when changing an item above. Releases declare supported MCP and Bun versions. Infrastructure pins Kubernetes, Istio, Envoy, the Bun image, routing contract, token formats, and routing schema, with version overlap before admitting dependent releases. Registry trust-root changes require separate authorization and audit.

## Representative Request Lifecycles

To specify:

- Provision the deployment registry
- Import a package release through pull-through
- Publish a package release
- Upgrade a package
- Invoke a package operation
- Invoke a package operation (package-to-package)
- Authentication
- Connect external credentials

## Glossary

| Term | Meaning |
| --- | --- |
| App | User-facing term for an installed package |
| API grant | Named, expiring set of permissions; an API key is the credential used to exercise it |
| Canonical package ID | Global package identifier: the concatenation of `registryId` and registry-local `packageId`, written as `registryId/packageId`; never reused |
| Deployment registry | A deployment's private registry and sole package catalog and artifact source |
| Package identity | Equivalent to canonical package ID; also representable as `(registryId, packageId)` |
| Pull-through import | Verified, atomic copy of an external release and its complete artifact closure into the deployment registry, identified by canonical package ID and release version, using upstream read credentials supplied at import time |
| `releaseVersion` | Publisher-assigned version that permanently resolves to one immutable `releaseDigest` within a package |
| `releaseDigest` | SHA-256 hash of the canonical manifest bytes; the authoritative installation input |
| Revision | Installed release or removal tombstone |
| Revision operation | Fenced execution of durable revision intent |
| Promotion | Atomic commit advancing the promoted head and catalog generations |
| Routing generation | Durable correlation of waypoint routing state, selected revision Services, route and proxy convergence, and observations |
| Service account | Organization-managed non-human principal that may be assigned to runtimes, migrations, or workflows |
| Workload identity | Runtime identity materialized from an assigned service account and enforced by Kubernetes and the mesh |
| Workflow identity | Identity bound to a workflow definition and used by the workflow service for executions |
| Caller token | Signed pre-authorization context presented only to an authorized destination waypoint |
| Internal caller context | Signed post-authorization context forwarded to a runtime |
| Bun runtime wrapper | Platform-owned runtime layer that verifies internal caller context, validates schemas, and dispatches publisher package code |
| Protected system package | Signer-pinned platform package unavailable to ordinary package APIs |
| Waypoint | Shared ambient Envoy proxy that authorizes declared invocation metadata and routes requests to revision-specific Services using standard Gateway API configuration |
| `ztunnel` | Node-level ambient proxy for L4 security and workload identity |

# Service Mesh Design

## Contents

- [Overview](#overview)
- [Registries](#registries)
- [Foundations](#foundations)
- [Publishing and Distribution](#publishing-and-distribution)
- [Package Revisions](#package-revisions)
- [Runtime and Access](#runtime-and-access)
- [Platform Architecture](#platform-architecture)
- [Representative Request Lifecycles](#representative-request-lifecycles)
- [Assurance](#assurance)
- [Glossary](#glossary)
- [Appendix: Design Invariants](#appendix-design-invariants)

## Overview

The platform builds, distributes, installs, and runs packages in a shared service mesh. A package release may include:

- An MCP server and its operations
- One or more single-page user interfaces
- Durable workflow definitions
- Dependency and migration metadata
- Per-operation authorization relationship or role requirements

The platform should enable the following user experiences.

### Organization Administrator

An organization administrator can:

- Operate one deployment, private registry, and administration surface, such as `https://vt.valon.tools`, `/registry`, and `/admin`.
- Pull trusted releases into the private registry by canonical package ID and release version, supplying upstream read credentials at import time.
- Browse installed apps as a graph, including operations, UIs, workflows, dependencies, versions, identities, and authorization.
- Open an app administration page, change its version, or install and remove it.

### Package Publisher

A publisher can:

- Define package metadata, operations, dependencies, workflows, UIs, and migration jobs in a source manifest.
- Run `vt build <dir>` to validate the package and build a release bundle locally, or add `--push` to publish it to the deployment registry.
- Run `vt dev --deployment <url> <dir>` to test a package in an isolated, user-scoped mesh sandbox before publishing it.

### Package Administrator

A package administrator can:

- Inspect an app and its deployment details.
- Manage its desired version within their package-scoped authority.
- Promote releases from the private registry manually or through deployment rules.
- Perform the first installation that admits a package into the mesh.
- Select organization-managed identities for runtimes, migrations, and workflows.
- Decide whether an operation runs only as its workload identity or may exercise explicitly delegated caller authority.
- Bind each permission as either a durable grant or delegated authority.

A durable grant cannot exceed the human issuer's authority when created and remains until explicitly revoked, expired, or invalidated by a policy migration. Delegated authority is bounded by the subject's current authority and narrows immediately when that authority is lost.

Package administration and publishing are independent roles, although the same person may hold both. Publishing, installation, and promotion remain separate actions.

### Package User

A package user can:

- Open a deployment and browse only the apps, operations, UIs, and workflows they are authorized to see.
- Launch an app UI or invoke an operation through MCP Streamable HTTP or `vt invoke`.
- Manage their API grants and use a development sandbox when authorized.
- Use these capabilities without access to organization or app administration surfaces.

## Registries

The registry protocol is open. Anyone may operate a compatible registry; there is no central creation or approval authority.

### Deployment Registry

Each deployment has exactly one registry and uses it as the sole package catalog and artifact source. Installation, upgrade, rollback, and recovery never fetch directly from an external registry.

Releases enter through:

- **Push**: an authorized publisher uploads a release created for the deployment registry. Publication credentials grant only the permitted package and release actions.
- **Pull-through import**: an administrator specifies a canonical package ID, release version, and upstream read credentials at import time. The registry resolves the source registry from the package ID, verifies the release, and stores the complete release locally before exposing it.

### Build

Publishers create releases from a source directory with `vt build <dir>`. For example:

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

`package.yaml` defines the package name, version, operations, dependencies, workflows, UIs, migration jobs, and authorization requirements. Runtime and migration code must be Bun applications; packages do not supply Dockerfiles or custom container images. Workflow definitions and operation schemas live in the migration jobs rather than separate source directories. `vt build g-issues/` validates the package, installs dependencies from the frozen Bun lockfile, builds digest-addressed Bun application artifacts locally, and writes a logical release bundle.

### Example Release Bundle

A typical bundle is not one container or folder. For example:

```text
g-issues@2.3.1
├── release_manifest.json
├── runtime
│   └── bun-app.tar.gz
├── migrations
│   ├── v2.3.0-bun-app.tar.gz
│   └── v2.3.1-bun-app.tar.gz
└── ui
    └── admin-console.tar.gz
```

A UI-only or workflow-only package omits the runtime Bun app.

```sh
vt build <dir>
vt build <dir> --push
```

Without `--push`, `vt build` writes the release bundle locally and needs no deployment credentials. With `--push`, it uploads to the deployment registry after building.

#### CLI authentication

`vt build <dir> --push` authenticates to the deployment registry before upload. The CLI resolves credentials in this order:

1. `VT_API_KEY` environment variable
2. A session stored in the OS keychain from a prior `vt auth login`
3. Interactive `vt auth login` if no credential is available

`--push` is safe to run idempotently: retrying the same release after interruption resumes or completes the existing attempt instead of creating a duplicate catalog entry.

### Stored Content and Backing Services

Google Artifact Registry stores the immutable release bundle: runtime and migration Bun app artifacts, UI bundles, workflow definitions, schemas, the canonical release manifest, and detached signatures. Bun app artifacts may use OCI artifact storage, but they are not publisher-supplied container images. Each artifact is content-addressed by SHA-256 digest.

The registry service also tracks metadata that Artifact Registry does not provide on its own:

- Canonical package IDs, slugs, version indexes, and tombstones
- Publication and import attempts, including pending and failed state
- Catalog indexes that map `(package, version)` to `releaseDigest`
- Import history, including source registry identity, release provenance, and whether the copy completed

### Identity and Trust

Every registry is responsible for permissioning who can pull from it and who can publish to it.

The deployment registry controls read access for discovery and download, and write access for authorized publishers. Upstream registries do the same on their side; pull-through import uses read-only credentials supplied for that import operation.

Each registry has a stable `registryId` derived from its root public key.

### Pull-through Imports

An administrator specifies a canonical package ID, release version, and upstream read credentials at import time. The canonical package ID encodes the source registry; no standing upstream source configuration is required. The import grants read access only for that operation; installation still requires organization trust in the release signature. The registry verifies the complete release, then atomically adds it to the local catalog; failed imports remain invisible. Imported releases keep their source identity, signatures, and digest.

## Publishing and Distribution

### Package Releases

`release_manifest.json` must declare:

- Canonical registry and package IDs, slug, and `releaseVersion`
- Digest references for every artifact produced by `vt build`
- MCP operations with input and output schema digests
- Required and optional package and operation dependencies
- Ordered migration jobs
- Required authorization relationships or roles for each operation
- Supported MCP protocol versions
- Bun version, entry point, startup instructions, and probe contract for releases that include a runtime
- Optional source metadata (e.g. PR number)

#### Publication

1. `vt build` produces the release artifacts; each artifact gets a SHA-256 digest.
2. `vt build` writes `release_manifest.json` listing those digests and the release metadata above.
3. `vt build` computes `releaseDigest` as the SHA-256 hash of the canonical manifest bytes. It is not embedded in the manifest.
4. The publisher signs `registryId`, `packageId`, `releaseVersion`, `releaseDigest`, and every referenced artifact digest. The signature is stored separately from the manifest.
5. `--push` uploads the artifacts, manifest, and signature to the deployment registry.

#### Verification

When the registry receives a push request, it authenticates the publisher, verifies signatures, digests, and contracts, and rejects conflicting republications of the same `releaseVersion`. A `releaseVersion` follows a documented grammar and binds permanently to one `releaseDigest`. Only after those checks succeed does the registry expose the release in the catalog.

Pull-through import has its own validation before copying an external release locally; installation is a separate check that re-verifies the selected `releaseDigest` before admitting a package into the **mesh**.

### First Installation

A release in the registry catalog is not yet running in the mesh. The first installation admits it. For example, installing `g-issues@2.3.1`:

1. **Select a release.** A package administrator chooses the app and an immutable `releaseDigest` from the registry catalog.
2. **Review the contract.** The platform shows declared operations, dependencies, workflows, UIs, migrations, and required authorization relationships.
3. **Bind identities.** The administrator selects organization-managed service accounts for the runtime, migration jobs, and workflows when the release includes them.
4. **Configure operation authority.** For each operation, the administrator decides whether it runs only as the workload identity or may exercise delegated caller authority, and binds permissions as durable grants or delegated authority.
5. **Approve admission.** The administrator confirms the bindings. This requires `service_account.assign` for each identity and `authorization.grant` for each relationship or role being granted.
6. **Verify and prepare.** The control plane re-verifies the `releaseDigest`, resolves dependencies, reserves the package slot, runs prepare migrations, and provisions identities, mesh routes, and policies.
7. **Roll out.** The revision controller creates runtime workloads when needed, stages UI and workflow artifacts, and promotes the revision once readiness checks converge. The app then appears in user-facing catalogs and becomes invocable.

## Package Revisions

### Durable Deployment State

The control plane persists:

- **Package slots**: one organization-scoped slot keyed by canonical package identity, reserved before allocating an installation ID and used to fence concurrent first installs
- **Revision requests**: append-only install, upgrade, rollback, and removal intent with actor, previous promoted revision, target release or tombstone, deployment-registry catalog generation and applicable trust-policy revision, approved authorization bindings, resolved dependencies, and timestamps
- **Installation head**: last successfully promoted revision and current admitted candidate
- **Revision operation**: one generation-fenced state machine per installation, including phase, candidate Kubernetes resources, deadlines, stability timestamp, and routing generation
- **Migration outcomes**: immutable status keyed by installation, `releaseDigest`, and migration ID
- **Routing and catalog generations**: independent operation-routing, package, operation, UI, and workflow generations committed for the promoted revision
- **Terminal outcomes**: completion or failure phase, duration, and reason

### Installation

Publishing does not install a package. An authorized installer selects an immutable `releaseDigest` and binds:

- An organization-managed Kubernetes service account when the release has a runtime or migration
- Organization-managed workflow identities
- Package-declared authorization relationships or roles for each operation
- Caller-delegation policy per operation
- Initial replica, resource, and availability settings when the release has a runtime

Installation is a reconciled saga:

1. Verify that the release is complete in the deployment registry, then verify its source registry identity and applicable trust policy, release signature and referenced artifact digests, platform support, runtime compatibility, and schema versions.
2. Resolve exact dependency revisions and operation-contract digests.
3. Reserve the organization package slot and persist the revision request, approved authorization snapshot, and fenced revision operation.
4. Provision the installation and any required runtime, migration, and workflow identities, authorization relationships, ambient data-plane enrollment, and `AuthorizationPolicy` attachments.
5. Hand the fenced operation to the revision controller, which follows the canonical phases in [Revision Rollouts](#revision-rollouts).

Failures preserve phase and diagnostics. The revision controller retries within policy and cleans up resources created for an unpromoted revision when an administrator cancels. Promotion, catalog exposure, and canary behavior follow the rules in Revision Rollouts.

### Dependencies and Contracts

Dependencies specify canonical package identities, `releaseVersion` ranges, required or optional status, and operation-contract digests. Admission records the selected revisions and digests.

The platform validates:

- Every required dependency has a promoted compatible revision
- Required operations exist
- Required input and output contracts match
- The candidate remains compatible with existing reverse dependents
- No conflicting revision operation is active in the affected dependency subgraph
- Workflow calls reference compatible operation contracts

All schemas use the same JSON Schema version and are normalized before hashing. Initially, schemas are compatible only when their hashes match.

### Revision Rollouts

Runtime upgrades use Kubernetes blue/green rollouts: each candidate revision runs in a distinct immutable Deployment behind a revision-specific Service, and the serving Deployment is never restarted or mutated in place. Promotion advances the promoted head and all catalog generations in one state-store transaction only after routing, UI, and workflow prerequisites converge for a stability window; failure before that commit restores prior routing without advancing any generation. Convergence is verified from Kubernetes workload health, per-proxy routing-status reports, and ambient enrollment gates.

The canonical revision-operation phases are:

1. **Admitted**: validate release, policy, compatibility, and dependencies.
2. **Preparing**: run fenced, backward-compatible prepare migrations.
3. **Starting**: create runtime resources and stage candidate UI and workflow artifacts.
4. **Ready**: hold every applicable check for the stability window.
5. **Canarying**: send limited traffic only for contracts identical to the promoted revision, without catalog exposure.
6. **Promoting**: execute the convergence and atomic commit protocol above.
7. **Draining**: enforce the pinned-session and hard-deadline protocol below.
8. **Observing**: monitor production traffic for the configured period.
9. **Finalizing**: run separately approved irreversible migrations.
10. **Complete**: record convergence and timing.

The prior revision drains pinned sessions and MCP SSE streams until a hard deadline while new sessions route only to the promoted revision. Pre-promotion failure leaves the prior revision primary; post-promotion failure marks the new revision degraded without automatic rollback—the next revision operation waits for administrator retry, approved rollback, or forward fix.

### Migrations and Rollback

Migrations are separately identified, digest-pinned jobs. Each declares:

- Migration ID and `releaseDigest`
- Phase: which revision lifecycle stage runs the job
  - **Prepare** — runs before promotion while the prior revision may still serve traffic. Changes must be backward-compatible with the serving revision (expand/contract expand).
  - **Finalize** — runs after promotion once the candidate is primary. May be irreversible; destructive jobs require separate administrator approval (expand/contract contract).
  - **Compensating** — runs on approved rollback to reverse or repair data changes from prior migrations when returning to an earlier revision.

- Dedicated workload identity
- Timeout, resource limits, retry policy, and idempotency key
- Compatibility with the prior and candidate package revisions

## Runtime and Access

### Authentication and Authorization

The authorization service is the system of record for organization policy. It stores Zanzibar-style tuples used by management, user-invocation, workflow, and package-to-package authorization checks. Enforcement topology and mesh policy attachment are defined in [Runtime Architecture](#runtime-architecture).

- Users authenticate through an external identity provider with organization-level SSO, issuer and tenant allowlists, and session revocation.
- API keys are named, expiring, scoped API grants that act on behalf of an owner. Only hashes are stored, plaintext is returned once, and empty scope does not mean unrestricted access.
- `service_account.create` permits creating an organization-managed service account but grants it no authority.
- `service_account.assign` permits binding a service account to a package runtime, workflow definition, or workflow execution but grants it no new authority.
- `authorization.grant` permits granting a relationship or role and may be held only by a human subject. The grant cannot exceed the issuer's current authority, resource scope, conditions, or expiration.
- Every installed package runtime uses an organization-owned workload identity; workflow definitions and executions use organization-owned workflow identities.
- Every invocation is centrally authorized before dispatch; package-specific resource checks may further restrict it.
- Production does not start in a fail-open mode when identity or authorization is unavailable.

### Authorization Model and Evaluation

Authorization uses a provider-independent check protocol built around:

- A canonical **subject** being evaluated, such as the effective subject, immediate caller workload, or management actor
- An **action**, such as a package operation or administrative permission
- A canonical **resource**, identified by a reserved platform type or a package-namespaced type

The platform reserves resource types for registries, packages, releases, installations, revisions, workflows, identities, and authorization grants. Packages register namespaced resource types at admission and cannot use reserved names.

### Package Access Policy

Release manifests define only the authorization relationships or roles required to invoke each operation. MCP endpoints are mesh-internal by default. External invocation uses the deployment ingress and the same authorization service. Direct package egress defaults to deny and is managed by deployment policy rather than release metadata.

### Workflows

Packages publish immutable workflow definitions that the revision controller registers with a mesh workflow service; each execution is pinned to its definition and operation-contract digests until completion, migration, or retirement, while new executions use the promoted catalog generation.

### User Interfaces

TODO

### Catalogs and Administration

Representative endpoints:

- Deployment: `https://vt.valon.tools`
- Private registry: `https://vt.valon.tools/registry`
- Administration: `https://vt.valon.tools/admin`
- Package administration: `https://vt.valon.tools/packages/<package>/admin`

Management APIs require dedicated administrative permissions and should use a private listener or equivalent network controls where practical.

### Invocation

Authorized operations are available over the deployment endpoint and through the CLI:

```sh
vt invoke --deployment <url> <package> <operation> --args <json>
```

Authentication comes from an interactive session, keychain, environment, or standard input. Positional arguments never carry secrets or tokens.

Invocation uses MCP Streamable HTTP: JSON-RPC over POST with JSON or SSE responses. The gateway enforces request size, timeout, rate, and stream lifetime.

Ingress strips external identity and routing fields, authenticates the credential, and exchanges it for a short-lived signed caller token; it neither parses MCP nor authorizes operations. The token is bound to its issuer, waypoint audience, target installation, and raw envelope hash. It carries a unique ID; issuance, not-before, and expiry times; actor and effective subject; credential and delegation upper bounds; optional browser-capability claims; delegation chain; and trace ID.

### Development Sandbox

`vt dev` builds a local package and connects it only to an explicitly enabled per-user sandbox:

```sh
vt dev --deployment <url> <dir>
```

Authentication uses the normal interactive or keychain flow. The platform assigns a short-lived development workload identity distinct from the user. Routes are scoped to the developer or named test session and cannot claim a production package slug.

Development packages:

- Receive traffic only from their developer or explicitly named test sessions and never become the default production route
- Cannot run migrations
- Cannot receive or forward raw caller credentials
- Do not inherit the production workload's permissions
- Use sandbox-scoped authorization
- Expire automatically and are audited on connect and disconnect

The first implementation has one production deployment profile: Kubernetes with Istio ambient mode and the protected platform services described below. `vt build` may run locally, but there is no standalone laptop, VM, no-auth, or non-mesh production runtime. The development sandbox is a capability of a connected deployment rather than a second local control plane.

## Platform Architecture

### Deployment Tiers

The platform separates deployment into three trust and lifecycle tiers:

1. **Bootstrap substrate**: Kubernetes and Istio, the control-plane state store, revision controllers, certificate authority, and backup and recovery tooling. Terraform deploys these components before package APIs are available.
2. **Protected system packages**: identity, authorization, secret management, registry verification, audit, and workflow execution. After the bootstrap substrate is healthy, revision controllers install and upgrade these services using signed releases and the normal immutable revision and rollout machinery.
3. **Ordinary organization packages**: packages installed by authorized organization or package administrators and constrained by organization policy.

The bootstrap substrate and protected system packages are both inside the platform availability boundary. If a required component in either tier is unavailable, the affected capability is counted as platform downtime even when some existing package traffic continues. An ordinary package failure counts as downtime for that package, not for the platform, unless a platform defect caused or spread the failure.

### Trusted Platform Services

The bootstrap substrate and protected system packages provide these trusted services:

- **Identity service**: authenticates sessions and API grants, resolves canonical principals, manages organization service accounts, issues caller tokens, and manages signing keys for internal caller contexts and session wrappers.
- **Authorization service**: stores immutable policy models, grants, assignments, and revocations and evaluates provider-independent and Envoy `ext_authz` checks.
- **Secret-management service**: stores and rotates platform and connection credentials and releases secret material only to explicitly authorized workload identities.
- **Registry-verification service**: verifies the deployment registry, validates pull-through imports identified by canonical package ID, verifies manifests, digests, signatures, platform compatibility, trust policy, and local artifact completeness before catalog exposure and admission.
- **Audit sink**: accepts append-only security, authorization, administration, and runtime events in storage that packages cannot control.
- **Workflow service**: registers promoted package-defined workflows and runs pinned executions outside package runtimes.
- **Control-plane state store**: a bootstrap-substrate service that durably stores deployment-registry state, import attempts and provenance, package slots, revision requests and operations, routing and catalog generations, migration outcomes, and recovery metadata.

### Runtime Architecture

Production uses Istio ambient mode. Package Pods have no mesh or application proxy sidecar; they expose MCP and health endpoints directly. Every package runtime and migration executes as a Bun application in a platform-owned, unprivileged Bun container image; publishers cannot provide arbitrary images or Dockerfiles. The runtime combines `istiod`, mandatory node-level `ztunnel`, shared destination and ingress waypoints, gateways, these standardized Bun application containers, protected services, the state store, and revision controllers. Revision controllers reconcile durable state into Deployments, Services, waypoint enrollment, Gateway API routes, signed cluster-selection configuration, and `AuthorizationPolicy`; `istiod` distributes the resulting xDS configuration. `Istiod` owns endpoint, routing, certificate, and mesh-policy distribution—not revision state, grants, workflows, or catalogs.

**`ztunnel` configures (L4):**

- mTLS and identity-based L4 `AuthorizationPolicy` on workload ports, including bypass prevention for direct Pod IP or revision Service access
- Ambient workload enrollment, policy-generation binding, and readiness gating until the current generation is loaded
- L4 telemetry and audit for connection, byte, policy-denial, and bypass events

`ztunnel` cannot enforce L7 policy, decode MCP, select revision backends, or run `ext_authz` authorization checks.

**Waypoints configure (L7):**

- Gateway API `HTTPRoute` resources, waypoint enrollment, and protected extensions (MCP decoder, `ext_authz`, cluster selector, session wrapper, routing-status reporter, and audit adapter)
- MCP decoding, authorization, and operation-specific revision routing via decoder metadata and signed generation-fenced routing tables—not caller headers or `HTTPRoute` matching
- Signed session routing wrappers for SSE pinning, plus L7 request, MCP outcome, and trace telemetry

Ingress authenticates external credentials and mints caller tokens; the destination waypoint decodes and authorizes MCP requests. Health probes use a separate protected port with the minimum exception. `NetworkPolicy` provides reachability enforcement alongside ambient policy. Authorization fails closed; health ports have no public ingress. If a required security event cannot be delivered or durably buffered, the affected request fails closed. If `istiod` is unavailable, established traffic uses its last configuration; new revision operations requiring routing or policy changes fail closed until dependencies recover.

### Infrastructure and Service Rollouts

The mesh is redeployed only for changes to Istio or its trust and extension configuration, including:

- `istiod`, `ztunnel`, and waypoint proxy versions
- Ingress and egress gateway versions or deploy-pinned configuration
- Istio certificate-authority and trust-domain configuration
- Mesh-wide extension-provider and ambient-enrollment configuration

Protected system packages roll independently through the revision controller and do not require a mesh redeployment. Ordinary package revisions, registry trust-policy changes, identity assignments, and authorization-binding updates likewise change runtime or control-plane state without redeploying the mesh.

Bootstrap-substrate changes, including control-plane state migrations and backup or recovery tooling, use infrastructure rollouts but do not redeploy the mesh unless they also change an Istio item listed above. Package releases declare supported MCP and Bun versions. Infrastructure pins Kubernetes, Istio, Envoy, the platform Bun container image, the shared MCP extension, session-token format, authorization adapter, and routing-status extension versions and accounts for overlapping old and new versions before admitting releases that require new features. Registry trust-root changes remain separately authorized and audited.

## Representative Request Lifecycles

- Provision the deployment registry
- Pull a package release
- Publish a package release
- Upgrade a package
- Invoke a package operation
- Invoke a package operation (package-to-package)
- Authentication
- Connect external credentials

# Minimum Dependencies



## Glossary

| Term | Meaning |
| --- | --- |
| Canonical package ID | Global package identifier: the concatenation of `registryId` and registry-local `packageId`, written as `registryId/packageId`; never reused |
| Package identity | Equivalent to canonical package ID; also representable as `(registryId, packageId)` |
| Pull-through import | Verified, atomic copy of an external release and its complete artifact closure into the deployment registry, identified by canonical package ID and release version, using upstream read credentials supplied at import time |
| `releaseDigest` | Hash of canonical manifest bytes and authoritative install input |
| Revision | Installed release or removal tombstone |
| Revision operation | Fenced execution of durable revision intent |
| Promotion | Atomic commit advancing the promoted head and catalog generations |
| Routing generation | Durable correlation of one routing decision, its resources, proxy set, and observations |
| Atomic rollout group | One fenced promotion across mutually dependent packages |
| Caller token | Signed pre-authorization context presented only to a destination waypoint |
| Internal caller context | Signed post-authorization context forwarded to a runtime |
| Protected system package | Signer-pinned platform package unavailable to ordinary package APIs |
| Waypoint | Shared Envoy proxy for MCP authorization, revision routing, sessions, and telemetry |
| `ztunnel` | Node-level ambient proxy for L4 security and workload identity |

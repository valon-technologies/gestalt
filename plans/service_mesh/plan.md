# Service Mesh Design

## Contents

- [Overview](#overview)
- [Foundations](#foundations)
- [Publishing and Distribution](#publishing-and-distribution)
- [Package Revisions](#package-revisions)
- [Runtime and Access](#runtime-and-access)
- [Platform Architecture](#platform-architecture)
- [Representative Request Lifecycles](#representative-request-lifecycles)
- [Assurance](#assurance)
- [Glossary](#glossary)

## Overview

The platform builds, distributes, installs, and runs packages in a shared service mesh. A package release may include:

- An MCP server and its operations
- One or more single-page user interfaces
- Durable workflow definitions
- Dependency and migration metadata
- Runtime and operation-authorization requirements

Each package runtime Pod includes a platform-owned supervisor sidecar that gates readiness and shutdown, exposes the mesh listener, and reports observed runtime state. Kubernetes starts and terminates the sibling runtime container. UI-only and workflow-only packages do not create runtime Pods.

Each organization operates one deployment backed by a private registry and optional trusted registries. The deployment exposes catalogs of installed packages, callable operations, user interfaces, workflows, and dependencies.

Publishing makes a release discoverable. Installation admits an immutable release into an organization. Canarying permits controlled, undiscoverable traffic. Routing and external registrations converge before promotion atomically makes an installed revision primary and exposes its catalog generations. These milestones are separate and must not be inferred from one another.

This design is a full replacement for the existing Gestalt implementation. It does not preserve the existing provider runtime, package archive, registry, deployment, authorization, or workflow protocols, and it does not define an in-place migration. Feature names shared with the existing system do not imply wire-format or state compatibility.

## Foundations

### Design Invariants

The following invariants apply to every implementation:

1. A published release is immutable and identified by its `releaseDigest`.
2. A deployment records every accepted install, upgrade, rollback, and removal request in an append-only revision history.
3. For each canonical package identity in an organization, only one revision operation — install, upgrade, rollback, or removal — may be in progress at a time.
4. Durable revision requests record intent; revision controllers perform runtime work and resume safely after process failure.
5. Registry publication, revision-operation phase, live workload health, routing promotion, and catalog convergence are tracked separately.
6. A pre-promotion failure does not replace the last successfully promoted revision.
7. Routing propagation is eventually consistent, so every revision that may receive traffic remains contract-compatible with operations referenced by installed reverse dependencies until propagation and draining finish. New contracts become visible only after routing convergence. A candidate may omit an operation when no installed package or workflow definition or execution retained by policy references it; stale or undeclared calls to that operation may fail after promotion.
8. Packages run only under organization-provisioned identities; release metadata may define access requirements but cannot grant authority.
9. Ordinary packages cannot replace components needed to administer the platform. Identity, authorization, workflow execution, secret management, registry verification, and audit may be implemented only by protected system packages; certificates, state storage, revision control, and recovery remain part of the bootstrap substrate.
10. Every externally supplied manifest, image, schema, and migration is untrusted until its digest, signature, and policy are verified.
11. Every authorization decision uses canonical identities and one immutable policy generation. Grants change only through explicit authorized actions; policy-model changes cannot silently delete or orphan them.
12. Raw user credentials and caller-supplied identity fields never cross into package runtimes. Internal caller context is short-lived, signed, and bound to its intended audience and target.

### Identity Model

The system keeps these identities separate:

- **Registry identity**: an immutable self-certifying `registryId` derived from the registry's offline root public key. The identifier is globally stable for that registry but does not imply endorsement by, or membership in, a central registry namespace. Signed registry descriptors authorize the current origin set and origin changes.
- **Package identity**: permanent `(registryId, packageId)` assigned when the package is reserved. A slug is a mutable display and discovery alias, never a security identifier.
- **Release identity**: `(registryId, packageId, releaseVersion, releaseDigest)`.
- **Installation identity**: an organization-scoped UUID representing one installed package.
- **Workload identity**: the installation principal used in authorization checks for one installation's runtime or migration job, bound at installation to an organization-managed Kubernetes service account.
- **Workflow identity**: the principal assigned to a workflow definition or execution.
- **Actor identity**: the user, API grant, platform service account, or workflow that initiated an action.
- **Effective subject**: the principal whose delegated authority an invocation exercises.

Authentication resolves a credential to one canonical principal before authorization. Gateways remove caller-supplied internal identity fields, and downstream services accept identity only from verified workload identity or signed internal context. Delegation keeps the actor and effective subject distinct throughout authorization and audit.

Package IDs are never reused. Slug renames retain aliases and tombstones. Content published under a different registry receives a distinct package identity, even when the release bytes are identical.

Release metadata cannot name workload identities or Kubernetes service accounts. Runtime identities and authorization bindings are selected from organization-managed resources during installation.

## Publishing and Distribution

### Package Releases

Each `(registryId, packageId, releaseVersion)` has one release manifest with detached signatures. A release with a runtime declares one or more OS, architecture, and optional variant tuples and references an OCI runtime image index covering them.

Runtime and migration images may contain platform-specific binaries, but contracts, dependencies, workflow definitions, user interfaces, and operation access requirements are shared across all supported platforms.

The release manifest contains:

- Canonical registry and package IDs, slug, and `releaseVersion`
- Optional OCI runtime image-index digest, supported platforms, startup instructions, and probe contract
- Zero or more digest-addressed migration image indexes with their supported platforms
- Zero or more MCP operations with input and output schema digests
- Zero or more digest-addressed workflow definitions and UI bundles
- Required and optional package and operation dependencies
- Ordered prepare, finalize, and compensating migration jobs
- Required authorization relationships or roles for each operation
- Supported MCP protocol versions and private runtime control API versions
- Minimum and maximum compatible package-supervisor versions
- Optional source repository URL and source revision

The registry stores signatures separately from the manifest. Each signature identifies the exact release or image digest it covers.

Source repository and revision are optional display metadata. Publication must support releases without either field; when present, catalogs and administration views may use them for links and context.

The `releaseDigest` is the hash of the canonical manifest bytes. Catalog entries, signatures, and installation records carry it; the manifest does not.

`releaseVersion` follows a documented registry grammar and comparison order. `(registryId, packageId, releaseVersion)` binds permanently to one `releaseDigest`: conflicting republications fail, while identical retries are idempotent and preserve publication time and metadata. Mutable tags may aid discovery but are never installation inputs.

### Package Runtime Contract

A release may contain an MCP runtime, static user interfaces, workflow definitions, or any combination of them. A release with no MCP runtime does not create a runtime Deployment. Protected system packages may additionally expose platform-owned typed APIs; those APIs are versioned platform protocols and are not part of the ordinary package operation catalog.

Every MCP runtime image follows one platform runtime contract:

- Kubernetes starts the runtime container through the manifest-declared executable and arguments. The image contains no embedded platform credential.
- The supervisor and runtime mount one private in-memory volume. The runtime exposes MCP Streamable HTTP on `/run/gestalt/mcp.sock` and the typed runtime control API on `/run/gestalt/control.sock`; it never binds a mesh-reachable application port directly.
- The supervisor owns the mesh-facing listener and relays MCP to the private MCP socket without changing JSON-RPC semantics. Configuration, readiness, cancellation, and graceful shutdown use the separate control socket.
- Startup, liveness, and readiness probes are distinct. Readiness reports the loaded `releaseDigest`, operation-contract digest set, MCP version, and private runtime control API version.
- The supervisor supplies configuration and secret references through the platform runtime API. Secret values are fetched under the installation workload identity and are never embedded in release metadata.
- Graceful shutdown first rejects new sessions, then allows pinned requests and streams to drain until the revision deadline. The supervisor requests shutdown over the control API and fails its readiness gate; Kubernetes remains responsible for container termination.
- The runtime accepts caller identity only through a verified supervisor context. Caller-supplied identity headers, credentials, and routing fields are discarded before dispatch.

The private runtime control API is a versioned typed protocol covering configuration, readiness, stream cancellation, graceful shutdown, and observed runtime state. It does not carry MCP invocation payloads. Its compatibility range is recorded independently from the MCP protocol and package-supervisor version range. Ordinary packages cannot replace or extend this protocol.

The OCI artifact specification defines canonical media types and paths for the release manifest, image indexes, UI bundles, workflow definitions, schemas, and migration images. Canonical manifest serialization, digest calculation, detached-signature envelopes, package reservation, and publication idempotency are normative parts of the registry protocol rather than implementation details.

### Registries

The registry protocol is open and has no central creation or approval authority. Anyone may operate a registry using the reference creation tooling or an independent implementation that serves the required registry descriptor, APIs, and signature bundles. Each organization independently decides whether to connect to and trust that registry.

Registries control who can publish, discover, or download releases. Each configured registry has:

- The registry's immutable `registryId`, root public key, descriptor generation, and verified origin set
- Discovery, metadata, blob, and publication endpoints
- Optional read credentials for discovery and download from a private registry
- A registry trust policy binding each trusted public-key fingerprint to a publisher identity and the package IDs that key may sign
- Whether local clients may publish directly
- A policy revision incremented when trust or local publication rules change

The registry root key signs descriptors containing the `registryId`, protocol version, authorized origin set, operational signing keys, and descriptor generation. Deployments verify that the identifier matches the root key and accept origin additions, removals, mirrors, and disaster-recovery endpoints only through a newer valid descriptor. The offline root key does not sign package releases; publisher and operational keys rotate independently.

The registry's publisher policy controls which releases it accepts. Each connected organization's registry trust policy independently controls which of those releases the organization will admit and may be more restrictive. Registry connection credentials, when present, are read-only and are stored by the secret-management service. Publishers authenticate to the registry with their own user or CI credentials, which the platform does not retain. Artifact verification uses the registry trust policy and public trust material, not registry credentials.

Connecting a registry does not trust its publishers. Catalogs and manifests remain untrusted until admission verifies digests, signatures, schema versions, and the registry trust policy.

The registry provides distinct APIs for paginated discovery, reading immutable metadata and blobs, and authenticated publication.

A registry catalog contains paginated package and release summaries that reference authoritative manifest digests, without duplicating release contracts. Per-package indexes use monotonic revisions and compare-and-swap updates.

Disconnecting a registry blocks discovery and installation but preserves its identity and history. Hard removal fails while any desired, running, dependent, or in-progress revision references it, or while its artifacts remain eligible for rollback or disaster recovery under retention policy. A replacement is a new registry; package identities do not carry over.

### Build and Publish

`vt build <dir>` validates package metadata and builds the applicable digest-addressed runtime, migration, UI, workflow, and schema artifacts, zero or more OCI image indexes, and one release manifest locally. Adding `--push` publishes the result to the selected registry:

```sh
vt build <dir> --push
```

Without `--push`, the command does not modify a registry.

Publication is a recoverable transaction:

1. Reserve `(packageId, releaseVersion)` and create a publish-attempt ID.
2. Record a pending release with any optional source metadata.
3. Upload all applicable content-addressed blobs, image indexes, and manifests.
4. Validate all referenced artifacts and release contracts server-side.
5. Store the immutable release manifest.
6. Attach signatures.
7. After all artifacts are stored, atomically add the release to the package catalog. Retry if another publisher changed the catalog first.
8. Mark the attempt succeeded or failed.

Pending and failed attempts are visible but never installable. Concurrent publishers cannot lose catalog updates. Stale attempts become failed, and orphaned content-addressed blobs are garbage-collected after a grace period.

## Package Revisions

### Durable Deployment State

The control plane persists:

- **Package slots**: one organization-scoped slot keyed by canonical package identity, reserved before allocating an installation ID and used to fence concurrent first installs
- **Revision requests**: append-only install, upgrade, rollback, and removal intent with actor, previous promoted revision, target `releaseDigest`, registry trust policy revision, approved authorization bindings, resolved dependencies, and timestamps
- **Installation head**: last successfully promoted revision and current admitted candidate
- **Revision operation**: one generation-fenced state machine per installation, including phase, candidate Kubernetes resources, deadlines, stability timestamp, and routing generation
- **Migration outcomes**: immutable status keyed by installation, `releaseDigest`, and migration ID
- **Routing and catalog generations**: independent operation-routing, package, operation, UI, and workflow generations committed for the promoted revision
- **Terminal outcomes**: completion or failure phase, duration, and reason

The revision-operation phase is the canonical lifecycle state. Image availability, process status, replica health, routing synchronization, and catalog generation are observations attached to that operation, not additional lifecycle states.

### Installation

Publishing does not install a package. An authorized installer selects an immutable `releaseDigest` and binds:

- An organization-managed Kubernetes service account when the release has a runtime or migration
- Organization-managed workflow identities
- Package-declared authorization relationships or roles for each operation
- Caller-delegation policy per operation
- Initial replica, resource, and availability settings when the release has a runtime

Before admission, the platform shows package-declared operation access requirements and resulting authorization bindings. The installer — an organization administrator or package administrator with install authority — needs `service_account.assign` for each selected identity and `authorization.grant` for each relationship or role being granted.

Installation is a reconciled saga:

1. Verify the registry identity and trust policy, release and image signatures, platform support, runtime compatibility, and schema versions.
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

Declared dependencies and workflow references are the authoritative compatibility boundary for operation removal. A candidate may remove an operation when no installed package or workflow definition or execution retained by policy references it, including after accounting for other revisions in the same atomic rollout group. External callers do not create an implicit dependency: callers using stale catalogs, cached operation names, or otherwise undeclared contracts may receive an operation-unavailable error after promotion.

Validation reads a revisioned desired-state snapshot and commits with generation preconditions. Independent packages may upgrade concurrently. Upgrades affecting the same dependency subgraph use deterministic locking or one atomic rollout group. Cyclic dependencies may be upgraded sequentially when every intermediate state is compatible. Mutually dependent changes require an atomic rollout group.

An atomic rollout group is one durable request containing candidates for multiple package slots, one desired-state snapshot, and one lock order. Admission validates compatibility across the complete before-and-after graph. Controllers may prepare, start, and canary members independently. All members' routing intents and external registration prerequisites converge first; promoted heads and package, operation, UI, and workflow catalog generations then commit in one generation-fenced state-store transaction. A pre-commit failure restores routing for every member and advances none of their promoted heads.

### Revision Rollouts

Releases with an MCP runtime use a Kubernetes-native blue/green rollout with optional Istio canary traffic. Each runtime upgrade creates a candidate revision in a distinct immutable Kubernetes Deployment behind a revision-specific Service. The promoted and candidate Deployments may run together, so the serving Deployment is never restarted or mutated in place. Runtime-less releases use the same durable revision state machine without creating runtime Deployments or Services.

Revision records use optimistic concurrency and fencing tokens, preventing a controller that loses its lease from mutating a newer generation. Retries are idempotent; every external mutation has a stable idempotency key. For example, controller A may start an upgrade at generation 4, create the candidate Deployment, and then lose its lease. Controller B takes over at generation 5. Any later update from controller A is rejected, while controller B can safely retry the Deployment request without creating a duplicate.

For runtime releases, Kubernetes and the package supervisor are the source of truth for live workload health. A runtime candidate is ready only when Kubernetes has observed the Deployment generation, its required replicas are available, and package-supervisor readiness confirms the target `releaseDigest` for a stability window. Replacement pods and autoscaled replicas count by current readiness, not persistent identity.

Before any candidate becomes promotable, the interface service must verify and stage every UI bundle, and the workflow service must register every workflow definition as candidate-only and acknowledge its digest. These records remain unreachable from user catalogs. A release with no runtime reaches Ready from artifact verification and the required UI and workflow acknowledgements; Starting, Canarying, runtime routing, Draining, and runtime observation are skipped when they do not apply.

Each installation with an MCP runtime has one stable frontend Service and revision-specific backend Services. Gateway API `HTTPRoute` resources select weighted revision backends; callers never address a revision Service directly. Packages may call other packages only through declared dependencies and these mesh-managed routes. Direct or undeclared package-to-package calls are blocked.

The controller assigns each routing change a durable `routingGeneration` and records the exact Kubernetes resource UIDs, generations, and desired route hashes that implement it. Infrastructure installs a protected routing-status extension in every ingress gateway and waypoint; the extension exposes, over an mTLS-protected administration endpoint, the routing generations and route hashes currently loaded by that proxy. This extension and endpoint are versioned with the pinned Istio/Envoy build and covered by infrastructure conformance tests.

The relevant proxy set is a persisted membership snapshot of available ingress-gateway and destination-waypoint replicas selected for those resources. Convergence requires accepted and resolved Gateway API status, ready route backends, every member's routing-status report matching the desired hashes, and authenticated synthetic probes observing the target generation through each proxy class. The controller does not infer application convergence from one global Istio xDS version. Proxy replacement, loss of availability, or membership change resets the stability window. A bounded timeout fails the phase and leaves enough recorded evidence for deterministic retry or restoration.

Gateway and waypoint readiness is gated on the routing-status extension reporting either the current committed generation or an active generation-fenced rollout generation authorized for that proxy and route. During promotion, the proxy may remain ready on the fenced candidate generation while convergence is observed. Immediately after the promotion commit, only the newly committed generation satisfies readiness. A new or restarted proxy cannot enter Service endpoints or load-balancer membership until it has loaded and reported the then-current committed generation, so proxy churn after promotion cannot reintroduce stale routing.

Promotion has one commit point. For runtime releases, the controller first writes primary routing intent while the previous revision remains the persisted promoted head. After routing converges and all UI-serving and workflow-registration acknowledgements remain valid for the stability window, one state-store transaction updates the promoted revision and the relevant routing, package, operation, UI, and workflow generations. Only that transaction constitutes successful promotion. If a prerequisite fails before the transaction, the controller restores previous routing and external registrations and confirms their convergence; it does not advance the promoted head or any catalog generation.

The control plane persists rollout intent and decisions, then reconstructs progress from Kubernetes, the package supervisor, and Istio after restart. Historical rollout outcomes remain immutable; current workload health is a separate live view.

The canonical revision-operation phases are:

1. **Admitted**: verify the release, policy, runtime compatibility, dependencies, and reverse dependents.
2. **Preparing**: run backward-compatible prepare migrations as fenced one-shot jobs.
3. **Starting**: create any revision-specific runtime resources and stage candidate-only UI bundles and workflow definitions.
4. **Ready**: require all applicable runtime, UI-serving, and workflow-registration checks for the configured stability window.
5. **Canarying**: route a configurable small traffic percentage for operations whose contracts exactly match the promoted revision; the candidate remains absent from user-facing catalogs.
6. **Promoting**: converge applicable routing and external registrations, then atomically commit the promoted head and relevant package, operation, UI, workflow, and routing generations.
7. **Draining**: keep the prior revision available for in-flight requests and streams until their drain deadline.
8. **Observing**: monitor the promoted revision under production traffic for a configured period.
9. **Finalizing**: run explicitly approved irreversible finalize migrations when required.
10. **Complete**: record convergence and terminal timing.

Starting runtime resources, Canarying, runtime routing, Draining, runtime observation, and Finalizing are skipped when they do not apply. A failure or cancellation records a terminal outcome against the phase where the operation stopped rather than introducing another progression phase.

Each phase has a timeout, retry policy, and terminal error taxonomy. Readiness requires the configured replica capacity, not a single ready pod. During routing propagation and draining, revisions follow the compatibility and operation-removal rules in [Dependencies and Contracts](#dependencies-and-contracts). Existing MCP SSE streams remain pinned to their original revision while the old revision drains, including streams for an operation omitted by the candidate.

Pre-promotion failures mark the candidate failed and leave the prior revision primary. Failures during post-promotion observation mark the new revision degraded. Neither case triggers automatic rollback. Degraded or finalization-error states block the next revision operation until an administrator retries, initiates an approved rollback, or approves a forward fix. Finalization failure records `promoted_with_finalization_error`; it neither marks the promoted revision failed nor changes routing.

Compatible packages target zero planned downtime. Any release requiring downtime must declare it and receive explicit administrator approval before admission.

### Migrations and Rollback

Migrations are separately identified, digest-pinned jobs. Each declares:

- Migration ID and `releaseDigest`
- Prepare, finalize, or compensating phase
- Dedicated workload identity
- Timeout, resource limits, retry policy, and idempotency key
- Compatibility with the prior and candidate package revisions

Migration jobs use least privilege and deny-by-default egress. They never run as the installer, revision controller, or installation's normal workload identity. Attempts and outcomes are durable.

The first version does not classify rollback safety in release metadata or initiate rollback automatically. Prepare migrations follow expand/contract rules and remain compatible with the serving revision. Destructive finalization requires separate administrator approval. Before manual rollback, an administrator must confirm data compatibility and select any required declared compensating migration; the platform does not infer rollback safety. Administrators may initiate an approved rollback through the revision controller; shell commands cannot update revision state.

### Removal

Removal is a durable tombstoned revision, not deletion of a catalog row:

1. Reject removal while required reverse dependents exist, unless they are removed in the same atomic rollout group.
2. In one fenced transition, promote tombstones for the package, operation, UI, and workflow generations, disable interface launch routes, unregister promoted workflow definitions, stop new workflow executions, and prevent new invocation resolution.
3. Drain resolved requests and streams and handle queued or running workflow executions according to their recorded retirement policy.
4. Stop the removed package's runtime workloads, scale them to zero, and block any stale ingress or package-initiated egress.
5. Revoke workload authorization bindings, installation-owned assignments, and delegated tokens.
6. Apply package data retention or destruction policy.
7. Preserve the package tombstone and audit history.
8. Garbage-collect runtime resources after the recovery window.

Removal preserves organization-owned grants and distinguishes them from installation-owned grants. Reinstallation creates a new installation identity unless an approved restore reactivates the tombstone.

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

Each authorization grant wraps one relationship tuple and records an immutable ID, organization, human issuer, creation and optional expiration, source authority, and policy revision. Grant mutation creates a new record; revocation appends a revocation record rather than rewriting or deleting history. Revoking or narrowing the source authority invalidates dependent grants. Platform controllers may apply a recorded human-authorized grant but cannot originate one.

The first implementation does not cache authorization decisions. Internal invocation tokens are short-lived, and every workflow step is authorized independently. Existing streams may continue until their configured lifetime expires. Security events go to an immutable audit sink outside package-controlled storage.

### Authorization Model and Evaluation

Authorization uses a provider-independent check protocol built around:

- A canonical **subject** being evaluated, such as the effective subject, immediate caller workload, or management actor
- An **action**, such as a package operation or administrative permission
- A canonical **resource**, identified by a reserved platform type or a package-namespaced type

The platform reserves resource types for registries, packages, releases, installations, revisions, workflows, identities, and authorization grants. Package-defined resource types are namespaced by canonical package identity and cannot use reserved names. Users and service accounts are subjects unless the requested action manages the identity itself.

A decision returns allow or deny together with the immutable policy model ID and revision used to evaluate it. A batch form evaluates multiple checks against the same policy snapshot and has the same semantics as individual checks; catalogs and controllers use it to avoid per-item authorization calls.

The authorization model defines resource types, relations, actions, and which relations satisfy each action. Relationship targets may be direct subjects, resources, or subject sets such as the members of a group. Subject-set traversal is cycle-safe. Package invocation uses the installation identity as the resource and the canonical operation ID as the action; package-specific checks may use a more specific package-owned resource.

The authorization service stores the policy model and organization-managed grants in one authoritative runtime system. Platform bootstrap registers reserved resource types, while package admission registers namespaced resource types and operation requirements. Each model change produces an immutable, content-hashed generation. Grants may be revoked at any time. A policy-model change cannot remove or rename a resource type or relation referenced by active grants unless those grants are revoked or migrated in the same transaction. Grants are indexed by subject, relation, and resource so evaluation does not scan the complete relationship set.

Credential scopes are an upper bound on authority: a relationship grant cannot restore access excluded by the authenticated credential. Missing subjects, actions, resources, policy models, or provider availability produce a denial.

### Package Access Policy

Release manifests define only the authorization relationships or roles required to invoke each operation. They do not declare configuration, secrets, ingress, or egress capabilities.

Package upgrades cannot directly change organization-managed grants. A subject with appropriately scoped `authorization.grant` may update those grants within their delegated authority. An upgrade succeeds admission only when all required bindings and delegation approvals are present; otherwise it fails before rollout and may be retried after they are supplied.

MCP endpoints are mesh-internal by default. External invocation uses the deployment ingress and the same authorization service. Direct package egress defaults to deny and is managed by deployment policy rather than release metadata.

### Workflows

Packages publish immutable workflow definitions, not workers. The revision controller registers promoted definitions with a workflow MCP service in the mesh. Each execution records its definition digest, actor, workflow identity, and compatible operation-contract digests.

The workflow service runs each execution against its pinned definition until completion, migration, or documented retirement. New executions use the promoted workflow catalog generation. Removal and incompatible upgrades account for queued work, retries, timers, and long-running executions.

Every workflow operation is authorized at execution. Selecting an organization-managed service account as a workflow identity requires `service_account.assign`. The triggering actor remains in the audit and delegation chain.

Each workflow definition has a package-stable ID and immutable definition digest. Its release metadata declares input and output schemas, operation-contract references, retry and timeout policy, supported signals, timer behavior, and whether concurrent executions are allowed. Definition IDs cannot be reassigned to unrelated workflows.

Running executions remain pinned to their definition and operation-contract digests. A replacement definition does not mutate them. Before an incompatible upgrade or removal, admission requires one explicit policy for every affected execution class: allow pinned executions to finish, stop starting new executions and retire after a deadline, or run a declared migration that produces a new execution record. Forced termination is a separately authorized and audited action. Signals, timers, retries, and queued steps follow the pinned definition until the selected policy completes.

`runAs` means explicit delegation from the triggering actor to the selected workflow identity. It never substitutes an unverified subject. The authorization service validates the delegation at execution start, and every step separately checks the workflow identity, effective subject, target operation, and current credential upper bound.

### User Interfaces

UI bundles are immutable content-addressed artifacts referenced by the release manifest. Each interface declares a package-stable interface ID, entry document, asset root, visibility, CSP profile, and an operation allowlist. Bundles are static and cannot contain server-side executable code.

The platform assigns each installation interface a stable non-executable launch origin and a distinct executable origin for every promoted UI generation beneath a deployment-controlled wildcard application domain. The launch origin only redirects to the currently promoted generation and serves no package-controlled document or script. Package generations never share an executable origin, cookies, service workers, browser storage, or CSP authority with deployment administration, protected services, another interface, or an older generation.

Private interfaces use an origin-scoped session established through the normal external identity flow. The interface service issues a short-lived browser capability only when the request originates from the currently promoted generation's verified entry document. The capability names the installation, interface ID, UI generation, allowed operations, and exact generation-specific origin. Ingress validates that capability and the `Origin` header in addition to normal subject authorization; it rejects stale generations and browser calls outside the declared allowlist. A public interface must be explicitly allowed by organization policy, and public asset access does not make its operations public. Browser code never receives workload credentials or raw internal invocation tokens.

The platform applies the declared CSP profile, strict CORS, MIME validation, integrity metadata, response-size limits, immutable caching for revision-qualified assets, and service-worker scope restrictions. Interfaces requiring embedding use opaque sandboxed frames and explicit `postMessage` schemas; embedding does not grant same-origin access.

### Catalogs and Administration

An organization deployment provides:

- A package catalog
- An operation catalog
- A user-interface catalog
- A workflow catalog
- A dependency graph
- Package revisions, rollout and migration status, identities, and authorization settings
- API grant management
- A development sandbox

Administrative catalogs may expose candidate metadata. User-facing package, operation, UI, and workflow catalogs expose only their atomically promoted generations. New operation contracts appear only after applicable routing, UI-serving, and workflow-registration prerequisites in [Revision Rollouts](#revision-rollouts) have converged.

Clients treat catalogs as snapshots. Invocation remains authoritative and may reject stale or undeclared operations under the rules in [Dependencies and Contracts](#dependencies-and-contracts).

Organization administrators can connect registries and install, upgrade, roll back, or remove packages. Package administrators can perform revision and administration actions within their delegated package and authorization scope. Package developers can build, publish, test, and invoke packages. Package users see only catalogs and operations authorized for them.

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

Ingress strips externally supplied internal-identity headers and exchanges user credentials for a short-lived, signed internal invocation token. The token is bound to its issuer, audience, target installation, request class, operation when applicable, and request-envelope hash and carries a unique ID, issuance, not-before, and expiry times, the original actor, effective subject, immediate caller workload identity, delegation chain, and trace ID. Signing keys rotate with an overlap window for in-flight requests. Packages never receive or forward raw user bearer tokens or API keys.

The deployment exposes package-scoped MCP endpoints rather than one endpoint that multiplexes package names from JSON-RPC bodies. A trusted MCP request decoder at ingress validates the JSON-RPC envelope, resolves the canonical operation ID from the MCP method and parameters, rejects ambiguous or batch requests that target more than one authorization scope, and writes immutable dynamic metadata for authorization and routing. External clients cannot supply that metadata. The decoder binds a hash of the authorized request envelope into the internal invocation token so a downstream component cannot change the target operation after authorization.

MCP methods map to authorization and routing as follows:

- `initialize`, `ping`, and session negotiation require the installation-level `package.connect` action and do not select an operation.
- Catalog and discovery methods require their catalog-read action and return only entries authorized for the effective subject.
- Operation calls such as `tools/call` resolve the package-stable operation ID from validated parameters and require that operation's action and resource checks.
- Cancellation, progress, and other session notifications must reference an existing authorized session and inherit its installation, revision, actor, and effective subject. They cannot change authorization scope.
- Unsupported methods fail closed. A JSON-RPC batch is accepted only when every item has the same target installation, request class, operation, authorization scope, session, and routing target and each item is authorized independently; otherwise the complete batch is rejected.

The destination waypoint uses the package endpoint and verified token claims for routing and `ext_authz`; it does not trust a caller-supplied operation header. The supervisor verifies the token, request-envelope hash, target installation, request class, operation when applicable, and selected revision before dispatch. Malformed JSON-RPC, unsupported MCP methods, unknown operations, mismatched hashes, and expired tokens fail before reaching the runtime.

After weighted routing selects a revision Service, a protected waypoint extension creates a short-lived signed dispatch receipt bound to the invocation-token ID, installation ID, revision ID, `releaseDigest`, routing generation, and request-envelope hash. Waypoint receipt-signing certificates are issued and rotated by the identity service for the waypoint workload identity. The supervisor verifies this receipt and confirms that its loaded digest matches it; packages never receive the receipt. Session continuation receipts must name the revision already pinned by the session.

MCP session IDs are client-opaque signed routing tokens issued by the supervisor under an identity-service-certified workload signing key. They contain the installation, revision, `releaseDigest`, routing generation, issued and expiry times, and random nonce. Before backend selection, the waypoint verifies the token and routes subsequent requests to its named revision Service. Cancellation is forwarded to that revision, and reconnect succeeds only while the token is valid and the revision remains inside its drain deadline. SSE streams send platform heartbeats, have a configured maximum lifetime, and remain pinned until completion, cancellation, or that deadline.

Authorization is enforced at two surfaces:

- **Ingress gateway**: user invocation, catalogs, package administration, and package revision APIs. The gateway's `ext_authz` filter calls the authorization service before forwarding to control-plane or package services.
- **Destination waypoint**: every MCP call is checked before revision routing. For external calls, the waypoint revalidates the ingress-issued token and authorized request metadata; for package calls, it performs the corresponding workload and effective-subject authorization check. Waypoint enforcement is described in [Runtime Architecture](#runtime-architecture).

Both surfaces use one authorization service and relationship graph. MCP checks always use the effective subject, target installation, and request-class action. Package-originated calls additionally use the immediate caller workload identity. Operation calls additionally use the canonical operation ID; connection, catalog, session, and notification classes use the actions defined in the MCP method mapping and do not invent an operation. Management checks use the actor and requested revision or administration action.

For an MCP operation call, the authorization decision intersects:

- The caller workload identity's permission to invoke the target when the call originated from a package
- The effective subject's permission
- The installed package's approved operation access policy
- Any operation-specific resource authorization

Credential scope and every relationship check applicable to the request class must allow the request. The decision records the policy model ID and revision. Missing or unknown identities, installations, request classes, required operations, policies, or authorization-provider outages deny access.

For package-to-package calls, the caller asks the identity service to mint an internal invocation token over its authenticated workload channel. The request names one declared dependency, operation, effective subject, and request-envelope hash. The identity service verifies the caller's workload identity and delegation chain, while the authorization service evaluates the call before a token is issued. Tokens are single-target, short-lived, non-refreshable by the package, and cannot broaden the caller's credential or delegated authority. Package SDKs perform this exchange and send the resulting token to the destination Service.

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

Protected system packages have fixed canonical identities, trusted-signer allowlists, dedicated platform workload identities, protected names and routes, and platform-only revision permissions. They cannot grant themselves authority, be replaced by an ordinary package, or be removed through ordinary package APIs. Infrastructure configuration pins the initial system-package releases and bootstrap order; after identity and authorization are available, subsequent changes require normal platform authorization.

Initial bootstrap uses a deliberately narrower trust path than ordinary admission:

1. Terraform provisions the state store, revision controllers, an append-only audit spool, secret bootstrap backend, certificate authority, and an offline-pinned bundle containing the initial protected-package manifests, digests, signer keys, platform identities, and network policy.
2. A bootstrap controller verifies that bundle without registry, identity, authorization, secret-service, or audit-service APIs. It creates the initial secret, audit, identity, and authorization workloads directly from digest-pinned images rather than through ordinary package admission.
3. Initial workloads use prebound Kubernetes service accounts and static namespace, mTLS, and `NetworkPolicy` allowlists from the signed bundle. Bootstrap secrets are mounted from the substrate backend. Security events are synchronously appended to the substrate audit spool until the audit package durably imports them.
4. Terraform supplies the initial human administrator identities and organization root grants as signed bootstrap records. The authorization service imports each record once and rejects replay. Identity, authorization, secrets, and audit then prove their typed APIs, policy attachments, secret access, and durable audit delivery through bootstrap conformance checks.
5. Using those now-normal services, the bootstrap controller directly creates the digest-pinned registry-verification workload, which verifies its own pinned manifest and signer before adoption. The revision controller then installs workflow execution through ordinary protected-package admission. It adopts every directly created workload into immutable revision records only after its manifest, identity, policy, secrets, and audit history pass the same checks.
6. The controller writes an irreversible bootstrap-complete marker and removes bootstrap-only API access, secret mounts, static allowlists, and controller permissions. From that point, bootstrap records and offline bundles cannot authorize upgrades; every protected-package change uses normal signed releases, revision control, and platform authorization.

Loss of identity or authorization does not reopen bootstrap mode. Recovery uses separately documented state-store restore and break-glass procedures with offline quorum approval and immutable audit records.

The bootstrap substrate and protected system packages are both inside the platform availability boundary. If a required component in either tier is unavailable, the affected capability is counted as platform downtime even when some existing package traffic continues. For example, a registry-verification outage is publishing and installation downtime, while an identity or authorization outage may prevent new requests without immediately terminating established connections. An ordinary package failure counts as downtime for that package, not for the platform, unless a platform defect caused or spread the failure.

### Trusted Platform Services

The bootstrap substrate and protected system packages provide these trusted services:

- **Identity service**: authenticates sessions and API grants, resolves canonical principals, manages organization service accounts, and issues short-lived internal invocation tokens.
- **Authorization service**: stores immutable policy models, grants, assignments, and revocations and evaluates provider-independent and Envoy `ext_authz` checks.
- **Secret-management service**: stores and rotates platform and connection credentials and releases secret material only to explicitly authorized workload identities.
- **Registry-verification service**: retrieves registry metadata and verifies origins, manifests, digests, signatures, platform compatibility, and the registry trust policy before admission.
- **Audit sink**: accepts append-only security, authorization, administration, and runtime events in storage that packages cannot control.
- **Workflow service**: registers promoted package-defined workflows and runs pinned executions outside package runtimes.
- **Control-plane state store**: a bootstrap-substrate service that durably stores registries, package slots, revision requests and operations, routing and catalog generations, migration outcomes, and recovery metadata.

The Istio control plane provides the certificate authority and workload-certificate lifecycle. Backup and recovery tooling protects control-plane state and trusted-service configuration but does not sit on the request path.

Each trusted service exposes a versioned typed API. Control-plane clients use shared libraries for workload authentication, canonical caller context, deadlines, bounded retries, idempotency keys, metrics, tracing, and audit metadata. Istio provides service discovery, mTLS, and network routing, so the first implementation does not add a generic control-plane proxy.

### Runtime Architecture

Production uses Istio ambient mode. `ztunnel` provides L4 transport security and workload identity, while waypoint proxies provide L7 routing and authorization. Istio data-plane sidecars are not planned for the first implementation; the package supervisor remains an application sidecar.

The runtime consists of:

- **Istio control plane (`istiod`)**: distributes routing, policy, endpoint, and certificate configuration.
- **`ztunnel`**: mandatory node proxies providing L4 transport security and workload identity.
- **Waypoint proxies**: shared Envoy proxies providing L7 routing, telemetry, and authorization for destination Kubernetes Services.
- **Ingress gateways**: terminate external transport, remove untrusted identity headers, authenticate credentials, enforce `ext_authz`, and route public and organization traffic.
- **Egress gateways**: enforce and audit organization-declared external access that cannot safely use direct egress.
- **Package supervisor (`gestaltd`)**: one platform-owned sidecar container in each runtime Pod that gates runtime readiness and shutdown through the control API, exposes the mesh-facing MCP listener, verifies invocation context, and reports observed state. It is not an Istio proxy.
- **Package runtime**: an unprivileged container that implements the private side of the package runtime contract. Static interfaces are served by a protected platform service, not by this container.
- **Protected system packages**: provide identity, authorization, secrets, registry verification, audit, and workflow execution as defined above.
- **Control-plane state store**: provides durable state for trusted services and revision controllers as part of the bootstrap substrate.
- **Revision controllers**: reconcile durable revision requests and rollout state into workloads, routing, catalogs, identities, and policy.

Revision controllers use trusted-service APIs and the Kubernetes API to create or update Deployments, stable frontend and revision Services, waypoint enrollment, Gateway API routes, traffic weights, and `AuthorizationPolicy` resources. `istiod` watches the Kubernetes and Istio resources, translates the desired state into xDS configuration, and distributes it to ingress gateways, waypoints, and `ztunnel`. The protected routing-status extension reports loaded route hashes and routing generations; revision controllers require those reports and active probes before advancing promoted catalog generations.

Istio keeps workload endpoints, routing, certificates, and mesh-policy configuration synchronized. It does not own or synchronize package revision state, authorization grants, workflow executions, or catalogs. Those remain the responsibility of revision controllers and trusted platform services. Package operation invocations remain on the mesh data plane and use ingress or destination-waypoint authorization.

`ztunnel` cannot enforce L7 authorization. MCP, revision, and administration checks therefore run at ingress gateways or destination waypoints. Every stable package Service is explicitly enrolled in a destination waypoint. Ingress-to-Service traffic is configured to traverse that waypoint, and `AuthorizationPolicy` `CUSTOM` rules use `targetRefs` for the enrolled Service or Gateway. Admission fails if the required waypoint, enrollment, extension provider, route, or policy attachment is absent or not accepted.

The ingress request decoder, external authenticator, internal-token exchange, and authorization adapter are protected extensions pinned by infrastructure configuration. The decoder executes before `ext_authz`. Ingress removes all external copies of their reserved headers and dynamic metadata before decoding. Only decoded metadata and signed token claims may select an operation or revision.

After token verification, the waypoint derives a trusted operation routing key from the signed claim. Gateway API header matches may use only that platform-generated key to select operation-specific canary weights; no route matches a client-supplied operation header. Route configuration remains declarative and digest-correlated with the operation contracts admitted for both revisions.

Production uses `ztunnel` for transport security and L4 workload authorization, Kubernetes `NetworkPolicy` for reachability, and ingress or waypoint `ext_authz` for application authorization. Authorization checks fail closed on denial or outage. Only the mesh data plane can reach package application ports; health and supervisor administration ports have minimal explicit exceptions and no public ingress.

If `istiod` is unavailable, established traffic uses its last configuration. New revision operations requiring routing or policy changes fail closed until dependencies recover.

Terraform manages the bootstrap substrate, including clusters, networking, container infrastructure, state storage, revision controllers, certificate authority, and recovery tooling. Package installation does not mutate this substrate.

### Production Operational Baseline

Production uses a regional Kubernetes cluster dedicated to the platform rather than assuming an unrelated existing cluster. Infrastructure configuration pins supported Kubernetes, Gateway API, Istio, CNI, `ztunnel`, gateway, and waypoint versions. Upgrades use documented skew ranges, staged control-plane and data-plane rollout, conformance tests, and rollback criteria.

Terraform defines regional node placement, autoscaling bounds, resource quotas, default requests and limits, Pod disruption budgets, topology-spread constraints, priority classes, and capacity reserved for protected services. Protected services and routing proxies remain available during one-zone loss and planned node disruption. Revision admission fails when required capacity cannot be reserved.

Network infrastructure preserves deployment-owned ingress addresses, TLS certificate and DNS validation, outbound NAT addresses, private DNS, private-service routes, and OAuth callback origins. Default-deny `NetworkPolicy` and ambient enrollment apply to every package namespace. External destinations use an audited organization egress policy; an egress gateway is required when source-IP stability, protocol inspection, or centralized policy cannot be provided safely by direct egress.

The state store and trusted-service state have encrypted regional replication, point-in-time recovery, versioned backup formats, restore verification, and declared RPO and RTO. Disaster-recovery tests cover state restoration, certificate and signing-key recovery, route reconstruction, protected-package bootstrap recovery without reopening bootstrap authorization, and reconciliation of Kubernetes resources from durable state.

Security audit storage uses retention-locked, package-inaccessible storage with independently managed encryption and deletion authority. Delivery has bounded buffering, loss and lag alerts, schema versions, and trace correlation. An audit outage fails closed for security-sensitive mutations when the event cannot be durably buffered.

Operational documentation defines DNS and gateway rollback, expired-certificate recovery, authorization and identity outages, stuck routing convergence, state-store restore, cluster loss, signer compromise, and quorum-controlled break-glass access. Break-glass credentials are offline, time-bounded, scope-limited, and always produce independently retained audit evidence.

### Infrastructure and Service Rollouts

The mesh is redeployed only for changes to Istio or its trust and extension configuration, including:

- `istiod`, `ztunnel`, and waypoint proxy versions
- Ingress and egress gateway versions or deploy-pinned configuration
- Istio certificate-authority and trust-domain configuration
- Mesh-wide extension-provider and ambient-enrollment configuration

Protected system packages roll independently through the revision controller and do not require a mesh redeployment. Ordinary package revisions, registry connection changes, identity assignments, and authorization-binding updates likewise change runtime or control-plane state without redeploying the mesh. `gestaltd` supervisor updates roll with the workloads that use them.

Bootstrap-substrate changes, including control-plane state migrations and backup or recovery tooling, use infrastructure rollouts but do not redeploy the mesh unless they also change an Istio item listed above. Package releases independently declare supported MCP versions, private runtime control API versions, and compatible `gestaltd` supervisor versions. Infrastructure pins Kubernetes, Istio, Envoy, routing-status extension, and dispatch-receipt protocol versions and accounts for overlapping old and new versions before admitting releases that require new features. Registry trust-root changes remain separately authorized and audited.

## Representative Request Lifecycles

The following lifecycles show how representative registry, management, and data-plane requests move through the system.

### Create a Registry

Registry creation is a registry-side operation and is distinct from connecting that registry to an organization. The reference registry-creation service and CLI are convenience tools, not gatekeepers; operators may instead use the underlying libraries or build any compatible implementation.

1. A registry operator uses the reference tooling or an independent implementation to provision a protocol-compatible registry and durable metadata store. The registry uses OCI 1.1 storage—Google Artifact Registry by default—for content-addressed images, manifests, and signature bundles.
2. The registry generates an offline root key and derives its immutable `registryId` from that public key. The identifier remains stable wherever the registry is connected.
3. The operator configures discovery and download visibility, publisher authentication, who may create packages, and who may publish versions to each package. The reference tooling may also configure optional GitHub listeners or other publication triggers.
4. The operator configures the registry's publisher policy by registering publisher identities, their public keys, and the package IDs each key may sign. The reference tooling may provision KMS-backed signing keys, but private keys remain publisher-controlled and are never stored by the registry.
5. The registry publishes a descriptor matching the required schema, including its protocol version, `registryId`, API endpoints, and supported signature formats, through its well-known metadata endpoint.
6. Readiness checks verify metadata and blob durability, digest validation, authentication, authorization, and signature enforcement before the registry accepts publication or discovery traffic.

### Connect a Registry

1. An organization administrator submits the registry's HTTPS origin, optional read credentials for a private registry, and a registry trust policy binding trusted publisher identities and public keys to package IDs.
2. The ingress gateway authenticates the actor, removes untrusted identity headers, and calls the authorization service through `ext_authz`. The request proceeds only when the actor has the required registry-management permission.
3. The registry-verification service connects through the permitted egress path and reads the registry's protocol version, immutable `registryId`, API endpoints, catalog, and signature bundles. Remote content remains untrusted.
4. The control plane verifies the self-certifying `registryId`, descriptor signature and generation, and that the submitted origin is in the signed origin set. It rejects an origin already associated with a different registry identity. It stores any read credentials through the secret-management service and persists the verified registry identity, descriptor generation, and initial registry trust policy revision.
5. After verification succeeds, the control plane marks the registry connected and advances the registry catalog generation. Packages become discoverable, but each release must still pass admission checks before installation.
6. The API returns the registry record and verification status. Failures preserve diagnostics without making the registry available for discovery or installation.

### Publish a Package Release

1. A developer or CI job authenticates directly to the registry with its own publishing credential and runs `vt build <dir> --push`.
2. `vt` validates the package metadata, builds every applicable digest-addressed artifact and OCI image index, creates the canonical release manifest, and calculates its `releaseDigest`.
3. The registry authorizes the publisher to reserve `(packageId, releaseVersion)` and creates a recoverable publish-attempt ID. A version already bound to a different digest is rejected.
4. `vt` uploads the content-addressed blobs, image indexes, and release manifest. The registry recalculates every digest and validates all referenced artifacts and release contracts before making anything discoverable.
5. The publisher signs a statement containing the `registryId`, `packageId`, `releaseVersion`, `releaseDigest`, and every referenced runtime, migration, UI, workflow, and schema artifact digest with an authorized KMS-backed key, then uploads the signature bundle.
6. The registry verifies the signature and confirms under its publisher policy that the signing key may publish the package. It then atomically binds the version to the digest and adds the release to the package catalog.
7. The registry returns the immutable `releaseDigest`. Failed or interrupted attempts remain non-installable and may be retried; publishing does not install or deploy the release.

### Upgrade a Package

1. An organization or package administrator selects a target `releaseDigest` for an installed package and submits a durable upgrade revision request with any approved identity or authorization changes.
2. The ingress gateway authenticates and authorizes the actor. The control plane verifies any required `service_account.assign` and `authorization.grant` permissions.
3. The control plane appends the revision request, verifies that no revision operation is active for the installation, and creates a generation-fenced revision operation. The API returns the operation ID so the client can observe progress asynchronously.
4. The revision controller verifies the recorded registry trust policy revision, release and image signatures, platform and runtime compatibility, schemas, dependencies, reverse dependents, workflows, and migrations against the recorded desired-state snapshot.
5. The controller runs declared prepare migrations, creates any immutable candidate runtime resources, stages UI bundles and workflow definitions, and applies required ambient enrollment and `AuthorizationPolicy` attachments. Candidate artifacts remain absent from user-facing catalogs and receive no default production traffic.
6. After applicable readiness remains stable, the controller may canary runtime operations whose contracts exactly match the promoted revision. Promotion writes any primary routing intent and waits for routing status, UI-serving, and workflow-registration prerequisites associated with the candidate generations.
7. After applicable routing, UI-serving, and workflow-registration prerequisites remain converged for the stability window, one transaction advances the promoted head and relevant routing, package, operation, UI, and workflow generations. The previous runtime revision drains pinned requests and streams while the controller observes the promoted revision and runs any separately approved finalization.
8. The controller records the terminal outcome. A pre-promotion failure leaves the previously promoted revision primary.

### Invoke a Package Operation

1. An external caller sends an MCP Streamable HTTP request to the package-scoped deployment endpoint with a session credential or API grant and a JSON-RPC envelope.
2. The ingress gateway authenticates the credential, removes untrusted internal metadata, applies request limits, and decodes the MCP request class and canonical operation when applicable before calling the authorization service through `ext_authz`.
3. The authorization service evaluates the credential scope, effective subject, target installation, request class, canonical operation ID when applicable, approved package access policy, and any operation-specific resource against one policy generation. An unknown target or denied check fails before dispatch.
4. The gateway exchanges the external credential for a short-lived internal invocation token bound to the target installation, request class, applicable operation, and request-envelope hash.
5. The destination waypoint revalidates the token, applies the trusted operation-specific route when applicable, selects a revision Service, and attaches a signed dispatch receipt. A new session becomes pinned to that revision.
6. The package supervisor verifies the token and dispatch receipt and relays the MCP request to the private runtime socket. The runtime returns one JSON-RPC response or opens an SSE stream; an open stream remains on the selected revision while that revision drains.
7. The response returns through the mesh and ingress gateway. The platform records the authorization decision, policy model ID and revision, actor, effective subject, immediate caller workload identity, target, `releaseDigest`, internal token and dispatch-receipt IDs, trace ID, duration, and outcome.

Package-to-package invocation does not pass through ingress. The calling package presents a platform-issued internal invocation token to the destination waypoint, which performs the corresponding workload and effective-subject authorization checks before revision routing and dispatch. Raw caller credentials are never forwarded.

## Assurance

### Retention and Audit

Release metadata, accepted revision history, and audit events are retained according to applicable policy. Registries and platform-owned artifact storage retain blobs required by active revisions or the configured rollback window. Unreferenced blobs may be garbage-collected after a grace period; shared OCI layers are deleted only after references are rechecked.

Authentication, authorization, registry, package-lifecycle, migration, secret-access, and network-policy events are written to the immutable audit sink with the actor, target, decision, relevant identifiers, trace ID, and outcome.

### Required Verification

Before production use, automated tests cover:

- **Registry and release integrity**: atomic publication under retries and concurrency, signature and compatibility rejection, and safe disconnection and garbage collection
- **Runtime contract**: supervisor protocol negotiation, digest-aware readiness, request-envelope decoding, token binding, cancellation, session pinning, SSE draining, and malformed or ambiguous MCP requests
- **Revision state machines**: validation failures, administrator, controller, and dependency races, restart at every phase, and migration idempotency and lease loss
- **Rollout and recovery**: canary and promotion failures before and after routing intent, proxy-membership churn, route restoration, atomic rollout groups, catalog convergence, draining and SSE behavior, rollback approval, removal, tombstones, retained workflows, autoscaling, and mixed versions
- **Platform lifecycle**: bootstrap ordering and outages, protected identities and signers, ordinary-package replacement prevention, and independent infrastructure and system-package rollouts
- **Authorization models**: deterministic policy publication, active-grant preservation, subject-set cycles, and parity between batch and individual checks
- **Identity and delegation**: spoofing, claim validation, token expiration and rotation, `runAs`, confused-deputy prevention, attenuation, and revocation
- **Fail-closed enforcement**: authorization outages, data-plane bypass attempts, and ingress or waypoint `ext_authz` failures
- **Interfaces and workflows**: route collisions, CSP and asset validation, revision-qualified caching, pinned workflow executions, signals and timers during upgrades, retirement, migration, and forced termination
- **Disaster recovery**: regional state restore, route reconstruction, signing-key and certificate recovery, protected-service recovery, audit continuity, and break-glass controls

## Glossary

**package identity**<br>
Permanent `(registryId, packageId)` for a registry package. Assigned when the package is reserved; never reused.

**release**<br>
Immutable artifact with detached signatures for one `releaseVersion` of a package, identified by `releaseDigest`.

**releaseVersion**<br>
Registry-scoped version label for one published release of a package. `(registryId, packageId, releaseVersion)` binds permanently to one `releaseDigest`.

**releaseDigest**<br>
Content hash of the canonical release manifest bytes. The authoritative install input; signatures bind to it.

**revision**<br>
One installed state of a package in an organization, backed by a specific `releaseDigest`.

**revision request**<br>
Durable intent to install, upgrade, rollback, or remove a package.

**revision operation**<br>
Generation-fenced state machine that executes one revision request.

**revision controller**<br>
Background worker that drives a revision operation to completion and resumes after failure.

**rollout**<br>
Path from admission through readiness, optional canarying, and promotion for an install or upgrade candidate.

**promotion**<br>
Atomic state-store commit that advances the promoted revision and relevant routing, package, operation, UI, and workflow generations after all applicable prerequisites have converged.

**routing generation**<br>
Durable identifier that correlates one routing decision with its exact Kubernetes resources, desired route hashes, relevant proxy-membership snapshot, and convergence observations.

**atomic rollout group**<br>
One generation-fenced revision request that validates and promotes mutually dependent package revisions as a single desired-state and catalog change.

**authorization model**<br>
Immutable, content-hashed definition of resource types, relations, actions, and allowed relationship targets used for one generation of authorization decisions.

**relationship tuple**<br>
An assignment of a subject, resource, or subject set to one relation on a resource.

**internal invocation token**<br>
Short-lived signed context carrying verified caller and delegation information, bound to its issuer, audience, target installation, request class, optional operation, and request-envelope hash.

**package supervisor**<br>
Platform-owned `gestaltd` sidecar in each runtime Pod that implements the mesh-facing runtime contract, verifies invocation context, gates runtime readiness and shutdown, and reports observed state.

**bootstrap substrate**<br>
Infrastructure that must exist before package APIs are available, including Kubernetes, Istio, control-plane state, revision controllers, certificate authority, and recovery tooling.

**protected system package**<br>
A trusted, signer-pinned package that uses normal revision rollouts but has a fixed platform identity and cannot be replaced or removed through ordinary package APIs.

**waypoint**<br>
Shared Envoy proxy that enforces L7 routing and authorization for Kubernetes Services exposing packages in ambient mode.

**workflow service**<br>
MCP service in the mesh that registers package-defined workflows and executes them outside package processes.

**ztunnel**<br>
Node-level ambient proxy that provides L4 transport security and workload identity for enrolled pods.

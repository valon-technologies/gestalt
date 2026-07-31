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

A platform package supervisor runs beside each installed package, starts its MCP server, and reports its observed runtime state.

Each organization operates one deployment backed by a private registry and optional trusted registries. The deployment exposes catalogs of installed packages, callable operations, user interfaces, workflows, and dependencies.

Publishing makes a release discoverable. Installation admits an immutable release into an organization. Canarying permits controlled, undiscoverable traffic. Promotion makes an installed revision primary, and convergence then makes its contracts user-discoverable. These milestones are separate and must not be inferred from one another.

## Foundations

### Design Invariants

The following invariants apply to every implementation:

1. A published release is immutable and identified by its `releaseDigest`.
2. A deployment records every accepted install, upgrade, rollback, and removal request in an append-only revision history.
3. For each canonical package identity in an organization, only one revision operation — install, upgrade, rollback, or removal — may be in progress at a time.
4. Durable revision requests record intent; revision controllers perform runtime work and resume safely after process failure.
5. Registry publication, revision-operation phase, live workload health, routing promotion, and catalog convergence are tracked separately.
6. A pre-promotion failure does not replace the last successfully promoted revision.
7. Routing propagation is eventually consistent, so every revision that may receive traffic remains contract-compatible with operations referenced by installed reverse dependencies until propagation and draining finish. New contracts become visible only after routing convergence. A candidate may omit an operation when no installed package or retained workflow references it; stale or undeclared calls to that operation may fail after promotion.
8. Packages run only under organization-provisioned identities; release metadata may define access requirements but cannot grant authority.
9. Ordinary packages cannot replace components needed to administer the platform. Identity, authorization, workflow execution, secret management, registry verification, and audit may be implemented only by protected system packages; certificates, state storage, revision control, and recovery remain part of the bootstrap substrate.
10. Every externally supplied manifest, image, schema, and migration is untrusted until its digest, signature, and policy are verified.
11. Every authorization decision uses canonical identities and one immutable policy generation. Grants change only through explicit authorized actions; policy-model changes cannot silently delete or orphan them.
12. Raw user credentials and caller-supplied identity fields never cross into package runtimes. Internal caller context is short-lived, signed, and bound to its intended audience and target.

### Identity Model

The system keeps these identities separate:

- **Registry identity**: deployment-scoped immutable `registryId` plus its configured origin. The identifier distinguishes registries within one platform deployment; it is not a global registry namespace.
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

Each `(packageId, releaseVersion)` has one signed release manifest and may support one or more `os`, `architecture`, and optional `variant` tuples. Stored by digest, the manifest references an OCI image index covering those platforms.

Platform images may contain different binaries, but contracts, dependencies, migrations, workflow definitions, and operation access requirements are shared across all supported platforms.

The release manifest contains:

- Canonical registry and package IDs, slug, and `releaseVersion`
- OCI image-index digest and supported platforms
- MCP server startup instructions and interface entry points
- MCP endpoint, operations, and input/output schema digests
- Included workflow definitions and user interfaces
- Required and optional package and operation dependencies
- Ordered prepare, finalize, and compensating migration jobs
- Required authorization relationships or roles for each operation
- Startup, liveness, and readiness endpoints
- Minimum and maximum supported package-supervisor and mesh protocol versions
- Optional source repository URL and source revision

The registry stores signatures separately from the manifest. Each signature identifies the exact release or image digest it covers.

Source repository and revision are optional display metadata. Publication must support releases without either field; when present, catalogs and administration views may use them for links and context.

The `releaseDigest` is the hash of the canonical manifest bytes. Catalog entries, signatures, and installation records carry it; the manifest does not.

`releaseVersion` follows a documented registry grammar and comparison order. `(registryId, packageId, releaseVersion)` binds permanently to one `releaseDigest`: conflicting republishes fail, while identical retries are idempotent and preserve publication time and metadata. Mutable tags may aid discovery but are never installation inputs.

### Registries

The registry protocol is open and has no central creation or approval authority. Anyone may operate a registry using the reference creation tooling or an independent implementation that serves the required registry descriptor, APIs, and signature bundles. Each organization independently decides whether to connect to and trust that registry.

Registries control who can publish, discover, or download releases. Each configured registry has:

- An immutable `registryId` that is unique within the platform deployment
- Discovery, metadata, blob, and publication endpoints
- Optional read credentials for discovery and download from a private registry
- A registry trust policy binding each trusted public-key fingerprint to a publisher identity and the package IDs that key may sign
- Whether local clients may publish directly
- A policy revision incremented when trust or local publication rules change

The registry's publisher policy controls which releases it accepts. Each connected organization's registry trust policy independently controls which of those releases the organization will admit and may be more restrictive. Registry connection credentials, when present, are read-only and are stored by the secret-management service. Publishers authenticate to the registry with their own user or CI credentials, which the platform does not retain. Artifact verification uses the registry trust policy and public trust material, not registry credentials.

Connecting a registry does not trust its publishers. Catalogs and manifests remain untrusted until admission verifies digests, signatures, schema versions, and the registry trust policy.

The registry provides distinct APIs for paginated discovery, reading immutable metadata and blobs, and authenticated publication.

A registry catalog contains paginated package and release summaries that reference authoritative manifest digests, without duplicating release contracts. Per-package indexes use monotonic revisions and compare-and-swap updates.

Disconnecting a registry blocks discovery and installation but preserves its identity and history. Hard removal fails while any desired, running, redeployable, dependent, or in-progress revision references it. A replacement is a new registry; package identities do not carry over.

### Build and Publish

`vt build <dir>` validates package metadata and builds an OCI image index and release manifest locally. Adding `--push` publishes the result to the selected registry:

```sh
vt build <dir> --push
```

Without `--push`, the command does not modify a registry.

Publication is a recoverable transaction:

1. Reserve `(packageId, releaseVersion)` and create a publish-attempt ID.
2. Record a pending release with any optional source metadata.
3. Upload content-addressed image blobs and manifests.
4. Validate all platform images and release contracts server-side.
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
- **Routing and catalog generation**: the promoted revision visible to callers
- **Terminal outcomes**: completion or failure phase, duration, and reason

The revision-operation phase is the canonical lifecycle state. Image availability, process status, replica health, routing synchronization, and catalog generation are observations attached to that operation, not additional lifecycle states.

### Installation

Publishing does not install a package. An authorized installer selects an immutable `releaseDigest` and binds:

- An organization-managed Kubernetes service account for the installation's workload identity
- Organization-managed workflow identities
- Package-declared authorization relationships or roles for each operation
- Caller-delegation policy per operation
- Initial replica, resource, and availability settings

Before admission, the platform shows package-declared operation access requirements and resulting authorization bindings. The installer — an organization administrator or package administrator with install authority — needs `service_account.assign` for each selected identity and `authorization.grant` for each relationship or role being granted.

Installation is a reconciled saga:

1. Verify the registry identity and trust policy, release and image signatures, platform support, runtime compatibility, and schema versions.
2. Resolve exact dependency revisions and operation-contract digests.
3. Reserve the organization package slot and persist the revision request, approved authorization snapshot, and fenced revision operation.
4. Provision the installation, workload, and migration identities, authorization relationships, ambient data-plane enrollment, and required `AuthorizationPolicy` attachments.
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

Declared dependencies and workflow references are the authoritative compatibility boundary for operation removal. A candidate may remove an operation when no installed package or retained workflow references it, including after accounting for other revisions in the same atomic rollout plan. External callers do not create an implicit dependency: callers using stale catalogs, cached operation names, or otherwise undeclared contracts may receive an operation-unavailable error after promotion.

Validation reads a revisioned desired-state snapshot and commits with generation preconditions. Independent packages may upgrade concurrently. Upgrades affecting the same dependency subgraph use deterministic locking or one atomic multi-package rollout plan. Cyclic dependencies may be upgraded sequentially when every intermediate state is compatible. Mutually dependent changes require an atomic rollout group.

### Revision Rollouts

Upgrades use a Kubernetes-native blue/green rollout with optional Istio canary traffic. Each upgrade creates a candidate revision in a distinct immutable Kubernetes Deployment behind a revision-specific Service. The promoted and candidate Deployments may run together, so the serving Deployment is never restarted or mutated in place.

Revision records use optimistic concurrency and fencing tokens, preventing a controller that loses its lease from mutating a newer generation. Retries are idempotent; every external mutation has a stable idempotency key. For example, controller A may start an upgrade at generation 4, create the candidate Deployment, and then lose its lease. Controller B takes over at generation 5. Any later update from controller A is rejected, while controller B can safely retry the Deployment request without creating a duplicate.

Kubernetes and the package supervisor are the source of truth for live workload health. A candidate is ready only when Kubernetes has observed the Deployment generation, its required replicas are available, and package-supervisor readiness confirms the target `releaseDigest` for a stability window. Replacement pods and autoscaled replicas count by current readiness, not persistent identity.

Istio routes traffic between revision-specific Services. Promotion converges when every currently live relevant ingress gateway and destination waypoint reports the target xDS generation and their Kubernetes Deployments remain available for a stability window. If the set of live routing proxies changes, the stability window resets. Packages may call other packages only through declared dependencies and mesh-managed routes. Direct or undeclared package-to-package calls are blocked.

The control plane persists rollout intent and decisions, then reconstructs progress from Kubernetes, the package supervisor, and Istio after restart. Historical rollout outcomes remain immutable; current workload health is a separate live view.

The canonical revision-operation phases are:

1. **Admitted**: verify the release, policy, runtime compatibility, dependencies, and reverse dependents.
2. **Preparing**: run backward-compatible prepare migrations as fenced one-shot jobs.
3. **Starting**: create the revision-specific Deployment and Service with no production traffic.
4. **Ready**: require Kubernetes availability and package-supervisor readiness for the configured stability window.
5. **Canarying**: route a configurable small traffic percentage for operations whose contracts exactly match the promoted revision; the candidate remains absent from user-facing catalogs.
6. **Promoting**: route primary traffic to the candidate, wait for live relevant proxies to remain synced to the routing generation, and then expose newly added contracts in callable catalogs. Promotion makes the candidate the last successfully promoted revision.
7. **Draining**: keep the prior revision available for in-flight requests and streams until their drain deadline.
8. **Observing**: monitor the promoted revision under production traffic for a configured period.
9. **Finalizing**: run explicitly approved irreversible finalize migrations when required.
10. **Complete**: record convergence and terminal timing.

Canarying and Finalizing may be skipped when they do not apply. A failure or cancellation records a terminal outcome against the phase where the operation stopped rather than introducing another progression phase.

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

1. Reject removal while required reverse dependents exist, unless they are removed in the same atomic plan.
2. Promote a tombstone generation that removes callable catalog entries and prevents new invocation resolution.
3. Drain resolved requests and streams, stop new workflow executions, and handle queued or running executions according to policy.
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

Administrative catalogs may expose candidate metadata. Callable catalogs are keyed to the converged routing generation and expose new contracts only after the promotion and synchronization requirements in [Revision Rollouts](#revision-rollouts) are satisfied.

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

Ingress strips externally supplied internal-identity headers and exchanges user credentials for a short-lived, signed internal invocation token. The token is bound to its issuer, audience, target package, and operation and carries a unique ID, issuance, not-before, and expiry times, the original actor, effective subject, immediate caller workload identity, delegation chain, and trace ID. Signing keys rotate with an overlap window for in-flight requests. Packages never receive or forward raw user bearer tokens or API keys.

Authorization is enforced at two surfaces:

- **Ingress gateway**: user invocation, catalogs, package administration, and package revision APIs. The gateway's `ext_authz` filter calls the authorization service before forwarding to control-plane or package services.
- **Destination waypoint**: package-to-package MCP calls are checked before forwarding to the MCP server. Waypoint enforcement is described in [Runtime Architecture](#runtime-architecture).

Both surfaces use one authorization service and relationship graph. Checks use the immediate caller workload identity, effective subject, target package, and operation for MCP traffic; management checks use the actor and requested revision or administration action.

For MCP invocation, the authorization decision intersects:

- The authenticated caller workload identity's permission to invoke the target
- The effective subject's permission
- The installed package's approved operation access policy
- Any operation-specific resource authorization

Credential scope and every applicable relationship check must allow the request. The decision records the policy model ID and revision. Unknown identities, packages, operations, policies, or authorization-provider outages deny access.

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

## Platform Architecture

### Deployment Tiers

The platform separates deployment into three trust and lifecycle tiers:

1. **Bootstrap substrate**: Kubernetes and Istio, the control-plane state store, revision controllers, certificate authority, and backup and recovery tooling. Terraform deploys these components before package APIs are available.
2. **Protected system packages**: identity, authorization, secret management, registry verification, audit, and workflow execution. After the bootstrap substrate is healthy, revision controllers install and upgrade these services using signed releases and the normal immutable revision and rollout machinery.
3. **Ordinary organization packages**: packages installed by authorized organization or package administrators and constrained by organization policy.

Protected system packages have fixed canonical identities, trusted-signer allowlists, dedicated platform workload identities, protected names and routes, and platform-only revision permissions. They cannot grant themselves authority, be replaced by an ordinary package, or be removed through ordinary package APIs. Infrastructure configuration pins the initial system-package releases and bootstrap order; after identity and authorization are available, subsequent changes require normal platform authorization.

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

Production uses Istio ambient mode. `ztunnel` provides L4 transport security and workload identity, while waypoint proxies provide L7 routing and authorization. Sidecars are not planned for the first implementation.

The runtime consists of:

- **Istio control plane (`istiod`)**: distributes routing, policy, endpoint, and certificate configuration.
- **`ztunnel`**: mandatory node proxies providing L4 transport security and workload identity.
- **Waypoint proxies**: shared Envoy proxies providing L7 routing, telemetry, and authorization for destination Kubernetes Services.
- **Ingress gateways**: terminate external transport, remove untrusted identity headers, authenticate credentials, enforce `ext_authz`, and route public and organization traffic.
- **Egress gateways**: enforce and audit organization-declared external access that cannot safely use direct egress.
- **Package supervisor (`gestaltd`)**: runs beside an installation, manages its process lifecycle and local invocation, and reports observed state. It is not an Istio proxy.
- **Package runtime**: runs the package's MCP server and serves its interfaces.
- **Protected system packages**: provide identity, authorization, secrets, registry verification, audit, and workflow execution as defined above.
- **Control-plane state store**: provides durable state for trusted services and revision controllers as part of the bootstrap substrate.
- **Revision controllers**: reconcile durable revision requests and rollout state into workloads, routing, catalogs, identities, and policy.

Revision controllers use trusted-service APIs and the Kubernetes API to create or update Deployments, Services, waypoint enrollment, routes, traffic weights, and `AuthorizationPolicy` resources. `istiod` watches the Kubernetes and Istio resources, translates the desired state into xDS configuration, and distributes it to ingress gateways, waypoints, and `ztunnel`. Proxies acknowledge the configuration they receive; revision controllers observe synchronization for the target routing generation before advancing callable catalogs.

Istio keeps workload endpoints, routing, certificates, and mesh-policy configuration synchronized. It does not own or synchronize package revision state, authorization grants, workflow executions, or catalogs. Those remain the responsibility of revision controllers and trusted platform services. Package operation invocations remain on the mesh data plane and use ingress or destination-waypoint authorization.

`ztunnel` cannot enforce L7 authorization. MCP, revision, and administration checks therefore run at ingress gateways or destination waypoints. Kubernetes Services exposing MCP servers enroll in a waypoint, where revision controllers attach `AuthorizationPolicy` `CUSTOM` rules.

Production uses `ztunnel` for transport security and L4 workload authorization, Kubernetes `NetworkPolicy` for reachability, and ingress or waypoint `ext_authz` for application authorization. Authorization checks fail closed on denial or outage. Only the mesh data plane can reach package application ports; health and supervisor administration ports have minimal explicit exceptions and no public ingress.

If `istiod` is unavailable, established traffic uses its last configuration. New revision operations requiring routing or policy changes fail closed until dependencies recover.

Terraform manages the bootstrap substrate, including clusters, networking, container infrastructure, state storage, revision controllers, certificate authority, and recovery tooling. Package installation does not mutate this substrate.

### Infrastructure and Service Rollouts

The mesh is redeployed only for changes to Istio or its trust and extension configuration, including:

- `istiod`, `ztunnel`, and waypoint proxy versions
- Ingress and egress gateway versions or deploy-pinned configuration
- Istio certificate-authority and trust-domain configuration
- Mesh-wide extension-provider and ambient-enrollment configuration

Protected system packages roll independently through the revision controller and do not require a mesh redeployment. Ordinary package revisions, registry connection changes, identity assignments, and authorization-binding updates likewise change runtime or control-plane state without redeploying the mesh. `gestaltd` supervisor updates roll with the workloads that use them.

Bootstrap-substrate changes, including control-plane state migrations and backup or recovery tooling, use infrastructure rollouts but do not redeploy the mesh unless they also change an Istio item listed above. Package releases declare compatible `gestaltd` and mesh protocol ranges, and infrastructure rollouts account for overlapping old and new versions before admitting releases that require new features. Registry trust-root changes remain separately authorized and audited.

## Representative Request Lifecycles

The following lifecycles show how representative registry, management, and data-plane requests move through the system.

### Create a Registry

Registry creation is a registry-side operation and is distinct from connecting that registry to an organization. The reference registry-creation service and CLI are convenience tools, not gatekeepers; operators may instead use the underlying libraries or build any compatible implementation.

1. A registry operator uses the reference tooling or an independent implementation to provision a protocol-compatible registry and durable metadata store. The registry uses OCI 1.1 storage—Google Artifact Registry by default—for content-addressed images, manifests, and signature bundles.
2. The registry generates one immutable `registryId`. The identifier scopes package and release identities and must be unique among registries connected to the same platform deployment.
3. The operator configures discovery and download visibility, publisher authentication, who may create packages, and who may publish versions to each package. The reference tooling may also configure optional GitHub listeners or other publication triggers.
4. The operator configures the registry's publisher policy by registering publisher identities, their public keys, and the package IDs each key may sign. The reference tooling may provision KMS-backed signing keys, but private keys remain publisher-controlled and are never stored by the registry.
5. The registry publishes a descriptor matching the required schema, including its protocol version, `registryId`, API endpoints, and supported signature formats, through its well-known metadata endpoint.
6. Readiness checks verify metadata and blob durability, digest validation, authentication, authorization, and signature enforcement before the registry accepts publication or discovery traffic.

### Connect a Registry

1. An organization administrator submits the registry's HTTPS origin, optional read credentials for a private registry, and a registry trust policy binding trusted publisher identities and public keys to package IDs.
2. The ingress gateway authenticates the actor, removes untrusted identity headers, and calls the authorization service through `ext_authz`. The request proceeds only when the actor has the required registry-management permission.
3. The registry-verification service connects through the permitted egress path and reads the registry's protocol version, immutable `registryId`, API endpoints, catalog, and signature bundles. Remote content remains untrusted.
4. The control plane rejects a `registryId` already bound to a different origin or an origin already bound to a different `registryId`. It stores any read credentials through the secret-management service and persists the verified registry identity and initial registry trust policy revision.
5. After verification succeeds, the control plane marks the registry connected and advances the registry catalog generation. Packages become discoverable, but each release must still pass admission checks before installation.
6. The API returns the registry record and verification status. Failures preserve diagnostics without making the registry available for discovery or installation.

### Publish a Package Release

1. A developer or CI job authenticates directly to the registry with its own publishing credential and runs `vt build <dir> --push`.
2. `vt` validates the package metadata, builds the platform images and OCI image index, creates the canonical release manifest, and calculates its `releaseDigest`.
3. The registry authorizes the publisher to reserve `(packageId, releaseVersion)` and creates a recoverable publish-attempt ID. A version already bound to a different digest is rejected.
4. `vt` uploads content-addressed image blobs, the image index, and the release manifest. The registry recalculates digests and validates platform images and release contracts before making anything discoverable.
5. The publisher signs a statement containing the `registryId`, `packageId`, `releaseVersion`, `releaseDigest`, and image-index digest with an authorized KMS-backed key, then uploads the signature bundle.
6. The registry verifies the signature and confirms under its publisher policy that the signing key may publish the package. It then atomically binds the version to the digest and adds the release to the package catalog.
7. The registry returns the immutable `releaseDigest`. Failed or interrupted attempts remain non-installable and may be retried; publishing does not install or deploy the release.

### Upgrade a Package

1. An organization or package administrator selects a target `releaseDigest` for an installed package and submits a durable upgrade revision request with any approved identity or authorization changes.
2. The ingress gateway authenticates and authorizes the actor. The control plane verifies any required `service_account.assign` and `authorization.grant` permissions.
3. The control plane appends the revision request, verifies that no revision operation is active for the installation, and creates a generation-fenced revision operation. The API returns the operation ID so the client can observe progress asynchronously.
4. The revision controller verifies the recorded registry trust policy revision, release and image signatures, platform and runtime compatibility, schemas, dependencies, reverse dependents, workflows, and migrations against the recorded desired-state snapshot.
5. The controller runs declared prepare migrations, creates the immutable candidate Deployment and revision-specific Service, and applies ambient data-plane enrollment and the required `AuthorizationPolicy` attachments. The candidate initially receives no production traffic.
6. After readiness remains stable, the controller may canary operations whose contracts exactly match the promoted revision. Promotion then updates primary routing and waits for every currently live relevant ingress gateway and destination waypoint to remain synchronized with the target xDS generation for the configured stability window.
7. The callable catalogs advance only after routing converges. The previous revision drains pinned requests and streams while the controller observes the promoted revision and runs any separately approved finalization.
8. The controller records the terminal outcome. A pre-promotion failure leaves the previously promoted revision primary.

### Invoke a Package Operation

1. An external caller sends an MCP Streamable HTTP request to the deployment endpoint with a session credential or API grant, the target package and operation, and JSON arguments.
2. The ingress gateway authenticates the credential, removes untrusted internal-identity headers, applies request limits, and calls the authorization service through `ext_authz`.
3. The authorization service evaluates the credential scope, immediate caller workload identity, effective subject, target installation, canonical operation ID, approved package access policy, and any operation-specific resource against one policy generation. An unknown target or denied check fails before dispatch.
4. The gateway exchanges the external credential for a short-lived, signed internal invocation token bound to the target audience, package, and operation, then resolves the request to one callable revision. That resolution remains pinned for the lifetime of the invocation.
5. The mesh routes the request to the selected revision-specific Service.
6. The package supervisor dispatches the operation to the package runtime. The runtime returns one JSON-RPC response or opens an SSE stream; an open stream remains on the selected revision while that revision drains.
7. The response returns through the mesh and ingress gateway. The platform records the authorization decision, policy model ID and revision, actor, effective subject, immediate caller workload identity, target, `releaseDigest`, internal token ID, trace ID, duration, and outcome.

Package-to-package invocation does not pass through ingress. The calling package presents a platform-issued internal invocation token to the destination waypoint, which performs the corresponding workload and effective-subject authorization checks before revision routing and dispatch. Raw caller credentials are never forwarded.

## Assurance

### Retention and Audit

Release metadata, accepted revision history, and audit events are retained according to applicable policy. Registries and platform-owned artifact storage retain blobs required by active revisions or the configured rollback window. Unreferenced blobs may be garbage-collected after a grace period; shared OCI layers are deleted only after references are rechecked.

Authentication, authorization, registry, package-lifecycle, migration, secret-access, and network-policy events are written to the immutable audit sink with the actor, target, decision, relevant identifiers, trace ID, and outcome.

### Required Verification

Before production use, automated tests cover:

- **Registry and release integrity**: atomic publication under retries and concurrency, signature and compatibility rejection, and safe disconnection and garbage collection
- **Revision state machines**: validation failures, administrator, controller, and dependency races, restart at every phase, and migration idempotency and lease loss
- **Rollout and recovery**: canary and promotion failures, routing and catalog convergence, draining and SSE behavior, rollback approval, removal, tombstones, retained workflows, autoscaling, and mixed versions
- **Platform lifecycle**: bootstrap ordering and outages, protected identities and signers, ordinary-package replacement prevention, and independent infrastructure and system-package rollouts
- **Authorization models**: deterministic policy publication, active-grant preservation, subject-set cycles, and parity between batch and individual checks
- **Identity and delegation**: spoofing, claim validation, token expiration and rotation, `runAs`, confused-deputy prevention, attenuation, and revocation
- **Fail-closed enforcement**: authorization outages, data-plane bypass attempts, and ingress or waypoint `ext_authz` failures

## Glossary

**package identity**<br>
Permanent `(registryId, packageId)` for a registry package. Assigned when the package is reserved; never reused.

**release**<br>
Immutable, signed artifact for one `releaseVersion` of a package, identified by `releaseDigest`.

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
Makes a revision primary for routing; callable catalogs advance only after routing convergence.

**authorization model**<br>
Immutable, content-hashed definition of resource types, relations, actions, and allowed relationship targets used for one generation of authorization decisions.

**relationship tuple**<br>
An assignment of a subject, resource, or subject set to one relation on a resource.

**internal invocation token**<br>
Short-lived signed context that carries verified caller and delegation information and is bound to its intended audience, package, and operation.

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

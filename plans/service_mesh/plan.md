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
- Configure source automation to build and publish packages from GitHub branches, pull requests, or label-based rules.
- Configure read-only upstream registry sources and pull trusted releases into the private registry.
- Browse installed apps as a graph, including operations, UIs, workflows, dependencies, versions, identities, and authorization.
- Open an app administration page, change its version, or install and remove it.

### Package Publisher

A publisher can:

- Define package metadata, operations, dependencies, workflows, UIs, and migration jobs in a source manifest.
- Run `vt build <dir>` and optionally push the result directly to the deployment registry.
- Use repository automation instead of pushing manually.
- Run `vt dev --deployment <url> <dir>` to test a package in an isolated, user-scoped mesh sandbox before publishing it.

Migration jobs should be retry-safe and reversible where practical. Explicitly approved one-time jobs are supported when idempotency is impossible; their durable outcome prevents accidental re-execution.

### Package Administrator

A package administrator can:

- Inspect an app and its deployment details.
- Manage its desired version within their package-scoped authority.
- Invoke any app operation they are authorized to use.
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
- **Pull-through import**: an administrator imports an external release using read-only upstream authorization. The registry verifies and stores the complete release locally before exposing it.

### Build

Publishers create releases from a source directory with `vt build <dir>`. For example:

```text
g-issues/
├── package.yaml
├── runtime/
│   ├── Dockerfile
│   └── src/
├── migrations/
│   ├── v2.3.0/
│   └── v2.3.1/
└── ui/
    └── admin-console/
```

`package.yaml` defines the package name, version, operations, dependencies, workflows, UIs, migration jobs, and authorization requirements. Workflow definitions and operation schemas live in the migration jobs rather than separate source directories. `vt build g-issues/` validates the package, builds digest-addressed artifacts locally, and writes a logical release bundle.

### Example Release Bundle

A typical bundle is not one container or folder. For example:

```text
g-issues@2.3.1
├── release_manifest.json
├── runtime
│   └── image-index
│       ├── linux-amd64 image
│       └── linux-arm64 image
├── migrations
│   ├── v2.3.0 image
│   └── v2.3.1 image
└── ui
    └── admin-console.tar.gz
```

A UI-only or workflow-only package omits the runtime image.

```sh
vt build <dir> --push
```

#### CLI authentication

`vt build <dir>` without `--push` needs no deployment credentials.

`vt build <dir> --push` authenticates to the deployment registry before upload. The CLI resolves credentials in this order:

1. `VT_API_KEY` environment variable
2. A session stored in the OS keychain from a prior `vt auth login`
3. Interactive `vt auth login` if no credential is available

`--push` is safe to run idempotently: retrying the same release after interruption resumes or completes the existing attempt instead of creating a duplicate catalog entry. Pending and failed attempts are visible but never installable. Concurrent publishers cannot lose catalog updates. Stale attempts become failed, and orphaned content-addressed blobs are garbage-collected after a grace period.

### Stored Content and Backing Services

Google Artifact Registry stores the immutable release bundle: runtime and migration OCI images, UI bundles, workflow definitions, schemas, the canonical release manifest, and detached signatures. Each artifact is content-addressed by SHA-256 digest.

The registry service also tracks metadata that Artifact Registry does not provide on its own:

- Package IDs, slugs, version indexes, and tombstones
- Publication and import attempts, including pending and failed state
- Catalog indexes that map `(package, version)` to `releaseDigest`
- Trusted upstream registries and which publishers we accept from them
- Import history, including where a release came from and whether the copy completed

Upstream read credentials live in the secret-management service, and signing keys live in KMS; neither is stored with package artifacts.

### Identity and Trust

Every registry is responsible for permissioning who can pull from it and who can publish to it.

The deployment registry controls read access for discovery and download, and write access for authorized publishers. Upstream registries do the same on their side; pull-through import uses read-only credentials supplied for that one-time operation.

Each registry has a stable `registryId` derived from its root public key.

### Pull-through Imports

Upstream access is a one-time import that grants read access only; installation still requires organization trust in the release signature. An administrator import verifies the complete release, then atomically adds it to the local catalog; failed imports remain invisible. Imported releases keep their source identity, signatures, and digest.

## Publishing and Distribution

### Package Releases

Each `(registryId, packageId, releaseVersion)` has one canonical release manifest with detached signatures. Runtime and migration binaries may vary by platform; manifest contracts do not.

`release_manifest.json` must declare:

- Canonical registry and package IDs, slug, and `releaseVersion`
- Digest references for every artifact produced by `vt build`
- MCP operations with input and output schema digests
- Required and optional package and operation dependencies
- Ordered prepare, finalize, and compensating migration jobs
- Required authorization relationships or roles for each operation
- Supported MCP protocol versions
- Runtime startup instructions and probe contract for releases that include a runtime
- Optional source metadata (e.g. PR number)

#### Publication

1. `vt build` produces the release artifacts; each artifact gets a SHA-256 digest.
2. `vt build` writes `release_manifest.json` listing those digests and the release metadata above.
3. `vt build` computes `releaseDigest` as the SHA-256 hash of the canonical manifest bytes. It is not embedded in the manifest.
4. The publisher signs `registryId`, `packageId`, `releaseVersion`, `releaseDigest`, and every referenced artifact digest. The signature is stored separately from the manifest.
5. `--push` uploads the artifacts, manifest, and signature to the deployment registry.

#### Verification

When the registry receives a push request, it authenticates the publisher, verifies signatures, digests, and contracts, and rejects conflicting republications of the same `releaseVersion`. A `releaseVersion` follows a documented grammar and binds permanently to one `releaseDigest`. Only after those checks succeed does the registry expose the release in the catalog.

Pull-through import has its own validation before copying an external release locally; installation is a separate check that re-verifies the selected `releaseDigest` before admitting a package into the mesh.

### First Installation

A release in the registry catalog is not yet running in the mesh. The first installation admits it. For example, installing `g-issues@2.3.1`:

1. **Select a release.** A package administrator chooses the app and an immutable `releaseDigest` from the registry catalog.
2. **Review the contract.** The platform shows declared operations, dependencies, workflows, UIs, migrations, and required authorization relationships.
3. **Bind identities.** The administrator selects organization-managed service accounts for the runtime, migration jobs, and workflows when the release includes them.
4. **Configure operation authority.** For each operation, the administrator decides whether it runs only as the workload identity or may exercise delegated caller authority, and binds permissions as durable grants or delegated authority.
5. **Approve admission.** The administrator confirms the bindings. This requires `service_account.assign` for each identity and `authorization.grant` for each relationship or role being granted.
6. **Verify and prepare.** The control plane re-verifies the `releaseDigest`, resolves dependencies, reserves the package slot, runs prepare migrations, and provisions identities, mesh routes, and policies.
7. **Roll out.** The revision controller creates runtime workloads when needed, stages UI and workflow artifacts, and promotes the revision once readiness checks converge. The app then appears in user-facing catalogs and becomes invocable.

Releases without a runtime skip workload creation but still register workflows, UIs, and catalog entries through the same path. See [Installation](#installation) and [Revision Rollouts](#revision-rollouts) for the durable state machine and promotion rules.

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

Declared dependencies and workflow references are the authoritative compatibility boundary for operation removal. A candidate may remove an operation when no installed package or workflow definition or execution retained by policy references it, including after accounting for other revisions in the same atomic rollout group. External callers do not create an implicit dependency: callers using stale catalogs, cached operation names, or otherwise undeclared contracts may receive an operation-unavailable error after promotion.

Validation reads a revisioned desired-state snapshot and commits with generation preconditions. Independent packages may upgrade concurrently. Upgrades affecting the same dependency subgraph use deterministic locking or one atomic rollout group. Cyclic dependencies may be upgraded sequentially when every intermediate state is compatible. Mutually dependent changes require an atomic rollout group.

An atomic rollout group is one durable request containing candidates for multiple package slots, one desired-state snapshot, and one lock order. Admission validates compatibility across the complete before-and-after graph. Controllers may prepare, start, and canary members independently. All members' routing intents and external registration prerequisites converge first; promoted heads and package, operation, UI, and workflow catalog generations then commit in one generation-fenced state-store transaction. A pre-commit failure restores routing for every member and advances none of their promoted heads.

### Revision Rollouts

Releases with an MCP runtime use a Kubernetes-native blue/green rollout with optional Istio canary traffic. Each runtime upgrade creates a candidate revision in a distinct immutable Kubernetes Deployment behind a revision-specific Service. The promoted and candidate Deployments may run together, so the serving Deployment is never restarted or mutated in place. Runtime-less releases use the same durable revision state machine without creating runtime Deployments or Services.

Revision operations use optimistic concurrency and fencing tokens, so a controller that loses its lease cannot mutate a newer generation. Retries are idempotent, and every external mutation has a stable idempotency key.

For runtime releases, Kubernetes is the source of truth for live workload health and configured image identity. A runtime candidate is ready only when Kubernetes has observed the Deployment generation, its required replicas are available, each Pod specification is pinned to the expected platform-manifest digest resolved from the admitted image index, ambient enrollment and policy readiness gates are true, and runtime probes and synthetic MCP contract checks remain successful for a stability window. Replacement pods and autoscaled replicas count by current readiness, not persistent identity.

Before promotion, the interface service verifies and stages every UI bundle, while the workflow service registers each definition as candidate-only and acknowledges its digest. These records remain absent from user catalogs. A release without a runtime reaches Ready after artifact verification and applicable UI and workflow acknowledgements; it skips runtime resource creation, Canarying, runtime routing, Draining, and runtime observation.

Each installation with an MCP runtime has one stable frontend Service and revision-specific backend Services. Gateway API `HTTPRoute` resources establish the package route, while a protected waypoint cluster-selector extension applies the admitted operation-specific revision weights after MCP decoding and authorization. Callers never address a revision Service directly. Packages may call other packages only through declared dependencies and these mesh-managed routes. Direct or undeclared package-to-package calls are blocked.

The controller assigns each routing change a durable `routingGeneration` and records the exact Kubernetes resource UIDs, generations, waypoint cluster-selection configuration, and desired route and policy hashes that implement it. Infrastructure installs a protected routing-status extension in every ingress gateway and waypoint; the extension exposes, over an mTLS-protected administration endpoint, the routing generations and configuration hashes currently loaded by that proxy. This extension and endpoint are versioned with the pinned Istio/Envoy build and covered by infrastructure conformance tests.

The relevant data-plane set is a persisted membership snapshot of available ingress gateways, destination waypoints, and destination-node `ztunnel` instances selected for those resources. Convergence requires accepted and resolved Gateway API status, ready route backends, every gateway and waypoint report matching the desired hashes, ambient status confirming workload enrollment and policy generation on every relevant `ztunnel`, and authenticated allow-path and direct-bypass probes. The controller does not infer application convergence from one global Istio xDS version. Data-plane replacement, loss of availability, or membership change resets the stability window. A bounded timeout fails the phase and leaves enough recorded evidence for deterministic retry or restoration.

Gateway and waypoint readiness is gated on the routing-status extension reporting either the current committed generation or an active generation-fenced rollout generation authorized for that proxy and route. Destination workload readiness is separately gated on ambient enrollment and the current or fenced `ztunnel` policy generation. During promotion, the data plane may remain ready on the fenced candidate generation while convergence is observed. Immediately after the promotion commit, only the newly committed generation satisfies readiness. New or restarted gateways, waypoints, and `ztunnel` instances cannot enter traffic-serving membership until they report the then-current committed generation, so data-plane churn after promotion cannot reintroduce stale routing or bypass policy.

Promotion has one commit point. For runtime releases, the controller first writes primary routing intent while the previous revision remains the persisted promoted head. After routing converges and all UI-serving and workflow-registration acknowledgements remain valid for the stability window, one state-store transaction updates the promoted revision and the relevant routing, package, operation, UI, and workflow generations. Only that transaction constitutes successful promotion. If a prerequisite fails before the transaction, the controller restores previous routing and external registrations and confirms their convergence; it does not advance the promoted head or any catalog generation.

The control plane persists rollout intent and decisions, then reconstructs progress from Kubernetes, Istio, and protected platform services after restart. Historical rollout outcomes remain immutable; current workload health is a separate live view.

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

Inapplicable runtime, canary, drain, observation, and finalization work is skipped. Failure or cancellation is a terminal outcome attached to the stopping phase, not another progression phase.

Each phase has a timeout, retry policy, and terminal error taxonomy. Readiness requires the configured replica capacity, not a single ready pod. During routing propagation and draining, revisions follow the compatibility and operation-removal rules in [Dependencies and Contracts](#dependencies-and-contracts). The prior revision Service remains available for pinned sessions while new session routing targets only the promoted revision. One termination-grace interval before the hard drain deadline, the controller starts Pod deletion and publishes a protected draining cluster containing the terminating Pod endpoints. The waypoint cluster selector admits only valid pinned-session tokens to that cluster and does not depend on normal Service endpoint discovery retaining terminating endpoints. Kubernetes force-terminates any remaining process at the deadline, after which the controller removes the drain cluster and prior revision resources. Existing MCP SSE streams remain pinned to their original revision while the old revision drains, including streams for an operation omitted by the candidate.

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

Removal is a durable tombstoned revision, not catalog-row deletion. Admission rejects it while required reverse dependents exist unless the same atomic rollout group removes them. Preparing records workflow-retirement and data-retention decisions. Starting stages tombstone generations and candidate interface and workflow retirement changes; Ready confirms their acknowledgements. Promoting atomically commits package, operation, UI, and workflow tombstones, disables interface launches, unregisters workflows, stops new executions, and prevents new invocation resolution.

Draining applies the normal hard deadline to resolved runtime requests and streams; workflow executions follow their recorded finish, retire, or migrate policy. Finalizing removes runtime workloads, blocks stale ingress and package egress, revokes installation-owned bindings, assignments, and delegated tokens, and performs approved data retention or destruction. Complete preserves the tombstone and audit history; runtime-resource garbage collection follows the recovery window. Canarying and Observing are skipped.

Organization-owned grants survive removal. Reinstallation gets a new installation identity unless an approved restore reactivates the tombstone.

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

Each authorization grant wraps one relationship tuple and records an immutable ID, organization, human issuer, creation and optional expiration, issuance-authority snapshot, and policy revision. Grant mutation creates a new record; revocation appends a revocation record rather than rewriting or deleting history. A later loss of the issuer's authority does not revoke an existing durable grant; revocation, expiration, or an explicit policy migration does. Delegated authority is evaluated against the delegating subject's current authority and narrows immediately with it. Platform controllers may apply a recorded human-authorized grant but cannot originate one.

The first implementation does not cache authorization decisions. Caller tokens and internal caller contexts are short-lived, and every workflow step is authorized independently. Existing streams may continue until their configured lifetime expires. Security events go to an immutable audit sink outside package-controlled storage.

### Authorization Model and Evaluation

Authorization uses a provider-independent check protocol built around:

- A canonical **subject** being evaluated, such as the effective subject, immediate caller workload, or management actor
- An **action**, such as a package operation or administrative permission
- A canonical **resource**, identified by a reserved platform type or a package-namespaced type

The platform reserves resource types for registries, packages, releases, installations, revisions, workflows, identities, and authorization grants. Package-defined resource types are namespaced by canonical package identity and cannot use reserved names. Users and service accounts are subjects unless the requested action manages the identity itself.

A decision returns allow or deny together with the immutable policy model ID and revision used to evaluate it. A batch form evaluates multiple checks against the same policy snapshot and has the same semantics as individual checks; catalogs and controllers use it to avoid per-item authorization calls.

The authorization model defines resource types, relations, actions, and which relations satisfy each action. Relationship targets may be direct subjects, resources, or subject sets such as the members of a group. Subject-set traversal is cycle-safe. Package invocation uses the installation identity as the resource and the canonical operation ID as the action; package-specific checks may use a more specific package-owned resource.

The authorization service stores the policy model and organization-managed grants in one authoritative runtime system. Platform bootstrap registers reserved resource types, while package admission registers namespaced resource types and operation requirements. Each model change produces an immutable, content-hashed generation. Grants may be revoked at any time. A policy-model change cannot remove or rename a resource type or relation referenced by active grants unless those grants are revoked or migrated in the same transaction. Grants are indexed by subject, relation, and resource so evaluation does not scan the complete relationship set.

Credential scopes are an upper bound on authority: a relationship grant cannot restore access excluded by the authenticated credential. Missing subjects, actions, resources, or policy models—and authorization-provider outages—produce a denial.

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

Private interfaces use an origin-scoped session established through the normal external identity flow. The interface service issues a short-lived browser capability only when the request originates from the currently promoted generation's verified entry document. The capability names the installation, interface ID, UI generation, allowed operations, and exact generation-specific origin. Ingress validates that capability and the `Origin` header, rejects stale generations, and carries its signed allowlist claims into the caller token. After decoding the MCP request, the destination waypoint rejects browser operations outside that allowlist in addition to performing normal subject authorization. A public interface must be explicitly allowed by organization policy, and public asset access does not make its operations public. Browser code never receives workload credentials, caller tokens, or internal caller contexts.

The platform applies the declared CSP profile, strict CORS, MIME validation, integrity metadata, response-size limits, immutable caching for revision-qualified assets, and service-worker scope restrictions. Interfaces requiring embedding use opaque sandboxed frames and explicit `postMessage` schemas; embedding does not grant same-origin access.

### Catalogs and Administration

An organization deployment exposes package, operation, UI, and workflow catalogs; a dependency graph; revision, rollout, migration, identity, and authorization administration; API-grant management; and a development sandbox.

Administrative catalogs may expose candidate metadata. User-facing package, operation, UI, and workflow catalogs expose only their atomically promoted generations. New operation contracts appear only after applicable routing, UI-serving, and workflow-registration prerequisites in [Revision Rollouts](#revision-rollouts) have converged.

Clients treat catalogs as snapshots. Invocation remains authoritative and may reject stale or undeclared operations under the rules in [Dependencies and Contracts](#dependencies-and-contracts).

Organization administrators can configure upstream pull sources, authorize imports, and install, upgrade, roll back, or remove packages. Package administrators can perform revision and administration actions within their delegated package and authorization scope. Package developers with registry write access can build, publish, test, and invoke packages. Package users see only catalogs and operations authorized for them.

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

The deployment exposes package-scoped MCP endpoints rather than one endpoint that multiplexes package names from JSON-RPC bodies. A protected MCP extension shared by destination waypoints validates the caller token and request-envelope hash, parses the JSON-RPC envelope, resolves the request class and canonical operation ID, and writes trusted dynamic metadata for `ext_authz`, routing, canary selection, and telemetry. Callers cannot supply that metadata. Malformed JSON-RPC, unsupported methods, unknown operations, mismatched hashes, and expired tokens fail at the waypoint before reaching a package.

MCP methods map to authorization and routing as follows:

- `initialize`, `ping`, and session negotiation require the installation-level `package.connect` action and do not select an operation.
- Catalog and discovery methods terminate at a protected catalog service behind the waypoint. They require their catalog-read action, use a batch authorization check against one policy snapshot, and return only entries authorized for the effective subject; untrusted package runtimes do not filter their own discoverability.
- Operation calls such as `tools/call` resolve the package-stable operation ID from validated parameters and require that operation's action and resource checks.
- Cancellation, progress, and other session notifications must reference an existing authorized session and inherit its installation, revision, actor, and effective subject. They cannot change authorization scope.
- Unsupported methods fail closed. A JSON-RPC batch is accepted only when every item has the same target installation, request class, operation, authorization scope, session, and routing target and each item is authorized independently; otherwise the complete batch is rejected.

The waypoint sends verified caller context and decoded request metadata to authorization. On allow, the adapter returns short-lived signed internal context carrying the actor, effective subject, applicable immediate workload, and delegation chain. It is bound to the target installation, request class, applicable operation, envelope hash, applied credential and delegation bounds, applicable browser claims, policy generation, and trace ID. The waypoint removes the external token and reserved fields. Catalog requests terminate at the protected catalog service; runtime requests select a revision Service and forward only the internal context.

After `ext_authz`, a protected metadata-aware cluster-selector extension uses only decoder-generated metadata and the signed routing table for the committed or active fenced generation to select operation-specific canary backends for runtime request classes. Catalog and discovery classes bypass revision selection and terminate at the catalog service. No route trusts a caller-supplied operation header, and standard `HTTPRoute` header matching is not used for decoded operations.

For a new session, the waypoint wraps the runtime-issued opaque session ID in a client-opaque signed routing token containing the installation, revision, `releaseDigest`, routing generation, issued and expiry times, and random nonce. On each successful continuation or response, the waypoint may rotate the wrapper without extending it beyond the configured maximum session lifetime or revision drain deadline. The waypoint verifies and unwraps continuations before routing to their named revision Service. Cancellation is forwarded to that revision, and reconnect succeeds only while the wrapper is valid and the revision remains inside its drain deadline. SSE streams have a configured maximum lifetime and remain pinned until completion, cancellation, or that deadline.

Ingress authenticates external credentials, creates package caller tokens, and authorizes catalogs and management APIs; it does not authorize MCP operations. The destination waypoint decodes and authorizes every MCP request before revision routing. Both use one authorization service and relationship graph.

Checks use the effective subject, target installation, and request-class action. Package-originated calls also require the immediate workload's authority; operation calls additionally require the canonical operation ID, approved package access policy, and any package-specific resource authorization. Credential scope and every applicable relationship check must allow the request. Decisions record the policy model and revision; missing or unknown identities, installations, request classes, operations, or policies—and provider outages—deny access.

For package-to-package calls, the caller asks the identity service to mint a caller token over its authenticated workload channel and presents the signed parent internal caller context or a platform-issued workload-execution context. The request names one declared dependency and request-envelope hash; it cannot supply a replacement actor, effective subject, or delegation chain. The identity service verifies the parent context, preserves its actor and effective subject, appends the authenticated caller workload to the delegation chain, and cryptographically attenuates scope and expiry. The destination waypoint remains authoritative for target operation authorization. Tokens are single-target, short-lived, non-refreshable by the package, and cannot broaden delegated authority. Package SDKs perform this exchange and send the resulting token to the destination Service.

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

- **Identity service**: authenticates sessions and API grants, resolves canonical principals, manages organization service accounts, issues caller tokens, and manages signing keys for internal caller contexts and session wrappers.
- **Authorization service**: stores immutable policy models, grants, assignments, and revocations and evaluates provider-independent and Envoy `ext_authz` checks.
- **Secret-management service**: stores and rotates platform and connection credentials and releases secret material only to explicitly authorized workload identities.
- **Registry-verification service**: verifies the deployment registry and upstream source identities, controls pull-through imports, verifies manifests, digests, signatures, platform compatibility, trust policy, and local artifact completeness before catalog exposure and admission.
- **Audit sink**: accepts append-only security, authorization, administration, and runtime events in storage that packages cannot control.
- **Workflow service**: registers promoted package-defined workflows and runs pinned executions outside package runtimes.
- **Control-plane state store**: a bootstrap-substrate service that durably stores deployment-registry state, upstream source configurations, import attempts, package slots, revision requests and operations, routing and catalog generations, migration outcomes, and recovery metadata.

The Istio control plane provides the certificate authority and workload-certificate lifecycle. Backup and recovery tooling protects control-plane state and trusted-service configuration but does not sit on the request path.

Each trusted service exposes a versioned typed API. Control-plane clients use shared libraries for workload authentication, canonical caller context, deadlines, bounded retries, idempotency keys, metrics, tracing, and audit metadata. Istio provides service discovery, mTLS, and network routing, so the first implementation does not add a generic control-plane proxy.

### Runtime Architecture

Production uses Istio ambient mode. `ztunnel` provides L4 transport security and workload identity, while waypoint proxies provide L7 routing, authorization, and telemetry. Package Pods have no mesh or application proxy sidecar.

The runtime combines `istiod`, mandatory node-level `ztunnel`, shared destination waypoints, ingress and egress gateways, unprivileged package containers, the protected services above, the state store, and revision controllers. Package containers expose MCP and health endpoints directly; the interface service serves static UIs.

Revision controllers reconcile durable state through trusted-service and Kubernetes APIs into Deployments, stable and revision Services, waypoint enrollment, Gateway API routes, signed cluster-selection configuration, and `AuthorizationPolicy`. `istiod` distributes the resulting xDS configuration. Controllers advance catalog generations only after protected status reports and allow-path and bypass probes confirm route, selector, enrollment, and policy convergence.

Istio owns endpoint, routing, certificate, and mesh-policy distribution—not revision state, grants, workflows, or catalogs. Ingress authenticates external callers; destination waypoints authorize MCP requests.

Because `ztunnel` cannot enforce L7 policy, ingress handles management checks while the destination waypoint decodes and authorizes every MCP request before revision selection. Every stable package Service is waypoint-enrolled, and ingress traffic traverses that waypoint. `AuthorizationPolicy` permits application-port traffic only from its authenticated identity, including requests addressed directly to a Pod IP or revision Service. Health probes use a separate protected port with the minimum exception.

A readiness controller keeps new `ztunnel` instances and affected workloads out of service until the current policy generation is loaded. Admission requires accepted waypoint enrollment, ingress waypoint use, bypass prevention, MCP decoding and authorization extensions, routes, and policy attachments.

The external authenticator, caller-token exchange, waypoint MCP decoder, authorization adapter, metadata-aware cluster selector, session wrapper, response observer, audit adapter, and routing-status reporter are protected extensions pinned by infrastructure configuration. The waypoint decoder executes before `ext_authz`, removes all external copies of reserved fields, and produces trusted dynamic metadata. After authorization, the cluster selector uses that metadata and signed generation-fenced configuration to choose a revision backend; standard `HTTPRoute` header matching is not used for decoded operations.

Production uses `ztunnel` for transport security and L4 authorization, `NetworkPolicy` for reachability, ingress `ext_authz` for management authorization, and destination-waypoint `ext_authz` for MCP authorization. Authorization fails closed; health ports have no public ingress.

Waypoints emit L7 request rate, transport and MCP outcome, duration, response-size, route, operation, source and destination identity, selected backend, access-log, and trace telemetry. The protected response observer distinguishes HTTP transport results from JSON-RPC errors and terminal SSE outcomes without exposing response bodies. `ztunnel` emits L4 connection, byte, policy-denial, and bypass telemetry. Kubernetes reports Pod health, restarts, resources, and rollout state, while package instrumentation reports application-internal metrics and trace spans. Application metrics are scraped separately rather than merged through a per-Pod proxy.

The waypoint audit adapter durably records malformed requests, token and envelope-hash failures, authorization denials, routing-generation mismatches, and session-token failures before returning an error. `ztunnel` policy denials flow through the protected audit pipeline. If a required security event cannot be delivered or durably buffered, the affected request fails closed.

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

Protected system packages roll independently through the revision controller and do not require a mesh redeployment. Ordinary package revisions, upstream source and registry-policy changes, identity assignments, and authorization-binding updates likewise change runtime or control-plane state without redeploying the mesh.

Bootstrap-substrate changes, including control-plane state migrations and backup or recovery tooling, use infrastructure rollouts but do not redeploy the mesh unless they also change an Istio item listed above. Package releases declare supported MCP versions. Infrastructure pins Kubernetes, Istio, Envoy, the shared MCP extension, session-token format, authorization adapter, and routing-status extension versions and accounts for overlapping old and new versions before admitting releases that require new features. Registry trust-root changes remain separately authorized and audited.

## Representative Request Lifecycles

These summaries add sequencing and externally observable behavior; preceding sections define the authoritative mechanics.

### Provision the Deployment Registry

Deployment provisioning creates one private registry with OCI 1.1 storage, a self-certifying `registryId`, publisher controls, and pull-through import. It publishes the signed descriptor and verifies storage, digest, authentication, authorization, and signature enforcement before serving traffic. Publisher private keys remain publisher-controlled.

### Pull a Package Release

An administrator selects an external release, read-only authorization, and trust policy. The deployment registry verifies the source identity and complete release, copies its content into local storage, and atomically exposes it. The API returns the import status and `releaseDigest`; failures retain diagnostics but expose no installable release.

### Publish a Package Release

A developer or CI job authenticates to the deployment registry and runs `vt build <dir> --push`. The registry validates and atomically publishes the signed release, then returns its immutable `releaseDigest`. Interrupted attempts remain non-installable, and publishing never installs the release.

### Upgrade a Package

An authorized administrator submits a durable request naming the target `releaseDigest` and approved identity or authorization changes. The control plane verifies permissions, appends the request, fences the operation, and returns its ID. Candidates remain absent from user catalogs and receive no primary traffic before promotion; operations with identical contracts may receive controlled canary traffic. Promotion uses the convergence-gated transaction above, and pre-promotion failure leaves the prior revision primary.

### Invoke a Package Operation

An external caller sends MCP Streamable HTTP to a package-scoped endpoint. Ingress authenticates, strips untrusted metadata, applies transport limits, and issues a caller token bound to the installation and envelope hash; it neither parses MCP nor authorizes an operation. The waypoint validates the token, decodes and authorizes the request, removes the caller token, adds signed internal context, selects the revision, and pins sessions through its signed wrapper. The runtime receives the request directly. Audit records include the decision and policy generation, actor, effective subject, immediate caller workload when applicable, target, `releaseDigest`, caller-token ID, trace ID, duration, and outcome.

Package-to-package calls bypass ingress but still terminate at the destination waypoint. The identity service derives an attenuated, single-target token from authenticated workload context; raw caller credentials are never forwarded.

## Assurance

### Retention and Audit

Retention follows active-revision, rollback, recovery, and garbage-collection rules above; shared OCI layers are deleted only after references are rechecked. Authentication, authorization, registry, lifecycle, migration, secret-access, and network-policy events go to the immutable audit sink with actor, target, decision, identifiers, trace ID, and outcome.

### Required Verification

Before production use, automated tests cover:

- **Registry and release integrity**: one-registry-per-deployment enforcement, least-privilege push and upstream read authorization, atomic publication and pull-through import under retries and concurrency, preservation of source identity, complete local artifact closure, signature and compatibility rejection, upstream-source disablement, and safe garbage collection
- **Runtime contract**: direct MCP and health endpoints, platform-manifest-pinned Pods, synthetic contract readiness, Kubernetes shutdown, waypoint request and response decoding, token upper bounds, protected catalog filtering, cancellation, session rotation and pinning, SSE draining, and malformed or ambiguous MCP requests
- **Revision state machines**: validation failures, administrator, controller, and dependency races, restart at every phase, and migration idempotency and lease loss
- **Rollout and recovery**: canary and promotion failures before and after routing intent, proxy-membership churn, route restoration, atomic rollout groups, catalog convergence, draining and SSE behavior, rollback approval, removal, tombstones, retained workflows, autoscaling, and mixed versions
- **Platform lifecycle**: bootstrap ordering and outages, protected identities and signers, ordinary-package replacement prevention, and independent infrastructure and system-package rollouts
- **Authorization models**: deterministic policy publication, active-grant preservation, subject-set cycles, and parity between batch and individual checks
- **Identity and delegation**: spoofing, claim validation, token expiration and rotation, `runAs`, confused-deputy prevention, attenuation, and revocation
- **Fail-closed enforcement**: authorization and audit outages, direct Pod-IP and revision-Service bypass attempts, `ztunnel` and waypoint churn, stale policy generations, and ingress or waypoint `ext_authz` failures
- **Interfaces and workflows**: route collisions, CSP and asset validation, revision-qualified caching, pinned workflow executions, signals and timers during upgrades, retirement, migration, and forced termination
- **Disaster recovery**: regional state restore, route reconstruction, signing-key and certificate recovery, protected-service recovery, audit continuity, and break-glass controls

## Glossary

| Term | Meaning |
| --- | --- |
| Package identity | Permanent `(registryId, packageId)`; never reused |
| Pull-through import | Verified, atomic copy of an external release and its complete artifact closure into the deployment registry using read-only upstream authorization |
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

## Appendix: Design Invariants

The following invariants apply to every implementation:

1. Published releases are immutable and digest-addressed.
2. Revision intent and outcomes are append-only.
3. Each package slot permits one active fenced revision operation.
4. Controllers reconcile durable intent idempotently and resume after failure.
5. Lifecycle state, workload observations, routing convergence, and catalog convergence remain distinct.
6. Only successful promotion changes the promoted head.
7. Every traffic-eligible revision satisfies the compatibility and removal rules in [Dependencies and Contracts](#dependencies-and-contracts).
8. Organization-managed identities are the sole source of runtime authority.
9. Ordinary package APIs cannot replace the bootstrap substrate or protected platform capabilities.
10. External artifacts remain untrusted until admission verifies their digests, signatures, schemas, and policy.
11. Authorization uses canonical identities and one immutable policy generation; policy changes cannot silently delete or orphan grants.
12. Package runtimes receive no raw credentials or caller-supplied identity, only short-lived, signed, audience-bound caller context.

## Appendix: Foundations

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

Authentication resolves credentials to canonical principals before authorization. Downstream services accept identity only from verified workload identity or signed context; delegation preserves actor and effective subject through authorization and audit.

Package IDs are never reused; slug renames retain aliases and tombstones. A different registry yields a different package identity even for identical bytes. Release metadata cannot name workload identities or Kubernetes service accounts; installation selects them from organization-managed resources.

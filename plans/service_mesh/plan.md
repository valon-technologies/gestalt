# Service Mesh Design

## Contents

- [Overview](#overview)
- [Design Invariants](#design-invariants)
- [Model](#identity-model)
- [Publishing](#package-releases)
- [Revisions](#durable-deployment-state)
- [Runtime](#workflows)
- [Authorization](#authentication-and-authorization)
- [Implementation](#runtime-architecture)
- [Assurance](#retention-and-audit)
- [Glossary](#glossary)

## Overview

The platform builds, distributes, installs, and runs packages in a shared service mesh. A package release may include:

- An MCP server and its operations
- One or more single-page user interfaces
- Durable workflow definitions and workers
- Dependency and migration metadata
- Runtime, configuration, and authorization requirements

An organization operates one deployment backed by a private registry and, optionally, additional trusted registries. The deployment exposes catalogs of installed packages, callable operations, user interfaces, workflows, and dependencies.

Publishing makes a release discoverable. Installation admits an immutable release into an organization. Canarying permits controlled, undiscoverable traffic, while promotion makes an installed revision primary and user-discoverable. These states are separate and must not be inferred from one another.

## Design Invariants

The following invariants apply to every implementation:

1. A published release is immutable and identified by its `releaseDigest`.
2. A deployment records every accepted install, upgrade, rollback, and removal request in an append-only revision history.
3. For each canonical package identity in an organization, only one revision operation — install, upgrade, rollback, or removal — may be in progress at a time.
4. Durable revision requests record intent; revision controllers perform runtime work and resume safely after process failure.
5. Published, admitted, materialized, running, ready, canary-callable, promoted, and converged are distinct states.
6. A pre-promotion failure does not replace the last successfully promoted revision.
7. Routing propagation is eventually consistent, so every revision that may receive traffic remains contract-compatible until propagation and draining finish. New contracts become visible only after routing convergence.
8. Packages run only under organization-provisioned identities; requested capabilities require explicit grant or delegation and cannot be self-asserted by release metadata.
9. Dynamic services cannot replace the identity, authorization, certificate, secret, registry-verification, audit, state-storage, or recovery components needed to administer the mesh.
10. Every externally supplied manifest, image, schema, migration, and attestation is untrusted until its digest, signature, and policy are verified.

## Identity Model

The system keeps these identities separate:

- **Registry identity**: immutable `registryId` plus its configured origin and trust-policy digest.
- **Package identity**: permanent `(registryId, packageId)` assigned when the package is reserved. A slug is a mutable display and discovery alias, never a security identifier.
- **Release identity**: `(registryId, packageId, releaseVersion, releaseDigest)`.
- **Installation identity**: an organization-scoped UUID representing one installed package.
- **Workload identity**: an orchestrator-owned SPIFFE identity for one installation's runtime or migration job.
- **Workflow identity**: the principal assigned to a workflow definition or execution.
- **Actor identity**: the user, API grant, service account, or workflow that initiated an action.
- **Effective subject**: the principal whose delegated authority an invocation exercises.

Package IDs are never reused. Slug renames retain aliases and tombstones. Mirroring a package preserves its identity only through a signed mirror or transfer relationship; otherwise importing it creates a distinct package identity.

Package authors cannot select workload identities. The orchestrator binds installations to Kubernetes service accounts and short-lived SPIFFE certificates. Trust domain, certificate lifetime, rotation, and compromise revocation are deployment policy.

## Package Releases

Each `releaseVersion` has one platform-independent, signed release manifest. The release manifest is stored as an OCI artifact or another digest-addressed immutable object and references an OCI image index digest. The image index selects immutable image manifests for supported `os`, `architecture`, and optional `variant` tuples.

Platform images may contain different binaries, but their operation contracts, dependencies, migrations, workflows, and requested capabilities must match the release manifest. A platform-specific release contract is not allowed.

The release manifest contains:

- Canonical registry and package IDs, slug, and `releaseVersion`
- OCI image-index digest and supported platforms
- Instructions for starting the MCP server, interfaces, and workflow workers
- MCP endpoint, operations, and input/output schema digests
- Included workflows and user interfaces
- Required and optional package and operation dependencies
- Ordered prepare, finalize, and compensating migration jobs
- Rollback classification
- Requested service-account, authorization, ingress, and egress capabilities
- Configuration and typed secret requirements
- Startup, liveness, and readiness endpoints
- Minimum and maximum supported `gestaltd` and mesh protocol versions
- Source repository and revision
- Subjects for detached signatures and build-provenance attestations

The manifest does not contain self-asserted signatures or provenance. Signatures and SLSA/in-toto provenance are detached attestations over the release and image digests, such as OCI referrers.

The `releaseDigest` is computed over the canonical manifest bytes and is carried by catalog entries, signatures, attestations, and installation records rather than by the hashed manifest itself.

`releaseVersion` values follow a registry-defined, documented grammar and comparison order. A `(registryId, packageId, releaseVersion)` binds permanently to one `releaseDigest`. A conflicting republish is rejected. An identical retry is idempotent and does not rewrite publication time or provenance. Mutable tags may aid discovery but are never installation inputs.

## Registries

Registries control who can publish, discover, or download releases. Each configured registry has:

- An immutable `registryId`
- Discovery, metadata, blob, and publication endpoints
- Authentication and authorization scopes
- Trusted signing identities and builders
- Allowed package namespaces
- Source repository and workflow constraints
- Required provenance level
- Whether direct developer publication is permitted
- A versioned trust-policy digest

Connecting a registry does not trust every publisher in it. Catalogs and manifests remain untrusted until admission verifies their immutable digests, signatures, provenance, schema versions, and the deployment's registry policy.

The registry interface defines pagination, conditional reads, consistency guarantees, error codes, retries, rate limits, and authentication separately for catalog discovery, immutable metadata, blob pulls, and publication.

A catalog contains paginated package and release summaries pointing to authoritative release-manifest digests. It does not duplicate the full release contract. Per-package indexes use monotonic revisions and compare-and-swap updates.

Disconnecting a registry blocks new discovery and installation but preserves its identity and all historical records. Hard removal is rejected while any desired, running, redeployable, dependent, or in-progress revision references it. Rebinding to a mirror requires digest-equivalent content and signatures trusted by the deployment.

## Build and Publish

`vt build <dir>` validates source metadata and builds an OCI image index and release manifest. The build records resolved source and material digests. Production publication requires attestations from a trusted builder; direct publication must satisfy the same policy.

Publication is a recoverable transaction:

1. Reserve `(packageId, releaseVersion)` and create a publish-attempt ID.
2. Record a pending release with source and workflow provenance.
3. Upload content-addressed image blobs and manifests.
4. Validate all platform images and release contracts server-side.
5. Create the immutable release manifest.
6. Attach signatures and provenance attestations.
7. Commit availability by compare-and-swap updating the package catalog last.
8. Mark the attempt succeeded or failed.

Pending and failed attempts are visible but never installable. Concurrent publishers cannot lose catalog updates. Stale attempts become failed, and orphaned content-addressed blobs are garbage-collected after a grace period.

## Durable Deployment State

The control plane persists:

- **Package slots**: one organization-scoped slot keyed by canonical package identity, reserved before allocating an installation ID and used to fence concurrent first installs
- **Revision requests**: append-only install, upgrade, rollback, and removal intent with actor, previous promoted revision, target `releaseDigest`, approved capabilities, resolved dependencies, and timestamps
- **Installation head**: last successfully promoted revision and current admitted candidate
- **Revision operation**: one generation-fenced state machine per installation
- **Migration outcomes**: immutable status keyed by installation, `releaseDigest`, and migration ID
- **Workload observations**: materialized, started, ready, draining, stopped, and error state for each target replica
- **Routing and catalog generation**: the promoted revision visible to callers
- **Terminal outcomes**: completion or failure phase, duration, and reason

Revision records use optimistic concurrency and fencing tokens. A stale controller cannot mutate a newer generation after losing its lease. Retries are idempotent, and every external mutation has a stable idempotency key.

Each rollout targets one immutable runtime cohort identified by the infrastructure generation and `gestaltd` supervisor generation. Replicas enroll during a bounded epoch; the nonempty cohort is then frozen for promotion accounting. Scaling replicas that join later still converge but do not retroactively change the completed cohort. Observations include the rollout generation and enrollment epoch so stale or overlapping infrastructure cohorts cannot satisfy a newer rollout.

Routing convergence uses a separately frozen proxy cohort. In sidecar mode it contains every live ingress gateway plus the live sidecars of declared reverse-dependent caller workloads at the promotion epoch. `istiod` xDS status supplies proxy membership and acknowledgement of the target routing generation. A reverse-dependent replica that starts later cannot become ready until it receives at least that generation. Undeclared direct east-west invocation is denied, so it cannot bypass this cohort. Ambient mode must define the equivalent gateway, waypoint, and `ztunnel` cohort before it is supported.

The runtime distinguishes:

- **Published**: committed in a registry
- **Admitted**: verified and accepted as a candidate by the organization
- **Materialized**: image and immutable contract are available to a workload node
- **Running**: the candidate process started
- **Ready**: the candidate passes readiness and minimum healthy-duration checks
- **Canary-callable**: an undiscoverable candidate receives a controlled subset of traffic for operations whose contracts exactly match the promoted revision
- **Promoted**: routing selects the candidate as primary
- **Converged**: the required workload cohort observes the promoted generation

## Installation

Publishing does not install a package. An organization administrator selects an immutable `releaseDigest` and binds:

- Organization-owned package and workflow identities
- Package-requested operations and capabilities to approved authorization relationships
- Caller-delegation policy per operation
- Typed configuration values and secret references
- Ingress and egress requests to organization policy
- Initial replica, resource, and availability settings

The platform shows the requested-versus-approved capability diff before admission. An installer can use only identities for which they have `can_use` or `can_delegate`, and cannot convert temporary authority into an unrestricted persistent package grant. Sensitive capabilities may require a second approver.

Installation is a reconciled saga:

1. Verify registry trust, release and image signatures, provenance, platform support, runtime compatibility, and schema versions.
2. Resolve exact dependency revisions and operation-contract digests.
3. Reserve the organization package slot and persist the revision request, approved capability snapshot, and fenced revision operation.
4. Provision the installation identity, workload identity, policy, configuration, and secret bindings.
5. Materialize and start the initial workload without making it callable.
6. Require readiness quorum and minimum healthy duration.
7. Promote routing, wait for the frozen proxy cohort to acknowledge the routing generation, and drain any prior compatible route.
8. Advance the callable catalog only after routing converges, then record convergence and the terminal outcome.

Failures preserve the durable phase and diagnostics. The revision controller retries retryable phases within policy and compensates unpromoted resources when an administrator cancels the installation. A package is not callable until promotion succeeds.

## Dependencies and Contracts

Dependencies use canonical package identities, `releaseVersion` ranges, and required operation contract digests. Dependencies may be required or optional. Admission records the exact revisions and contract digests selected.

The platform validates:

- Every required dependency has a promoted compatible revision
- Required operations exist
- Required input and output contracts match
- The candidate remains compatible with existing reverse dependents
- No conflicting revision operation is active in the affected dependency subgraph
- Workflow calls reference compatible operation contracts

JSON Schemas use one pinned dialect and canonical encoding before hashing. The first implementation requires exact schema digests. Semantic compatibility can be introduced only with documented contravariant input and covariant output rules.

Validation reads a revisioned desired-state snapshot and commits with generation preconditions. For independent packages, upgrades may proceed concurrently. Upgrades that affect the same dependency subgraph use deterministic locking or one atomic multi-package rollout plan. Dependency cycles are rejected until atomic rollout groups are implemented.

## Upgrades and Traffic Promotion

An upgrade never mutates the running revision in place. It creates a candidate generation while the last promoted revision continues serving.

The rollout state machine is:

1. **Admitted**: verify the release, policy, runtime compatibility, dependencies, reverse dependents, and rollback classification.
2. **Staging**: pull and verify images for the target cohort.
3. **Preparing**: run backward-compatible prepare migrations as fenced one-shot jobs.
4. **Starting**: start candidate workloads with no production traffic.
5. **Ready**: require a readiness quorum and minimum healthy duration.
6. **Canarying**: route a configurable small traffic percentage for operations whose contracts exactly match the promoted revision; the candidate remains absent from user-facing catalogs.
7. **Promoting**: make the candidate primary, wait for the frozen proxy cohort to acknowledge the routing generation, and only then expose newly added contracts in callable catalogs. Promotion is the boundary after which the candidate is the last successfully promoted revision.
8. **Draining**: keep the prior revision available for in-flight requests and streams until their drain deadline.
9. **Soaking**: observe the promoted revision for a configured period.
10. **Finalizing**: run irreversible contract/finalize migrations only after the rollback window when required.
11. **Complete**: record convergence and terminal timing.

Each phase has a timeout, retry policy, and terminal error taxonomy. Promotion requires nonempty frozen runtime and proxy cohorts and cannot be inferred from one ready instance. Routing changes define weight propagation, proxy acknowledgement, readiness quorum, connection draining, and behavior for MCP SSE streams. During partial routing propagation, both revisions support the promoted operation-contract digests.

Failures before promotion produce a failed candidate and leave the prior promoted revision primary. Failures during soak after promotion mark the new revision degraded and trigger rollback when its rollback class permits; otherwise they require an audited forward fix. Finalization failure does not relabel the promoted revision as a failed candidate or route traffic implicitly: it records `promoted_with_finalization_error`, blocks the next revision operation, and requires retry or an approved forward fix.

The availability goal is zero planned downtime for packages that satisfy the compatibility contract. Any supported operation that requires downtime declares and receives explicit administrator approval before admission; a vague downtime target is not sufficient.

## Migrations and Rollback

Migrations are separately identified, digest-pinned jobs. Each declares:

- Migration ID and `releaseDigest`
- Prepare, finalize, or compensating phase
- Dedicated workload identity and requested capabilities
- Required secrets and network destinations
- Timeout, resource limits, retry policy, and idempotency key
- Compatibility with the prior and candidate package revisions

Migration jobs run with least privilege and deny-by-default egress. They never run as the installer, revision controller, or normal package identity. Attempt and completion records are durable.

Each release declares one rollback class:

- **Traffic-only**: the previous revision can serve against the current data model
- **Code and compatible data**: old code and schema remain mutually compatible
- **Compensating migration required**: rollback requires a declared, verified job
- **No post-promotion rollback**: an explicitly approved release cannot return traffic to the prior revision after promotion

Prepare migrations follow expand/contract rules and must remain compatible with the serving revision. Releases with a rollback-capable class defer destructive finalize migrations until the rollback window closes. A no-post-promotion-rollback release requires explicit approval before promotion and has no rollback window. A rollback command is not proof that data rollback is safe.

Administrators initiate rollback through the same revision controller. Manual shell execution cannot update revision state. Automatic rollback may later invoke the same audited state machine when canary or soak gates fail.

## Removal

Removal is a durable tombstoned revision, not deletion of a catalog row:

1. Reject removal while required reverse dependents exist, unless they are removed in the same atomic plan.
2. Promote a tombstone generation that removes callable catalog entries and prevents new invocation resolution.
3. Drain already-resolved requests, streams, workers, and queued work according to policy.
4. Stop workloads and block ingress and egress.
5. Revoke workload certificates, installation-owned assignments, delegated tokens, secret bindings, and runtime configuration.
6. Apply package-data retention or destruction policy.
7. Preserve the package tombstone and audit history.
8. Garbage-collect runtime resources after the recovery window.

Organization-owned persistent grants are not silently deleted; the removal UI distinguishes them from installation-owned grants. Reinstallation creates a new installation identity unless an explicit restore operation reactivates the tombstone within policy.

## Workflows

Workflow definitions are immutable and versioned independently from package processes. Each execution records its workflow-definition digest, actor, workflow identity, and compatible package-operation contract revisions.

Existing executions continue on workers that support their pinned definition until they complete, migrate, or reach a documented retirement policy. New executions use the promoted workflow catalog generation. Removal and incompatible upgrades must account for queued work, retries, timers, and long-running executions before workers are drained.

Every workflow operation is authorized at execution time. `can_run_as` is required to select an organization-managed workflow identity. The original triggering actor remains in the audit and delegation chain.

## Catalogs and Administration

An organization deployment provides:

- A package catalog
- An operation catalog
- A user-interface catalog
- A workflow catalog
- A dependency graph
- Package revisions, rollout and migration status, identities, and authorization settings
- API-grant management
- A development sandbox

Candidate metadata may be visible to administrators, but user-facing callable catalogs expose only contracts safe for the currently propagated routes. Routing is updated first because xDS propagation is asynchronous; newly added contracts become discoverable only after the frozen proxy cohort acknowledges the routing generation. Both revisions remain contract-compatible until old routes drain. Each invocation, including an SSE stream, resolves once and remains pinned to one package revision.

Organization administrators can connect registries and install, upgrade, roll back, or remove packages. Package developers can build, publish, test, and invoke packages. Package users see only catalogs and operations authorized for them.

Representative endpoints:

- Deployment: `https://vt.valon.tools`
- Private registry: `https://vt.valon.tools/registry`
- Administration: `https://vt.valon.tools/admin`
- Package administration: `https://vt.valon.tools/packages/<package>/admin`

Management APIs use a private listener or equivalent network restriction and require stronger authorization than user invocation endpoints.

## Invocation

Authorized operations are available over the deployment endpoint and through the CLI:

```sh
vt invoke --deployment <url> <package> <operation> --args <json>
```

Authentication comes from an interactive session, keychain, environment, or standard input. Secrets and tokens are not positional command arguments.

Operation invocation uses MCP Streamable HTTP. Clients and packages support JSON-RPC over POST and the protocol's JSON and SSE response forms. The gateway enforces request size, timeout, rate, and stream-lifetime policy.

Ingress strips externally supplied internal-identity headers. It exchanges user credentials for a short-lived, audience-bound internal invocation token containing the original actor, effective subject, immediate calling workload, delegation chain, target package and operation, issued/expiry times, nonce, and trace ID. Packages never forward raw user bearer tokens or API keys.

The authorization decision intersects:

- The authenticated calling workload's permission to invoke the target
- The effective subject's permission
- The installed package's approved operation policy
- Any operation-specific resource authorization

Unknown identities, packages, operations, policies, or authorization-provider outages deny access.

## Authentication and Authorization

- Users authenticate through an external identity provider with organization-level SSO, issuer and tenant allowlists, and session revocation.
- API keys are named, expiring, scoped API grants that act on behalf of an owner. Only hashes are stored, plaintext is returned once, and empty scope does not mean unrestricted access.
- Users create service accounts only with an attenuated subset of authority and explicit expiry or organization policy.
- Every installation has organization-owned workload and workflow identities.
- Every invocation is centrally authorized before dispatch; package-specific resource checks may further restrict it.
- Production does not start in a fail-open mode when identity or authorization is unavailable.

Authorization uses a Zanzibar-style graph. Relationship definitions declare whether they support delegated assignments, persistent grants, both, or neither. The default is neither.

- **Delegated assignment (`delegatable`)**: a holder with `can_delegate_<relationship>` may create a child assignment that cannot exceed or outlive its parent.
- **Persistent grant (`grantable`)**: a principal with `can_grant_<relationship>` may create an independent assignment that remains until explicitly revoked.

An assignment is a first-class record containing an immutable ID, organization, subject, relationship, resource, mode, issuer, creation and optional expiration, optional parent assignment, policy revision, and revocation state. Delegation has bounded depth, acyclic ancestry, transactional creation, and cascading invalidation. Independent valid paths preserve access after one path is revoked.

Authorization caches and delegated invocation tokens have bounded lifetimes. Revocation has a documented maximum propagation time and applies to long-lived streams and each workflow step. Security events are written to an immutable audit sink outside package-controlled storage.

## Configuration, Secrets, Ingress, and Egress

Release manifests declare typed requirements, not concrete secret values or unrestricted secret names. Administrators bind them to organization-owned configuration and secret-manager objects. Secret access is granted only to the installation or migration workload identity and uses short-lived retrieval where possible.

Capability expansion on upgrade requires renewed approval. Expansion includes new authorization relationships, identities, secrets, public routes, egress destinations, migration permissions, or caller delegation. Capability reduction may be automatically accepted under organization policy.

Every endpoint is classified as public, organization-authenticated, mesh-internal, or management-only. Egress is deny-by-default and explicitly grants destination, port, protocol, TLS/SNI, DNS, and credential behavior.

## Development Sandbox

`vt dev` builds a local package and connects it only to an explicitly enabled per-user sandbox:

```sh
vt dev --deployment <url> <dir>
```

Authentication uses the normal interactive or keychain flow. The platform issues a short-lived development workload identity distinct from the user's identity. Development routing is scoped to the developer or named test session and cannot take over a production package slug.

Development packages:

- Receive no production traffic by default
- Cannot run migrations
- Cannot access production secrets
- Cannot forward caller credentials
- Use sandbox-scoped authorization and egress
- Expire automatically and are audited on connect and disconnect

## Runtime Architecture

The runtime consists of:

- **Istio control plane (`istiod`)**: distributes routing, policy, endpoint, and certificate configuration to the Istio data plane.
- **Istio workload data plane**: Envoy sidecars mediate inbound and outbound workload traffic. Ambient mode, using mandatory node-level `ztunnel` proxies and optional L7 waypoint proxies, is a possible later deployment mode; a deployment uses one documented mode at a time.
- **Ingress gateways**: separate Envoy data-plane proxies terminate external transport, remove untrusted identity headers, authenticate supported credentials, and route public and organization traffic.
- **Egress gateways**: separate Envoy data-plane proxies enforce and audit declared external network access where direct egress cannot be safely allowed.
- **Package supervisor (`gestaltd`)**: runs alongside one package installation, mediates application-level invocation and authorization, reports observed state, and manages the package process. It is not an Istio data-plane proxy and does not replace Envoy.
- **Package runtime**: runs the package's MCP server, interfaces, and workflow workers.
- **Revision controllers**: reconcile durable install and rollout records into workloads, routing, catalogs, identities, and policy.

Production uses Istio `PeerAuthentication` in `STRICT` mode for mesh mutual TLS, Istio `AuthorizationPolicy` for workload-to-workload policy, and Kubernetes `NetworkPolicy` for network reachability. The deployment installs explicit default-deny authorization and network policies; these mechanisms are not assumed to deny traffic merely because they are enabled. Package application ports are reachable only through the mesh data plane. Health and supervisor administration ports have explicit, minimal exceptions and are not exposed through public ingress.

Established data-plane traffic continues using the last known configuration when `istiod` is unavailable. New revision operations that require routing, certificates, policy, or registry verification fail closed until their control-plane dependencies recover.

Terraform deploys the GCP infrastructure, including the Valon Tools cluster, GitHub Actions cluster, networking, container infrastructure, identity services, and deploy-pinned trusted components. Installing packages does not mutate this trusted computing base.

## Mesh Deployment

The mesh is redeployed only when changing deploy-pinned infrastructure, including:

- `istiod` and Istio data-plane versions
- `gestaltd` supervisor versions
- Ingress and egress gateways
- Identity, authorization, certificate, secret, audit, state-storage, registry-verification, or recovery services

Package releases declare compatible `gestaltd` and mesh protocol ranges. Infrastructure rollouts account for old and new runtime cohorts before admitting a package release that requires new capabilities.

Installing, upgrading, rolling back, or removing packages is a runtime change and does not require mesh redeployment. Connecting or disconnecting a registry is durable control-plane state and does not require redeployment, but changing registry trust roots remains a separately authorized and audited action.

## Retention and Audit

Immutable release metadata, attestations, and accepted revision history are retained permanently or according to organization audit policy. Blob retention is separate:

- Unused releases expire after a configurable window
- Running, admitted, and rollback-eligible revisions retain all required blobs
- Historical revisions retain blobs through a configurable recovery window
- Expired historical blobs may be garbage-collected while metadata and audit history remain

OCI garbage collection is reference-aware because layers may be shared. Pruning is serialized with admission and rechecks eligibility immediately before deletion.

Audit events include authentication, API-grant lifecycle, certificate issuance, authorization decisions, assignment mutation, delegated identity use, registry and trust-policy changes, publish/install/upgrade/rollback/removal, migration execution, secret access, ingress/egress denial, and development injection. Events include actor, effective subject, calling workload, target, policy revision, decision reason, trace ID, and `releaseDigest`.

## Required Verification

Before production use, automated tests cover:

- Create-only and atomic publication, concurrent publishers, and stale attempts
- Signature, provenance, trust-policy, platform, and runtime compatibility failures
- Install and upgrade validation failures writing no admitted revision
- Concurrent administrator and controller fencing
- Controller failure and restart at every revision phase
- Migration idempotency, lease loss, and incompatible rollback declarations
- Dependency and reverse-dependent races
- Canary failure, routing/catalog generation safety, draining, and SSE behavior
- Registry disconnection, mirror rebinding, and blob-prune concurrency
- Removal cleanup, tombstones, and retained workflow executions
- Authorization outage, confused-deputy attempts, delegation attenuation, and revocation propagation
- Multi-replica convergence and old/new infrastructure cohort overlap

## Glossary

**package identity**  
Permanent `(registryId, packageId)` for a published package. Assigned when the package is reserved; never reused.

**release**  
Immutable, signed artifact for one `releaseVersion` of a package, identified by `releaseDigest`.

**releaseVersion**  
Registry-assigned label for one published release of a package. `(registryId, packageId, releaseVersion)` binds permanently to one `releaseDigest`.

**releaseDigest**  
Content hash of the canonical release manifest bytes. The authoritative install input; signatures and attestations bind to it.

**revision**  
One installed state of a package in an organization, backed by a specific `releaseDigest`.

**revision request**  
Durable intent to install, upgrade, rollback, or remove a package.

**revision operation**  
Generation-fenced state machine that executes one revision request.

**revision controller**  
Background worker that drives a revision operation to completion and resumes after failure.

**rollout**  
Staged path from admission through canarying and promotion for an install or upgrade candidate.

**promotion**  
Makes a revision primary for routing and user-facing catalogs.

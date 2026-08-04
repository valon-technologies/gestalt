# Service Mesh Implementation Phases

Build in this order:

```text
1. Platform substrate
2. Durable control plane
3. Secure runtime and request path
4. Revision orchestration and reconciliation
5. Hardening, migration, and cutover
```

See [plan.md](plan.md) for the design, [responsibilities.md](responsibilities.md) for component ownership, and [prototypes.md](prototypes.md) for validated behavior.

## 1. Platform Substrate

### Outcome

A reproducible environment can host the control plane and ambient mesh.

### Build

- Extend the `valon-tools` GKE Standard Terraform stack with private networking, Cloud NAT, Workload Identity, Secret Manager CSI, and restricted control-plane access.
- Provision PlanetScale branches, roles, TLS access, backup retention, monitoring, and restore tooling.
- Create Artifact Registry repositories and GitHub OIDC identities for controller, Bun, and Helm artifacts.
- Install pinned Gateway API CRDs, Istio base, `istiod`, CNI, and `ztunnel` through Helm.
- Define the trust domain, certificate authority, ambient enrollment, default-deny L4 policy, and `NetworkPolicy`.
- Deploy OpenTelemetry collection and add Terraform, Helm, and mesh conformance workflows.

### Exit Criteria

- Terraform creates and destroys an environment without manual cloud changes.
- PlanetScale migrations, backup creation, and restore into a clean branch succeed.
- Sidecarless mTLS, waypoint traffic, and bypass denial pass the Prototype 1 checks.
- `istiod` restart behavior matches the design.

Defer package APIs, runtimes, and external traffic.

## 2. Durable Control Plane

### Outcome

The platform admits immutable releases and persists fenced revision intent without creating runtime workloads.

### Build

- Define PlanetScale schemas for registry state, package slots, revision requests, installation heads, fenced operations, migration outcomes, generations, terminal outcomes, and a transactional outbox.
- Store immutable bundles, manifests, signatures, schemas, workflows, migrations, and UI artifacts in Cloud Storage.
- Implement release verification, dependency checks, resumable publication, and package-version immutability.
- Start or signal deterministic Temporal workflows from the outbox.
- Add schema migrations, transaction-fencing tests, workflow replay tests, and recovery tools.
- Deploy public and management APIs with separate identities and listeners. Do not grant the API Kubernetes mutation permissions.

### Exit Criteria

- Invalid or incomplete releases remain unavailable.
- Concurrent first installations reserve one package slot.
- An outbox worker recovers a commit-before-workflow-start crash without duplicate workflows.
- Stale generations cannot replace newer intent.
- PlanetScale restore and Temporal replay recover without changing authoritative state.

Defer runtime Deployments, waypoint routing, promotion, pull-through import, UIs, and external connections.

## 3. Secure Runtime and Request Path

### Outcome

One static package revision supports authorized external and package-to-package invocation.

### Build

- Publish the unprivileged Bun image and runtime wrapper.
- Add the SDK outbound invocation adapter for destination headers and destination-bound caller tokens.
- Materialize organization service accounts as Kubernetes workload identities.
- Build ingress authentication, internal-header stripping, routing validation, limits, and caller-token minting.
- Deploy the authorization service and Envoy `ext_authz` endpoint.
- Deploy shared destination waypoint replicas, front and revision Services, Gateway API routes, `AuthorizationPolicy`, ambient L4 policy, and `NetworkPolicy`.
- Convert Prototype 2 checks into automated integration tests.

### Exit Criteria

- Authorized external invocation reaches package code.
- Package A invokes package B with B-specific headers and a new token.
- Direct revision Service and Pod IP access fail closed.
- Invalid tokens and operation/body disagreement fail before dispatch.
- Any replica of the assigned waypoint can process a request.

Defer automated upgrades, canaries, final migrations, UIs, and development sandboxes.

## 4. Revision Orchestration and Reconciliation

### Outcome

The platform installs, upgrades, rolls back, cancels, and removes runtime revisions.

### Build

- Define the minimal `RevisionOperation` CRD and status contract.
- Run the Temporal projection activity with narrow CRD permissions.
- Deploy leader-elected revision controllers built with `controller-runtime` and least-privilege RBAC.
- Require an authoritative PlanetScale generation check before mutation.
- Reconcile immutable Deployments, Services, ambient enrollment, routes, waypoint attachments, authorization policy, migration Jobs, and cleanup.
- Implement Admitted, Preparing, Starting, Ready, Promoting, Draining, and Complete.
- Implement atomic promotion, cancellation, pre-promotion rollback, post-promotion degradation, prepare migrations, and compensating migrations.
- Convert Prototype 3 checks into failure-injection tests.

### Exit Criteria

- Deleted resources are repaired.
- Duplicate workflow delivery and controller retries are idempotent.
- Controller and Temporal restarts converge from PlanetScale.
- Stale or cancelled operations cannot damage the promoted revision.
- Promotion advances routing and catalog generations once.
- Install, upgrade, rollback, cancellation, removal, and recovery pass end to end.

Defer advanced canaries, irreversible final migrations, multi-cluster placement, UI pinning, and sandboxes.

## 5. Hardening, Migration, and Cutover

### Outcome

The new platform can receive production traffic with a rehearsed rollback.

### Build

- Add replicas, PodDisruptionBudgets, topology spread, anti-affinity, priority classes, resource limits, and autoscaling.
- Add SLOs and alerts for PlanetScale, Temporal, controllers, route convergence, Istio, authorization, ingress, runtimes, and audit delivery.
- Route Kubernetes telemetry to Datadog and the immutable audit archive.
- Exercise PlanetScale restore, Temporal replay, certificate recovery, controller loss, and cluster recreation.
- Rehearse Kubernetes, Istio, controller, Bun, token-format, routing-contract, and database changes with rollback.
- Run policy, penetration, load, soak, drain, pinned-session, noisy-neighbor, and waypoint-failure tests.
- Migrate representative packages in increasing order of dependency and criticality.
- Shift ingress only after the previous serving path has passed a rollback rehearsal.

### Exit Criteria

- Protected components survive node disruption within their availability objectives.
- Recovery exercises meet recovery-time and recovery-point objectives.
- Load and soak tests meet latency, error, saturation, and reconciliation-age budgets.
- Platform upgrades preserve version overlap and rollback.
- A representative package passes invocation, upgrade, drain, rollback, and recovery under production-like load.
- Cutover and rollback runbooks succeed before the old path is retired.

Defer retirement of the old path until the observation window and operational approval are complete.

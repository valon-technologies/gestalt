# Service Mesh Implementation Phases

This roadmap turns the validated prototypes into production infrastructure and control-plane components. Build the pieces in order; each piece establishes a contract required by the next.

```text
1. Platform substrate
        |
        v
2. Durable control plane
        |
        v
3. Secure runtime and request path
        |
        v
4. Revision orchestration and reconciliation
        |
        v
5. Hardening, migration, and cutover
```

## Fixed Ownership

- PlanetScale is the authoritative SQL store for registry state, revisions, generations, grants, migrations, and recovery state.
- Temporal sequences long-running phases and cross-system retries.
- `RevisionOperation` is a disposable, generation-fenced projection of work.
- Revision controllers built with `controller-runtime` converge Kubernetes and Istio resources and report status.
- `istiod` distributes mesh configuration to `ztunnel` and waypoint proxies. It does not own Gestalt revisions or workflows.
- Ingress authenticates external callers, waypoints authorize and route each destination request, and the Bun wrapper enforces the application protocol.
- Terraform owns cloud resources. Pinned Helm releases own shared Kubernetes infrastructure. Controllers own per-installation runtime resources.

## 1. Platform Substrate

### Objective

Create a reproducible, empty deployment platform before moving any Gestalt package behavior onto it.

### Build

1. Use the existing `valon-tools` GKE Standard module to provision private, regional, VPC-native clusters with separate Pod and Service ranges, private nodes, Cloud NAT, Workload Identity, Secret Manager CSI, release channels, and restricted control-plane access.
2. Create Artifact Registry repositories for the revision-controller image, platform Bun image, and Helm charts. Publish only digest-addressed artifacts through GitHub OIDC identities.
3. Provision the PlanetScale database and production branch, migration and application roles, TLS connectivity from GKE, connection management, backup retention, monitoring, and restore tooling through the PlanetScale Terraform provider.
4. Create Secret Manager containers and workload-specific IAM for database credentials, Temporal credentials, token-signing keys, registry trust roots, and telemetry credentials.
5. Install pinned Gateway API CRDs and self-managed Istio ambient components through Helm: base CRDs, `istiod`, CNI, and `ztunnel`. Define the trust domain, certificate authority, ambient enrollment policy, default-deny L4 policy, and supplemental `NetworkPolicy`.
6. Deploy cluster-local OpenTelemetry collection and connect it to the existing Datadog and audit pipelines. Add basic cluster, `istiod`, `ztunnel`, and waypoint dashboards.
7. Add Terraform plan/apply and Helm render, dry-run, rollout, and conformance workflows that run from an in-network deploy runner.

### Dependencies

None. This is the bootstrap layer.

### Completion Gate

- A clean environment can be created and destroyed without manual cloud changes.
- Terraform plans contain no unexpected replacement or destroy operations.
- Nodes pull private images through Workload Identity and private networking.
- PlanetScale migrations, backup creation, and restore into a clean branch succeed.
- Sidecarless workloads receive ambient mTLS, reach a shared waypoint, and cannot bypass required L4 policy.
- Existing traffic continues with cached configuration during an `istiod` restart, while configuration-dependent changes stop.
- Prototype 1 conformance checks pass against the Helm-managed installation.

### Defer

Package admission APIs, external production traffic, package runtimes, revision controllers, canarying, and organization package migration.

## 2. Durable Gestalt Control Plane

### Objective

Establish authoritative state and release admission before creating package workloads.

### Build

1. Define PlanetScale schemas for registry catalogs, immutable release metadata, import and publication attempts, package slots, append-only revision requests, installation heads, fenced revision operations, migration outcomes, routing generations, terminal outcomes, and a transactional outbox.
2. Store immutable release bundles, manifests, detached signatures, schemas, workflow definitions, migrations, and UI artifacts in Cloud Storage with generation-match preconditions, retention, and versioning.
3. Build registry verification for canonical manifests, artifact digests, signatures, trust policy, package-version immutability, operation contracts, dependency compatibility, and complete local artifact availability.
4. Implement authenticated, resumable publication into the deployment registry. Start with direct push; defer pull-through import until the release and trust contracts are stable.
5. Connect the control plane to Temporal Cloud with deterministic workflow IDs. Use the PlanetScale outbox to start or signal workflows idempotently after the authoritative transaction commits.
6. Add database migration jobs, schema-version checks, transaction-fencing tests, workflow replay tests, and recovery tooling.
7. Deploy control-plane APIs with separate public and management listeners, workload identities, minimal permissions, health checks, audit emission, and OTLP telemetry. The API process does not receive Kubernetes mutation permissions.

### Dependencies

Piece 1 provides GKE, PlanetScale, storage, secrets, artifact publication, networking, and telemetry.

### Completion Gate

- Conflicting publication of one package version is rejected.
- Interrupted uploads resume, and incomplete publications remain invisible.
- Admission uses only artifacts present in the deployment registry and rejects digest, signature, trust, schema, or dependency mismatches.
- Concurrent first installations reserve one package slot.
- A crash after committing PlanetScale state but before starting Temporal is recovered through the outbox without duplicating the workflow.
- Stale generations cannot overwrite newer revision intent.
- PlanetScale restore recovers authoritative state, and Temporal replay resumes workflows without changing that state.

### Defer

Runtime Deployments, waypoint routing, production invocation, revision promotion, pull-through registry federation, UIs, and external credential connections.

## 3. Secure Runtime and Request Path

### Objective

Run one package revision through the complete external and package-to-package authorization path before automating upgrades.

### Build

1. Publish the platform-owned, unprivileged Bun image and runtime wrapper. The wrapper owns MCP and health listeners, verifies signed internal context, checks operation metadata against the request body, validates schemas, dispatches package code, emits telemetry, and drains.
2. Add the SDK outbound invocation adapter. Package code supplies the destination installation and operation; the trusted adapter obtains a destination-bound caller token and adds reserved installation, operation, and optional revision or session headers.
3. Materialize organization service accounts as Kubernetes workload identities with no ambient authority beyond explicit grants.
4. Build ingress authentication, internal-header stripping, routing-metadata validation, external limits, and short-lived caller-token minting.
5. Deploy the authorization service and Envoy `ext_authz` endpoint. Support workload-only authority and explicit delegation of the original caller.
6. Deploy shared destination waypoint replicas, front and revision Services, Gateway API routes, `CUSTOM` `AuthorizationPolicy`, ambient L4 policy, and `NetworkPolicy`.
7. Initially deploy test revisions declaratively. Do not add production rollout automation until the request path is stable.

### Dependencies

Piece 1 provides the mesh, identities, artifact pipeline, and telemetry. Piece 2 provides package contracts, grants, token state, and admitted release artifacts.

### Completion Gate

- An authorized external invocation passes ingress, destination authorization, routing, and wrapper validation.
- Package A can invoke package B with a new destination-bound token and B-specific routing headers.
- Package A cannot reuse or blindly forward its incoming caller token or internal-context headers.
- Requests that bypass the front Service or waypoint through a revision Service or Pod IP fail closed.
- Invalid, expired, mismatched, or unauthorized tokens never reach package code.
- Operation-header and MCP-body disagreement is rejected by the wrapper.
- Any replica of the assigned shared waypoint can handle a request without application session state.
- Prototype 2 authorization, routing, bypass, and metadata assertions run as automated integration tests.

### Defer

Automated upgrades, weighted canaries, final migrations, UI serving, development sandboxes, and broad package migration.

## 4. Revision Orchestration and Reconciliation

### Objective

Turn durable revision intent into repeatable installation, upgrade, rollback, cancellation, and removal.

### Build

1. Define the minimal `RevisionOperation` CRD and status contract. Treat it as a rebuildable work projection, not the source of truth.
2. Build separately deployed revision controllers with `controller-runtime`, leader election, health endpoints, narrow service accounts, and least-privilege RBAC.
3. Have Temporal own phase sequencing. After committing fenced PlanetScale intent, project the current phase into Kubernetes and wait for controller convergence before advancing.
4. Run the Temporal projection activity under a dedicated identity with narrow permission to create and update `RevisionOperation` resources. Do not give the public control-plane API Kubernetes mutation permissions.
5. Have controllers perform an authoritative generation check before mutation and reconcile deterministic, immutable Deployments, revision Services, front Services, ambient enrollment, routes, waypoint attachment, authorization policy, migration Jobs, and cleanup.
6. Implement the initial lifecycle: Admitted, Preparing, Starting, Ready, Promoting, Draining, and Complete. Add Observing after the base lifecycle is reliable.
7. Promote by first converging routing and staged artifacts, then atomically advancing the PlanetScale installation head and catalog generations. Preserve the prior revision until drain and rollback deadlines expire.
8. Implement cancellation, pre-promotion rollback, post-promotion degradation, forward recovery, idempotent prepare migrations, and explicit compensating migrations. Never automate destructive down migrations.
9. Convert Prototype 3 drift, restart, stale-generation, leader-election, and candidate-cancellation scenarios into failure-injection tests.

### Dependencies

Piece 2 owns authoritative state and workflow sequencing. Piece 3 supplies the runtime contract, authorization path, waypoint behavior, and revision routing.

### Completion Gate

- Deleting a managed child resource causes deterministic repair.
- Duplicate Temporal delivery and controller retries do not duplicate resources or phase effects.
- Controller or Temporal restarts converge from PlanetScale and the projected operation.
- A stale or cancelled operation cannot mutate or delete the promoted revision.
- Pre-promotion failure restores prior routing without advancing PlanetScale generations.
- Promotion advances routing and catalog generations exactly once.
- Existing pinned sessions remain on their revision until closure or deadline; new sessions use the promoted revision.
- Install, upgrade, rollback, cancellation, removal, and recovery pass end-to-end failure-injection tests.

### Defer

Advanced weighted canary policy, irreversible finalize migrations, multi-cluster placement, autoscaling based on business metrics, UI version pinning, and package developer sandboxes.

## 5. Hardening, Migration, and Cutover

### Objective

Prove the platform can operate safely under production load and failure before moving the deployment endpoint.

### Build

1. Add multiple replicas, PodDisruptionBudgets, topology spread, anti-affinity, priority classes, resource guarantees, autoscaling, and capacity limits for every bootstrap and protected component.
2. Define SLOs and alerts for control-plane admission, PlanetScale and Temporal latency, controller queues, reconciliation age, route convergence, `istiod`, `ztunnel`, waypoints, authorization, ingress, runtime health, and audit delivery.
3. Route Kubernetes logs and OTLP telemetry into Datadog and the immutable audit archive. Ensure authorization and required security-event delivery fail closed or buffer durably.
4. Exercise PlanetScale restore, Temporal replay, expired-certificate recovery, lost-controller recovery, cluster recreation, and restoration from authoritative state.
5. Establish pinned Kubernetes, Istio, Envoy, Bun, routing-contract, token-format, and CRD compatibility windows. Rehearse mesh, database, controller, and runtime upgrades and rollback.
6. Add policy regression, penetration, load, soak, connection-drain, pinned-session, noisy-neighbor, and waypoint-failure tests.
7. Migrate representative packages in cohorts: internal test package, non-critical package, package with dependencies, package with workflows, and finally critical packages.
8. Shift external ingress traffic only after rollback to the previous serving path has been rehearsed. Keep the old path available through the agreed observation window.

### Dependencies

Pieces 1 through 4 must pass their completion gates.

### Completion Gate

- Every protected component survives a node disruption without violating its availability objective.
- Disaster-recovery exercises meet recovery-time and recovery-point objectives.
- Load and soak tests stay within latency, error, saturation, and reconciliation-age budgets.
- Mesh and platform upgrades preserve supported version overlap and can be rolled back.
- Audit events are complete, ordered where required, retained, and inaccessible to organization packages.
- At least one representative package completes install, package-to-package invocation, upgrade, drain, rollback, and recovery under production-like load.
- External cutover and rollback runbooks are executed successfully before the old path is retired.

### Defer

Remove the old serving path only after the cutover observation window and explicit operational approval. Add UI staging, external credential connections, development sandboxes, multi-cluster scheduling, and advanced canary policy as separate follow-on plans unless they become cutover requirements.

## Existing Foundations

- [Service mesh design](plan.md)
- [Component responsibilities](responsibilities.md)
- [Prototype findings](prototypes.md)
- `valon-tools/modules/gke` for private GKE Standard
- `valon-tools/stacks/infra` for environment composition, deploy identity, and private-cluster access
- `gestaltd/terraform` and Gestalt publication workflows for Artifact Registry and GitHub OIDC
- `gestaltd/deploy/helm/gestaltd` for the current deployment surface
- `toolshed/otel` for OpenTelemetry and Datadog export
- `toolshed/valon-tools/deploy/temporal-deploymentctl` for Temporal worker-version rollout patterns

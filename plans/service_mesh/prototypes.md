# Service Mesh Prototypes

Disposable experiments; not production tooling.

## Prototype 1: Managed Mesh and Ambient Waypoints

Implementation: [valon-tools#366](https://github.com/valon-technologies/valon-tools/pull/366)

Tested managed Google Cloud Service Mesh, then converted the GKE Standard
cluster to self-managed Istio ambient mode with a shared waypoint.

Findings:

- Managed Google Cloud Service Mesh lacks the required ambient `ztunnel` and
  waypoint data plane.
- Self-managed Istio ambient supports sidecarless workloads and shared
  destination waypoints on GKE Standard.
- Use self-managed Istio, not managed Google Cloud Service Mesh.

Run from `valon-tools`:

```sh
scripts/setup-mesh-prototype-cluster.sh dev
scripts/deploy-mesh-prototype-demo.sh dev
AUTO_APPROVE=true scripts/deploy-mesh-prototype-waypoint-demo.sh dev
```

The second command tests managed Cloud Service Mesh. The final command
permanently converts the cluster to self-managed Istio ambient.

Remove workloads:

```sh
scripts/deploy-mesh-prototype-demo.sh dev --destroy
scripts/deploy-mesh-prototype-waypoint-demo.sh dev --destroy
```

## Prototype 2: Authorized Revision Promotion

Implementation: [valon-tools#367](https://github.com/valon-technologies/valon-tools/pull/367)

Tested two immutable package revisions behind a destination waypoint.

Findings:

- A waypoint `HTTPRoute` can route by revision header and change the default
  revision independently.
- A waypoint `CUSTOM` `AuthorizationPolicy` with `ext_authz` fails closed for
  front Service, revision Service, and direct Pod IP requests.
- Default traffic can move from v1 to v2 while pinned requests remain on v1.
- The runtime can reject operation metadata that differs from the JSON-RPC body.
- Persisting promotion intent before route changes and committing the promoted
  revision afterward provides the required rollout ordering. SQLite state
  survived a Pod restart; Cloud SQL transaction fencing and recovery remain
  untested.

Run from `valon-tools` after Prototype 1 converts the cluster:

```sh
scripts/deploy-mesh-vertical-slice-prototype.sh dev
```

The script deploys and verifies the prototype. Remove its workloads and PVC:

```sh
scripts/deploy-mesh-vertical-slice-prototype.sh dev --destroy
```

## Prototype 3: Revision Reconciliation

Implementation: [valon-tools#368](https://github.com/valon-technologies/valon-tools/pull/368)

Tested a `controller-runtime` revision controller with two replicas.

Findings:

- Kubernetes lease election kept one active reconciler.
- Child-resource watches repaired a deleted Service. Reconciliation also
  converged after both controller Pods restarted with partial state.
- An uncached authoritative-intent read fenced a stale `RevisionOperation`
  generation without deleting the promoted or candidate revision.
- Cancellation deleted the candidate Deployment and Service while retaining
  the promoted revision.
- `controller-runtime` fits the revision controllers. Keep SQL authoritative
  and use a minimal `RevisionOperation` as a rebuildable work projection.
- The prototype used a ConfigMap in place of SQL. SQL transactions and Temporal
  integration remain untested.

Responsibility split:

- **Gestalt control plane** owns package admission, authoritative SQL state,
  authorization bindings, Temporal workflows, revision intent, promotion, and
  recovery. It projects fenced work into Kubernetes and consumes convergence
  status.
- **`controller-runtime`** is a Go library used by Gestalt revision controllers,
  not a standalone control plane. Controllers elect a leader, watch
  `RevisionOperation` projections and child resources, and reconcile
  Deployments, Services, routes, enrollment, and policies. They do not own
  package state or workflow sequencing.
- **Gestalt SDK** provides package-authoring APIs, runtime schemas, catalog
  metadata, typed handlers, host-service clients, and transport adapters. It
  does not deploy workloads or configure the mesh.
- **`istiod`** is the Istio control plane, not the Gestalt control plane. It
  watches Kubernetes, Gateway API, and Istio resources, then distributes
  endpoints, routes, identities, certificates, and policies to the mesh data
  plane. It does not own Gestalt revisions, grants, workflows, or catalogs.
- **Ingress** is the external trust boundary. It authenticates callers, removes
  caller-supplied internal headers, validates routing metadata, applies external
  limits, and mints short-lived caller tokens.
- **Destination waypoints** are L7 data-plane proxies. They call `ext_authz`,
  enforce authorization policy, and route approved requests to revision
  Services. They do not decode MCP bodies or own deployment state.

Run from `valon-tools` against the shared prototype cluster:

```sh
scripts/deploy-mesh-controller-runtime-prototype.sh dev
```

The script builds the controller image, deploys the resources, and verifies the
failure cases. Remove the namespace, RBAC, and CRD:

```sh
scripts/deploy-mesh-controller-runtime-prototype.sh dev --destroy
```

Remove the shared cluster and network:

```sh
AUTO_APPROVE=true scripts/setup-mesh-prototype-cluster.sh dev --destroy
```

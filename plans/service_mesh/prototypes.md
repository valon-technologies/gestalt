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

Remove the shared cluster and network:

```sh
AUTO_APPROVE=true scripts/setup-mesh-prototype-cluster.sh dev --destroy
```

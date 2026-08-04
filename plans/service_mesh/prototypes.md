# Service Mesh Prototypes

These prototypes are disposable experiments used to validate infrastructure
choices and the highest-risk request and rollout paths. They are not intended
to become production deployment tooling.

## Prototype 1: Managed Mesh and Ambient Waypoints

Implementation: [valon-tools#366](https://github.com/valon-technologies/valon-tools/pull/366)

This prototype created an isolated GKE Standard cluster, first exercised
managed Google Cloud Service Mesh, and then converted the cluster to
self-managed upstream Istio ambient mode to deploy a shared waypoint.

What we learned:

- Managed Google Cloud Service Mesh does not provide the ambient `ztunnel` and
  waypoint data plane required by this architecture.
- Self-managed upstream Istio ambient mode can run sidecarless workloads and a
  shared destination waypoint on GKE Standard.
- The production architecture should therefore use self-managed upstream
  Istio rather than managed Google Cloud Service Mesh.

## Prototype 2: Authorized Revision Promotion

Implementation: [valon-tools#367](https://github.com/valon-technologies/valon-tools/pull/367)

This prototype deployed two immutable package revisions behind a destination
waypoint and exercised the request and promotion path proposed in the service
mesh design.

What we learned:

- A waypoint-attached Gateway API `HTTPRoute` can select revision-specific
  Services from a reserved revision header and independently change the
  default revision.
- A waypoint-attached `CUSTOM` `AuthorizationPolicy` can fail closed through
  `ext_authz` for the front Service, revision-specific Services, and direct Pod
  IP requests.
- Default traffic can move from revision v1 to v2 while requests explicitly
  pinned to v1 continue to reach v1.
- The runtime can independently reject a request when its authorized operation
  metadata does not match its JSON-RPC body.
- Persisting promotion intent before changing the route, and committing the
  promoted revision afterward, works as the basis of the proposed rollout
  ordering. The prototype's SQLite PVC survived a state-store Pod restart, but
  it did not test Cloud SQL transaction fencing or recovery.

The prototype uses a mock bearer token, a small HTTP authorization service, and
a SQLite state store. Signed caller and session tokens, the production
authorization model, MCP streaming, and database fencing remain to be tested.

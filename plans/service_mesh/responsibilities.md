# Service Mesh Responsibilities

- **Gestalt control plane** owns package admission, authoritative SQL state, authorization bindings, Temporal workflows, revision intent, promotion, and recovery. It projects fenced work into Kubernetes and consumes convergence status.
- **`controller-runtime`** is a Go library used by Gestalt revision controllers, not a standalone control plane. Controllers elect a leader, watch `RevisionOperation` projections and child resources, and reconcile Deployments, Services, routes, enrollment, and policies. They do not own package state or workflow sequencing.
- **Gestalt SDK** provides package-authoring APIs, runtime schemas, catalog metadata, typed handlers, host-service clients, and transport adapters. It does not deploy workloads or configure the mesh.
- **`istiod`** is the Istio control plane, not the Gestalt control plane. It watches Kubernetes, Gateway API, and Istio resources, then distributes endpoints, routes, identities, certificates, and policies to the mesh data plane. It does not own Gestalt revisions, grants, workflows, or catalogs.
- **Ingress** is the external trust boundary. It authenticates callers, removes caller-supplied internal headers, validates routing metadata, applies external limits, and mints short-lived caller tokens.
- **Destination waypoints** are L7 data-plane proxies. They call `ext_authz`, enforce authorization policy, and route approved requests to revision Services. They do not decode MCP bodies or own deployment state.

## Interactions

```text
CONTROL

Administrator
    |
    v
Gestalt control plane ---> SQL (authoritative state)
    |
    v
Temporal workflow
    |
    v
RevisionOperation
    |
    v
Revision controller (controller-runtime)
    |
    v
Kubernetes and Istio resources
    |
    v
istiod ---> ztunnel and waypoint configuration

REQUEST

External caller
    |
    v
Ingress (authenticate and mint caller token)
    |
    v
Destination waypoint A (authorize and route)
    |
    v
Package A Bun wrapper (verify context)
    |
    v
Package A SDK invokes package B
    |
    v
Bun outbound adapter (add B headers and destination-bound token)
    |
    v
ztunnel (mTLS)
    |
    v
Destination waypoint B (ext_authz and revision routing)
    |
    v
Package B Bun wrapper (verify context and body)
    |
    v
Package B handler
```

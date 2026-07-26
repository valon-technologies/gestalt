# App Registry Implementation Changelog

The app registry was delivered as sixteen implementation milestones. This file
records the purpose and merged pull requests for each milestone.

## 1. GCS registry and publish command

Introduced the GCS-backed registry model, immutable version metadata and
artifacts, registry configuration, and the original app publish CLI.

PRs:

- [gestalt#2709 — Prototype gestaltd app publish for GCS app registries](https://github.com/valon-technologies/gestalt/pull/2709)

## 2. Parallel registry publish workflow

Added a toolshed registry publish workflow alongside the existing snapshot
workflow so the registry could be tested without disrupting normal publishing.

PRs:

- [toolshed#3220 — Add parallel app registry publish workflow](https://github.com/valon-technologies/toolshed/pull/3220)

## 3. Automatic `g-issues` publishing

Enrolled `g-issues` as the first registry app and automatically published a
registry version when it changed on `main`.

PRs:

- [toolshed#3221 — Auto-publish g-issues to the app registry](https://github.com/valon-technologies/toolshed/pull/3221)

## 4. Registry version listing API

Added the registry HTTP reader and admin endpoints for listing configured
registries and published versions.

PRs:

- [gestalt#2716 — List app registry versions via admin API](https://github.com/valon-technologies/gestalt/pull/2716)

## 5. Fleet installation state

Added IndexedDB stores and services for shared app installation state. The
model later converged on append-only `app_version_change_requests` and
known-version projections.

PRs:

- [gestalt#2718 — Add IndexedDB install state for registry app versions](https://github.com/valon-technologies/gestalt/pull/2718)
- [gestalt#2753 — Rename app_version_catalog to app_version_change_requests](https://github.com/valon-technologies/gestalt/pull/2753)

## 6. Registry app installation prototype

Implemented fleet install locking, registry artifact validation and
materialization on the handling instance, known-version writes, and admin
install endpoints. The later catalog-only flow replaced local materialization
on the request handler.

PRs:

- [gestalt#2730 — Registry app install with app_version_catalog](https://github.com/valon-technologies/gestalt/pull/2730)

## 7. Catalog-only install admission

Separated fleet admission from local materialization. The install endpoint
validates registry metadata and appends a version change request; replicas
perform runtime work asynchronously.

PRs:

- [gestalt#2748 — App registry lifecycle docs and catalog-only install](https://github.com/valon-technologies/gestalt/pull/2748)

## 8. Per-replica catalog polling

Added `app_instance_materializations` and a background controller on every
replica to acknowledge new fleet-known app versions.

PRs:

- [gestalt#2750 — Per-replica catalog polling](https://github.com/valon-technologies/gestalt/pull/2750)

## 9. Coordinated provider restarts

Added one active rollout per app, dynamic replica enrollment, rollout cohorts,
and coordinated stop/start behavior with persistent convergence state.

PRs:

- [gestalt#2812 — Coordinate provider restarts across replicas](https://github.com/valon-technologies/gestalt/pull/2812)

## 10. Materialize before restart

Changed the catalog controller to download and validate the desired registry
artifact before stopping the running app, recording `materialized_at` per
replica.

PRs:

- [gestalt#2829 — Materialize artifacts before restart](https://github.com/valon-technologies/gestalt/pull/2829)

## 11. Mount the registry-installed package

Changed catalog-driven restart to bind the package at
`{artifactsDir}/registry-installed/{app}/{version}` so the restarted provider
uses the selected binary, manifest, and static assets.

PRs:

- [gestalt#2838 — Mount registry-installed binary on restart](https://github.com/valon-technologies/gestalt/pull/2838)

## 12. Registry-only app lifecycle

Added `source.registry`, separate add/upgrade admission routes, bootstrap
startup for registry-only apps, latest-version-only materialization, retry
limits, and cleanup of superseded local packages.

PRs:

- [gestalt#2868 — Document registry-only source and conditional bootstrap](https://github.com/valon-technologies/gestalt/pull/2868)
- [gestalt#2878 — Clarify latest-only app registry materialization](https://github.com/valon-technologies/gestalt/pull/2878)
- [gestalt#2879 — Implement app registry step 12](https://github.com/valon-technologies/gestalt/pull/2879)

## 13. Install-time validation

Added typed admission checks for platform artifacts, `gestaltd`
compatibility, declared dependencies, and reverse dependents. Validation
failures reject admission without changing fleet state.

PRs:

- [gestalt#2885 — Document app registry step 13 install-time validation](https://github.com/valon-technologies/gestalt/pull/2885)
- [gestalt#2889 — Document app registry install-time validation](https://github.com/valon-technologies/gestalt/pull/2889)
- [gestalt#2887 — Implement app registry install-time validation](https://github.com/valon-technologies/gestalt/pull/2887)

## 14. Fleet admin observability

Added authenticated registry-only app summaries, rollout and per-replica
materialization APIs, and App Registry list/detail views in the embedded admin
UI.

PRs:

- [gestalt#2886 — Document app registry admin observability](https://github.com/valon-technologies/gestalt/pull/2886)
- [gestalt#2890 — Implement app registry admin observability](https://github.com/valon-technologies/gestalt/pull/2890)

## 15. App-scoped version selection

Added app-scoped authorization and APIs for first install, upgrade, and safe
revert. Added `/apps/{app}/admin`, publication provenance, and the published
snapshots table used to select the desired fleet version.

PRs:

- [gestalt#2897 — Document app registry version selection](https://github.com/valon-technologies/gestalt/pull/2897)
- [gestalt#2909 — Implement app registry version selection](https://github.com/valon-technologies/gestalt/pull/2909)
- [gestalt#2914 — Add app admin snapshot table API support](https://github.com/valon-technologies/gestalt/pull/2914)
- [gestalt-providers#1142 — Add app registry admin UI](https://github.com/valon-technologies/gestalt-providers/pull/1142)
- [gestalt-providers#1146 — Replace app admin version dropdown with snapshots table](https://github.com/valon-technologies/gestalt-providers/pull/1146)
- [toolshed#3696 — Switch g-issues to app registry source](https://github.com/valon-technologies/toolshed/pull/3696)

## 16. Pending and failed publish visibility

Added `pending.json` and `failed.json`, CI lifecycle commands, early pending
recording, app-admin API rows, publishing duration, polling, and the final
status/last-update presentation.

PRs:

- [gestalt#2927 — Document pending publish visibility](https://github.com/valon-technologies/gestalt/pull/2927)
- [gestalt#2932 — Add app registry pending write path](https://github.com/valon-technologies/gestalt/pull/2932)
- [gestalt#2931 — Expose pending and failed catalogs in app-admin registry API](https://github.com/valon-technologies/gestalt/pull/2931)
- [toolshed#3772 — Wire pending lifecycle into the publish workflow](https://github.com/valon-technologies/toolshed/pull/3772)
- [toolshed#3775 — Record pending earlier in the publish workflow](https://github.com/valon-technologies/toolshed/pull/3775)
- [gestalt-providers#1158 — Show pending and failed rows](https://github.com/valon-technologies/gestalt-providers/pull/1158)
- [gestalt-providers#1159 — Poll app admin registry during bootstrap window](https://github.com/valon-technologies/gestalt-providers/pull/1159)
- [gestalt-providers#1160 — Improve app admin snapshot published column labels](https://github.com/valon-technologies/gestalt-providers/pull/1160)
- [gestalt-providers#1161 — Fix publishing spinner visibility](https://github.com/valon-technologies/gestalt-providers/pull/1161)
- [gestalt-providers#1162 — Add the Last update column](https://github.com/valon-technologies/gestalt-providers/pull/1162)

# App Registry Implementation Changelog

The app registry has implementation details across `gestalt`, `toolshed`, and `gestalt-providers`.

## At a Glance

The `Completed` timestamp is the merge time of the PR that delivered the milestone's main outcome.

| `Step` | `Completed (UTC)` | `Milestone` | `Tags` |
| --: | :-- | :-- | :-- |
| `01` | `🗓️ Jul 10 · 00:30` | `GCS Registry and Publish Command` | [`config.md`](../architecture/config.md) [`models.md`](../architecture/models.md) |
| `02` | `🗓️ Jul 10 · 02:05` | `Parallel Registry Publishing` | [`config.md`](../architecture/config.md) |
| `03` | `🗓️ Jul 10 · 02:14` | `First Automatically Published App` | [`config.md`](../architecture/config.md) |
| `04` | `🗓️ Jul 10 · 03:22` | `Registry Listing API` | [`lifecycle.md`](../operations/lifecycle.md) |
| `05` | `🗓️ Jul 10 · 16:52` | `Installation State in IndexedDB` | [`indexeddb.md`](../architecture/indexeddb.md) |
| `06` | `🗓️ Jul 11 · 23:41` | `Registry Installation Prototype` | [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `07` | `🗓️ Jul 13 · 21:04` | `Catalog-Only Admission` | [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `08` | `🗓️ Jul 13 · 22:38` | `Per-Replica Catalog Polling` | [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `09` | `🗓️ Jul 16 · 21:24` | `Coordinated Provider Restarts` | [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `10` | `🗓️ Jul 17 · 19:52` | `Materialize Before Restart` | [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `11` | `🗓️ Jul 20 · 16:19` | `Mount the Registry-Installed Package` | [`lifecycle.md`](../operations/lifecycle.md) |
| `12` | `🗓️ Jul 21 · 11:44` | `Complete Registry-Only Lifecycle` | [`config.md`](../architecture/config.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `13` | `🗓️ Jul 22 · 04:16` | `Install-Time Validation` | [`validation.md`](../architecture/validation.md) |
| `14` | `🗓️ Jul 22 · 15:51` | `Fleet Admin Observability` | [`admin.md`](../operations/admin.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `15` | `🗓️ Jul 23 · 16:33` | `App-Scoped Version Selection` | [`admin.md`](../operations/admin.md) [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md) |
| `16` | `🗓️ Jul 26 · 00:18` | `Pending and Failed Publish Visibility` | [`admin.md`](../operations/admin.md) [`models.md`](../architecture/models.md) [`pending-publish.md`](../operations/pending-publish.md) |

## Tag Glossary

- [`admin.md`](../operations/admin.md)
- [`config.md`](../architecture/config.md)
- [`indexeddb.md`](../architecture/indexeddb.md)
- [`lifecycle.md`](../operations/lifecycle.md)
- [`models.md`](../architecture/models.md)
- [`pending-publish.md`](../operations/pending-publish.md)
- [`validation.md`](../architecture/validation.md)

---

## Changes

<a id="changelog-01"></a>

### 01 — GCS Registry and Publish Command

**Tags:** [`config.md`](../architecture/config.md) [`models.md`](../architecture/models.md)

Established the GCS registry layout, immutable version metadata and artifacts, registry configuration, and the original app publish CLI.

**Merged:** [gestalt#2709](https://github.com/valon-technologies/gestalt/pull/2709)

<a id="changelog-02"></a>

### 02 — Parallel Registry Publishing

**Tags:** [`config.md`](../architecture/config.md)

Added a toolshed registry workflow alongside snapshot publishing so the new path could be exercised without disrupting existing releases.

**Merged:** [toolshed#3220](https://github.com/valon-technologies/toolshed/pull/3220)

<a id="changelog-03"></a>

### 03 — First Automatically Published App

**Tags:** [`config.md`](../architecture/config.md)

Enrolled `g-issues` as the first registry app and published a new registry version whenever it changed on `main`.

**Merged:** [toolshed#3221](https://github.com/valon-technologies/toolshed/pull/3221)

<a id="changelog-04"></a>

### 04 — Registry Listing API

**Tags:** [`lifecycle.md`](../operations/lifecycle.md)

Added the HTTP registry reader and admin endpoints for listing configured registries and their published versions.

**Merged:** [gestalt#2716](https://github.com/valon-technologies/gestalt/pull/2716)

<a id="changelog-05"></a>

### 05 — Installation State in IndexedDB

**Tags:** [`indexeddb.md`](../architecture/indexeddb.md)

Added shared installation stores and services. The model later converged on the append-only `app_version_change_requests` log and fleet-known projections.

**Merged:** [gestalt#2718](https://github.com/valon-technologies/gestalt/pull/2718) · [gestalt#2753](https://github.com/valon-technologies/gestalt/pull/2753)

<a id="changelog-06"></a>

### 06 — Registry Installation Prototype

**Tags:** [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md)

Implemented fleet install locking, registry validation, materialization on the request-handling instance, fleet-known writes, and admin install endpoints. Milestone 07 replaced the synchronous materialization behavior.

**Merged:** [gestalt#2730](https://github.com/valon-technologies/gestalt/pull/2730)

<a id="changelog-07"></a>

### 07 — Catalog-Only Admission

**Tags:** [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md)

Separated fleet admission from local runtime work. The install endpoint validates registry metadata and appends a version change request; replicas materialize asynchronously.

**Merged:** [gestalt#2748](https://github.com/valon-technologies/gestalt/pull/2748)

<a id="changelog-08"></a>

### 08 — Per-Replica Catalog Polling

**Tags:** [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md)

Added `app_instance_materializations` and a background controller on every replica to acknowledge newly fleet-known app versions.

**Merged:** [gestalt#2750](https://github.com/valon-technologies/gestalt/pull/2750)

<a id="changelog-09"></a>

### 09 — Coordinated Provider Restarts

**Tags:** [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md)

Added one active rollout per app, dynamic replica enrollment, rollout cohorts, and coordinated stop/start behavior with persistent convergence state.

**Merged:** [gestalt#2812](https://github.com/valon-technologies/gestalt/pull/2812)

<a id="changelog-10"></a>

### 10 — Materialize Before Restart

**Tags:** [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md)

Changed the catalog controller to download and validate the desired artifact before stopping the running app, recording `materialized_at` per replica.

**Merged:** [gestalt#2829](https://github.com/valon-technologies/gestalt/pull/2829)

<a id="changelog-11"></a>

### 11 — Mount the Registry-Installed Package

**Tags:** [`lifecycle.md`](../operations/lifecycle.md)

Changed restart to bind `{artifactsDir}/registry-installed/{app}/{version}`, making the selected binary, manifest, and static assets active.

**Merged:** [gestalt#2838](https://github.com/valon-technologies/gestalt/pull/2838)

<a id="changelog-12"></a>

### 12 — Complete Registry-Only Lifecycle

**Tags:** [`config.md`](../architecture/config.md) [`lifecycle.md`](../operations/lifecycle.md)

Added `source.registry`, separate add/upgrade routes, bootstrap startup, desired-version-only materialization, retry limits, and local cleanup of superseded packages.

**Merged:** [gestalt#2868](https://github.com/valon-technologies/gestalt/pull/2868) · [gestalt#2878](https://github.com/valon-technologies/gestalt/pull/2878) · [gestalt#2879](https://github.com/valon-technologies/gestalt/pull/2879)

<a id="changelog-13"></a>

### 13 — Install-Time Validation

**Tags:** [`validation.md`](../architecture/validation.md)

Added typed admission checks for platform artifacts, `gestaltd` compatibility, declared dependencies, and reverse dependents. Failed validation leaves fleet state unchanged.

**Merged:** [gestalt#2885](https://github.com/valon-technologies/gestalt/pull/2885) · [gestalt#2889](https://github.com/valon-technologies/gestalt/pull/2889) · [gestalt#2887](https://github.com/valon-technologies/gestalt/pull/2887)

<a id="changelog-14"></a>

### 14 — Fleet Admin Observability

**Tags:** [`admin.md`](../operations/admin.md) [`lifecycle.md`](../operations/lifecycle.md)

Added authenticated app summaries, rollout and per-replica materialization APIs, and App Registry list/detail views in the embedded admin UI.

**Merged:** [gestalt#2886](https://github.com/valon-technologies/gestalt/pull/2886) · [gestalt#2890](https://github.com/valon-technologies/gestalt/pull/2890)

<a id="changelog-15"></a>

### 15 — App-Scoped Version Selection

**Tags:** [`admin.md`](../operations/admin.md) [`indexeddb.md`](../architecture/indexeddb.md) [`lifecycle.md`](../operations/lifecycle.md)

Added app-scoped authorization and APIs for first install, upgrade, and safe downgrade. Added `/apps/{app}/admin`, publication provenance, the snapshots table, and the production `g-issues` registry binding.

**Merged:** [gestalt#2897](https://github.com/valon-technologies/gestalt/pull/2897) · [gestalt#2909](https://github.com/valon-technologies/gestalt/pull/2909) · [gestalt#2914](https://github.com/valon-technologies/gestalt/pull/2914) · [gestalt-providers#1142](https://github.com/valon-technologies/gestalt-providers/pull/1142) · [gestalt-providers#1146](https://github.com/valon-technologies/gestalt-providers/pull/1146) · [toolshed#3696](https://github.com/valon-technologies/toolshed/pull/3696)

<a id="changelog-16"></a>

### 16 — Pending and Failed Publish Visibility

**Tags:** [`admin.md`](../operations/admin.md) [`models.md`](../architecture/models.md) [`pending-publish.md`](../operations/pending-publish.md)

Added `pending.json` and `failed.json`, CI lifecycle commands, early pending recording, app-admin API rows, publish duration, bootstrap polling, and the final status and last-update presentation.

**Registry and CI:** [gestalt#2927](https://github.com/valon-technologies/gestalt/pull/2927) · [gestalt#2932](https://github.com/valon-technologies/gestalt/pull/2932) · [gestalt#2931](https://github.com/valon-technologies/gestalt/pull/2931) · [toolshed#3772](https://github.com/valon-technologies/toolshed/pull/3772) · [toolshed#3775](https://github.com/valon-technologies/toolshed/pull/3775)

**App UI:** [gestalt-providers#1158](https://github.com/valon-technologies/gestalt-providers/pull/1158) · [gestalt-providers#1159](https://github.com/valon-technologies/gestalt-providers/pull/1159) · [gestalt-providers#1160](https://github.com/valon-technologies/gestalt-providers/pull/1160) · [gestalt-providers#1161](https://github.com/valon-technologies/gestalt-providers/pull/1161) · [gestalt-providers#1162](https://github.com/valon-technologies/gestalt-providers/pull/1162)

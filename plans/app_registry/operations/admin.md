# App Registry Administration

Operator-facing visibility for registry-only apps and app-scoped fleet version selection.

## Goals

Operators installing registry apps should answer these questions without reading pod logs or querying IndexedDB directly:

| Question                          | Admin surface                 |
| --------------------------------- | ----------------------------- |
| Which apps are registry-only?     | Embedded admin apps list      |
| What is the desired version?      | Desired version on app detail |
| Is the rollout still in progress? | Rollout phase stepper (`/apps/{app}/admin`); rollout badge (embedded `/admin`) |
| Which replicas have converged?    | Replica convergence table     |

App admins additionally select the fleet-wide desired version and inspect each candidate version's publication provenance.

---

## Terminology

Use the same names as [lifecycle.md](./lifecycle.md#runtime-version-invariants):

- **Fleet-known** — an accepted `(app, version)` projected from `app_version_change_requests`.
- **Desired version** — latest fleet-known version for an app (`LatestKnownVersion`).
- **Rollout** — fleet-wide execution record in `app_rollouts` (`enrolling` → `restarting` → `complete` | `failed`).
- **Target deployment** — Toolshed `SOURCE_VERSION` whose replicas determine the rollout outcome.
- **Converged** — the poller recorded `restarted_at` for the replica and version. Rollout accounting, not proof the provider is still running that version.

Label **converged** as rollout progress, not current runtime state.

---

## Embedded Admin UI (`/admin`)

Read-only fleet observability for registry-only apps. Requires global `gestaltAdmin`. API shapes: [lifecycle.md](./lifecycle.md#admin-observability-api).

The embedded shell at `/admin` keeps the Prometheus metrics viewer and adds an **App Registry** section. It does not install, upgrade, publish, or mutate rollouts.

### Navigation

```text
/admin
├── Metrics          (existing)
└── App Registry
    ├── Apps list
    └── App detail: {app}
```

### Apps List (`/admin/registry`)

| App      | Registry | Desired version     | Rollout  | Cohort        |
| -------- | -------- | ------------------- | -------- | ------------- |
| g-issues | toolshed | `0.0.0-snapshot.g…` | Complete | 3/3 restarted |

Show configured registry-only apps even when the fleet catalog is empty (desired version "—", status "not installed").

Auto-refresh every 10–15s while any listed rollout is non-terminal.

### App Detail (`/admin/registry/{app}`)

1. **Summary** — registry binding, desired version, latest published version (if available), install metadata (`installedBy`, `installedAt`).
2. **Rollout** — state badge, target deployment, target Cloud Run revision, timestamps, enrollment deadline, failure reason when `failed`.
3. **Replicas** — per-replica rollout progress (`instanceId`, `deploymentId`, `cloudRunRevision`, `inCohort`, acknowledged / materialized / stopped / restarted, `attemptCount`, last error). Group by deployment and sort by `instanceId`.

```text
┌─────────────────────────────────────────────────────────────┐
│ g-issues                                    rollout: Complete │
│ registry: toolshed                                            │
├─────────────────────────────────────────────────────────────┤
│ Desired: 0.0.0-snapshot.gcd9d741…      installed 2026-07-21 … │
│ Published latest: 0.0.0-snapshot.gcd9d741… (same)             │
├─────────────────────────────────────────────────────────────┤
│ Replicas — deployment 61885be… (target)          3/3 restarted │
│ instanceId                 mat.  restart  att  error           │
│ gestaltd-…-ncnq6           ✓     ✓        0                    │
│ gestaltd-…-hdnx2           ✓     ✓        0                    │
│ gestaltd-…-smmq7           ✓     ✓        0                    │
│ Deployment 574fe77… (superseded)                 1/3 restarted │
└─────────────────────────────────────────────────────────────┘
```

The apps-list denominator includes only target-deployment replicas. Superseded deployment rows remain on app detail so an operator can explain Cloud Run overlap without treating terminated old revisions as rollout failures.

---

## App Admin UI (`/apps/{app}/admin`)

Fleet version selection for one registry-only app. Requires `admin` on `app/{app}`. API shapes and admission checks: [lifecycle.md](./lifecycle.md#app-admin-version-selection).

Implemented in `gestalt-providers` (default `/apps` UI), not the embedded `/admin` shell.

### Capabilities

- **Manage app** on the `/apps` catalog when the caller can administer that app.
- Select a never-deployed version before `expiresAt` or a historical version before `expiresAt` as the fleet-wide desired version.
- Show per-version `publishedAt`, linked source commit, triggering PR or commit, and publishing workflow run.
- Show in-flight (**Publishing**) and recent failed (**Failed**) publishes with elapsed or total publish time. See [pending-publish.md](./pending-publish.md).
- Show the permanent, read-only deploy chain in a **Revision history** tab.
- Show whether historical versions are still redeployable or permanently locked.
- Legacy published versions without workflow metadata still link the commit and show **not recorded** for workflow/PR fields.
- Disable deploy actions while a rollout is `enrolling` or `restarting`; poll registry state every **3s** and refresh until rollout is terminal.
- Show rollout progress and the admitted version on the snapshots tab. See [Rollout phase stepper](#rollout-phase-stepper) and [Selected version row](#selected-version-row).
- Render access denied on **403** without leaking registry metadata.

Selection is fleet-wide. It is not per-user or per-replica.

### App Admin Page

The page header shows the app name, **App management** label, registry binding, and current **Desired version**. Two tabs separate deployment from audit:

- **Published snapshots** — pending, failed, and published entries in one newest-first list. A published row has **Deploy** when the version is **Available** or **Redeployable**.
- **Revision history** — accepted fleet version changes in reverse chronological order. This tab is always read-only.

See [pending-publish.md](./pending-publish.md) for snapshot merge rules, **3s** registry polling during bootstrap and active publish/rollout, and live **Publishing** duration labels.

```text
g-issues                                                                 App management
registry: toolshed

Desired version: 0.0.0-snapshot.gabc123

Published snapshots

Pull request     | Snapshot          | Status                  | Last update   | Action
---------------- | ----------------- | ----------------------- | ------------- | ------
PR #3740 · Title | 0.0.0-snapshot.g… | Publishing · for 4m     | 4 minutes ago | —
PR #3251 · Title | 0.0.0-snapshot.g… | Available               | Jul 22 15:00  | Deploy
                 |                   | Published in 4m 32s     |               |
PR #3200 · Title | 0.0.0-snapshot.g… | Deployed                | Jul 21 12:00  | —
PR #3100 · Title | 0.0.0-snapshot.g… | Redeployable · 12d left | Jul 18 09:00  | Deploy
PR #3000 · Title | 0.0.0-snapshot.g… | Locked                  | Jun 10 09:00  | —
PR #2900 · Title | 0.0.0-snapshot.g… | Expired                 | Jun 01 09:00  | —
```

Row timing labels: [pending-publish.md — Publish duration](./pending-publish.md#publish-duration). **Deploy** is unavailable on **Publishing**, **Failed**, **Deployed**, **Locked**, and **Expired** rows. **Deployed** marks the desired version. **Redeployable** shows the deadline or remaining duration.

### Rollout phase stepper

Replace the `Rollout {state}: {version}` text banner with a horizontal three-phase marker under the page header (above the tabs). Always visible on `/apps/{app}/admin`.

| Phase | `rollout.state` | Label |
| ----- | --------------- | ----- |
| 1 | `enrolling` | **Enrolling** |
| 2 | `restarting` | **Restarting** |
| 3 | `complete` | **Available** |
| 3 | `failed` | **Failed** |

Do **not** add a materialization phase. Per-replica artifact download during enrollment is part of phase 1; see [lifecycle.md](./lifecycle.md#rollout-admission-and-completion).

During `enrolling` or `restarting`, emphasize the current phase node, mark completed phases yellow, and leave future phases as yellow outline markers. Connector segments before the current phase are solid; later segments are muted.

When rollout is terminal or absent, show **Enrolling** and **Restarting** as yellow completed nodes. The rightmost node is **green** on `complete`, **red** on `failed`, or muted when the app has never been deployed.

```text
During restarting:

○----------------------●-------------------------○
Enrolling              Restarting                Available

Rollout complete:

○----------------------○-------------------------●
Enrolling              Restarting                Available

Rollout failed:

○----------------------○-------------------------●
Enrolling              Restarting                Failed
```

The stepper reads `registry.rollout.state` and `registry.rollout.version`. It does not duplicate replica-level materialization from the embedded `/admin` UI.

### Selected version row

Once an operator selects a version, keep a leading arrow (`→`) and row tint on that version through rollout and after it finishes. Target `registry.rollout.version` while a rollout exists; after terminal `complete` or `failed`, keep the affordance until a newer rollout replaces it. Do not use a **Rolling out** badge in the **Status** column.

| `rollout.state` | Row tint | Arrow | Action |
| --------------- | -------- | ----- | ------ |
| `enrolling`, `restarting` | Accent, slow pulse (**~2s** cycle) | Pulses with row | **Deploying...** (disabled) |
| `complete` | Solid success | Static | `—` |
| `failed` | Solid error | Static | **Deploy** when still selectable, otherwise `—` |

Deploy actions stay disabled on every row while rollout is active (`selectionDisabled` or local `deployingVersion`). Only the admitted version shows **Deploying...**.

During an active rollout:

```text
g-issues                                                                 App management
registry: toolshed

Desired version: 0.0.0-snapshot.gdef456

○----------------------●-------------------------○
Enrolling              Restarting                Available

Published snapshots

Pull request     | Snapshot          | Status    | Last update  | Action
---------------- | ----------------- | --------- | ------------ | ---------------
→ PR #3251 · …   | 0.0.0-snapshot.g… | Available | Jul 22 15:00 | Deploying...
PR #3200 · …     | 0.0.0-snapshot.g… | Deployed  | Jul 21 12:00 | —
```

(`→` and row tint pulse slowly during `enrolling` / `restarting`.)

After `complete`, the admitted row keeps a solid success tint and **Deployed** status. After `failed`, keep a solid error tint on the failed admission; the fleet may still run the previous desired version.

A **Failed** row in the same table:

```text
Pull request     | Snapshot          | Status           | Last update  | Action
---------------- | ----------------- | ---------------- | ------------ | ------
PR #3740 · Title | 0.0.0-snapshot.g… | Failed           | Jul 24 18:35 | —
                 |                   | Failed after 35m |              |
```

After a successful deploy selection, keep deploy actions disabled until the rollout reaches `complete` or `failed`.

### Revision History Tab

The Revision history tab displays `app_version_change_requests` as an immutable deploy ledger. It is not built from the deduplicated `knownVersions` projection: every accepted transition appears exactly once, including upgrades and downgrades that revisit an earlier version.

```text
Revision history

Deployed at  | Transition                    | Availability | Deployed by
------------ | ----------------------------- | ------------ | ---------------
Jul 25 09:10 | gdef456 → gabc123 (downgrade) | Current      | alice@valon.com
Jul 24 16:42 | gabc123 → gdef456 (upgrade)   | Redeployable | alice@valon.com
Jul 21 12:00 | First deployment → gabc123    | Current      | bob@valon.com
```

Each row links to the source commit and, when recorded, the triggering pull request and publish workflow. **Availability** shows the selected version's present-day state, so repeated entries for the same version have the same value. The current desired version is **Current**; other versions are **Redeployable** or **Locked**. The history tab itself has no deploy action because availability is an attribute of a version, not an individual event. Eligible selection remains on Published snapshots.

Load the newest page when the tab opens and paginate older entries with a cursor. An empty deploy chain renders **No deployments yet**. Registry retention permanently preserves the chain and version metadata. Artifacts for locked revisions may be pruned; see [retention.md](./retention.md).

---

## Out of Scope

- Per-replica observed running version (runtime heartbeats)
- Installing or upgrading from the embedded `/admin` UI
- Canceling or force-completing a rollout from either UI
- Publishing versions from either UI
- Granting or editing app authorization relationships
- Selecting a version for only one user or one replica
- Replacing `kubectl logs` for provider crash diagnostics

---

## Appendix

### Related Changelogs

<pre>
├── <a href="../project/changelog.md#changelog-14">14 — Fleet Admin Observability</a>
├── <a href="../project/changelog.md#changelog-15">15 — App-Scoped Version Selection</a>
├── <a href="../project/changelog.md#changelog-16">16 — Pending and Failed Publish Visibility</a>
├── <a href="../project/changelog.md#changelog-17">17 — Version Retention and Cleanup</a>
├── <a href="../project/changelog.md#changelog-18">18 — Revision History and Redeploy Windows</a>
└── <a href="../project/changelog.md#changelog-21">21 — Rollout Phase Stepper and Selected Version Row</a>
</pre>

### Related Docs

<pre>
├── <a href="../readme.md">readme.md</a> — architecture and future work
├── <a href="../project/changelog.md">changelog.md</a> — implementation milestones and pull requests
├── <a href="./lifecycle.md">lifecycle.md</a> — HTTP APIs, admission checks, and rollout behavior
├── <a href="../architecture/indexeddb.md">indexeddb.md</a> — app_rollouts, app_instance_materializations, change-request projections
├── <a href="./pending-publish.md">pending-publish.md</a> — in-flight publish visibility on app admin
├── <a href="./retention.md">retention.md</a> — version cleanup policy, redeploy windows, and revision-history preservation
└── <a href="../project/tests.md#admin-observability-tests">tests.md</a> — observability HTTP and UI tests; <a href="../project/tests.md#app-version-selection-and-revision-history-tests">app version selection and revision history tests</a>
</pre>

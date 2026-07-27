# App Registry Administration

Operator-facing visibility for registry-only apps and app-scoped fleet version selection.

## Goals

Operators installing registry apps should answer these questions without reading pod logs or querying IndexedDB directly:

| Question                          | Admin surface                 |
| --------------------------------- | ----------------------------- |
| Which apps are registry-only?     | Embedded admin apps list      |
| What is the desired version?      | Desired version on app detail |
| Is the rollout still in progress? | Rollout status badge (embedded `/admin`); rollout phase stepper (`/apps/{app}/admin`) |
| Which replicas have converged?    | Replica convergence table     |

App admins additionally select the fleet-wide desired version and inspect each candidate version's publication provenance.

---

## Terminology

Use the same names as [lifecycle.md](./lifecycle.md#runtime-version-invariants):

- **Fleet-known** — an accepted `(app, version)` projected from `app_version_change_requests`.
- **Desired version** — latest fleet-known version for an app (`LatestKnownVersion`).
- **Rollout** — fleet-wide execution record in `app_rollouts` (`enrolling` → `restarting` → `complete` | `failed`).
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
2. **Rollout** — state badge, timestamps, enrollment deadline, failure reason when `failed`.
3. **Replicas** — per-replica rollout progress (`instanceId`, acknowledged / materialized / stopped / restarted, `attemptCount`, last error). Sort by `instanceId`.

```text
┌─────────────────────────────────────────────────────────────┐
│ g-issues                                    rollout: Complete │
│ registry: toolshed                                            │
├─────────────────────────────────────────────────────────────┤
│ Desired: 0.0.0-snapshot.gcd9d741…      installed 2026-07-21 … │
│ Published latest: 0.0.0-snapshot.gcd9d741… (same)             │
├─────────────────────────────────────────────────────────────┤
│ Replicas                                                      │
│ instanceId                 mat.  restart  att  error          │
│ gestaltd-…-ncnq6           ✓     ✓        0                   │
│ gestaltd-…-hdnx2           ✓     ✓        0                   │
│ gestaltd-…-smmq7           ✓     ✓        0                   │
└─────────────────────────────────────────────────────────────┘
```

---

## App Admin UI (`/apps/{app}/admin`)

Fleet version selection for one registry-only app. Requires `admin` on `app/{app}`. API shapes and admission checks: [lifecycle.md](./lifecycle.md#app-admin-version-selection).

Implemented in `gestalt-providers` (default `/apps` UI), not the embedded `/admin` shell.

### Capabilities

- **Manage app** on the `/apps` catalog when the caller can administer that app.
- Select a never-deployed version before its unused-retention deadline or a historical version inside its redeploy window as the fleet-wide desired version.
- Show per-version `publishedAt`, linked source commit, triggering PR or commit, and publishing workflow run.
- Show in-flight (**Publishing**) and recent failed (**Failed**) publishes with elapsed or total publish time. See [pending-publish.md](./pending-publish.md).
- Show the permanent, read-only deploy chain in a **Revision history** tab.
- Show whether historical versions are still redeployable or permanently locked.
- Legacy published versions without workflow metadata still link the commit and show **not recorded** for workflow/PR fields.
- Disable deploy actions while a rollout is `enrolling` or `restarting`; poll registry state every **3s** and refresh until rollout is terminal.
- Show rollout progress as a three-phase stepper at the top of the page (`enrolling` → `restarting` → `available` | `failed`). Materialization is not a separate phase; it runs during enrollment. See [Rollout phase stepper](#rollout-phase-stepper).
- Highlight the rollout target snapshot row with a tint and leading arrow instead of a **Rolling out** status badge. The affordance persists after rollout completes. See [Deploying row affordance](#deploying-row-affordance).
- Render access denied on **403** without leaking registry metadata.

Selection is fleet-wide. It is not per-user or per-replica.

### App Admin Page

The page header shows the app name, **App management** label, registry binding, and current **Desired version**. Two tabs separate deployment from audit:

- **Published snapshots** — pending, failed, and published entries in one newest-first list. A published row has **Deploy** when it is never deployed and before `publishedAt + unusedRetention`, or historical and before `deployableUntil`.
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

Row timing labels: [pending-publish.md — Publish duration](./pending-publish.md#publish-duration). **Deploy** is unavailable on **Publishing**, **Failed**, **Deployed**, **Locked**, and **Expired** rows. **Deployed** marks the desired version. **Redeployable** shows the fixed deadline or remaining duration.

### Rollout Phase Stepper

Replace the current `Rollout {state}: {version}` text banner with a horizontal three-phase marker directly under the page header (above the tabs). The stepper is always visible on `/apps/{app}/admin`.

Map backend rollout state to three operator-facing phases:

| Phase | Backend `rollout.state` | Label |
| --- | --- | --- |
| 1 | `enrolling` | **Enrolling** |
| 2 | `restarting` | **Restarting** |
| 3 | `complete` | **Available** |
| 3 | `failed` | **Failed** |

Do **not** add a fourth phase for materialization. Per-replica artifact download during enrollment is part of phase 1; see [lifecycle.md](./lifecycle.md#rollout-admission-and-completion).

#### Layout

```text
o----------------------o-------------------------o
Enrolling              Restarting                Available
```

When `rollout.state` is `failed`, the rightmost label reads **Failed** instead of **Available**.

#### Active rollout (`enrolling` or `restarting`)

- The **current** phase node is emphasized (filled accent).
- **Future** phases use yellow circle markers.
- **Completed** phases within the same rollout use yellow filled markers.
- Connector segments before the current phase are solid; segments after it are muted.

```text
During enrolling:

●----------------------○-------------------------○
Enrolling              Restarting                Available

During restarting:

○----------------------●-------------------------○
Enrolling              Restarting                Available
```

#### Terminal idle (no active rollout)

When rollout is `complete` or `failed`, or no rollout exists yet:

- **Enrolling** and **Restarting** nodes are yellow (completed path).
- The rightmost node is **green** when the latest rollout is `complete`, or **red** when it is `failed`.
- When the app has never been deployed, all three nodes are muted and the rightmost label stays **Available**.

```text
Rollout complete:

○----------------------○-------------------------●
Enrolling              Restarting                Available

Rollout failed:

○----------------------○-------------------------●
Enrolling              Restarting                Failed
```

The stepper reads `registry.rollout.state` and `registry.rollout.version`. It does not duplicate replica-level materialization progress from the embedded `/admin` UI.

### Deploying Row Affordance

Once an operator selects a version, keep a leading arrow and row tint on that version through rollout and after it finishes. The target is `registry.rollout.version` while a rollout exists; after terminal `complete` or `failed`, keep highlighting the same version until a newer rollout replaces it.

Do **not** add a **Rolling out** badge in the **Status** column. Use row-level affordance instead.

#### Visual states

| Rollout state | Row tint | Leading arrow | Action column |
| --- | --- | --- | --- |
| `enrolling` or `restarting` | Accent tint with a **slow pulse** (~1.5–2s cycle; do not flash faster than ~1 Hz) | Visible, pulses with the row | Disabled **Deploying...** |
| `complete` | Solid success tint (no animation) | Visible, static | `—` (**Deployed**) |
| `failed` | Solid error/destructive tint (no animation) | Visible, static | **Deploy** when the version is still selectable, otherwise `—` |

Deploy actions stay disabled on every row while rollout is active (`selectionDisabled` or local `deployingVersion`). Only the rollout target shows **Deploying...**; other rows show **Deploy** disabled or `—` without that label.

The pulse should be noticeable but calm — enough to draw attention during an in-flight rollout without feeling like an alarm.

#### During active rollout

```text
Published snapshots

→ Pull request     | Snapshot          | Status    | Last update  | Action
------------------ | ----------------- | --------- | ------------ | ---------------
  PR #3251 · Title | 0.0.0-snapshot.g… | Available | Jul 22 15:00 | Deploying...
  PR #3200 · Title | 0.0.0-snapshot.g… | Deployed  | Jul 21 12:00 | —
```

(`→` and row background pulse slowly during `enrolling` / `restarting`.)

#### After successful rollout

Keep the arrow and a **solid** success tint on the version that was just admitted. **Status** becomes **Deployed**.

```text
→ Pull request     | Snapshot          | Status   | Last update  | Action
------------------ | ----------------- | -------- | ------------ | ------
  PR #3251 · Title | 0.0.0-snapshot.g… | Deployed | Jul 22 15:00 | —
  PR #3200 · Title | 0.0.0-snapshot.g… | Available| Jul 21 12:00 | Deploy
```

#### After failed rollout

Keep the arrow and apply a **solid error tint** on the version that failed to roll out.

```text
→ Pull request     | Snapshot          | Status      | Last update  | Action
------------------ | ----------------- | ----------- | ------------ | ------
  PR #3251 · Title | 0.0.0-snapshot.g… | Available   | Jul 22 15:00 | Deploy
  PR #3200 · Title | 0.0.0-snapshot.g… | Deployed    | Jul 21 12:00 | —
```

The fleet may still be running the previous desired version; the highlighted row shows which admission attempt failed.

#### Active rollout (full page)

```text
g-issues                                                                 App management
registry: toolshed

Desired version: 0.0.0-snapshot.gdef456

○----------------------●-------------------------○
Enrolling              Restarting                Available

Published snapshots

→ Pull request     | Snapshot          | Status    | Last update  | Action
------------------ | ----------------- | --------- | ------------ | ---------------
  PR #3251 · Title | 0.0.0-snapshot.g… | Available | Jul 22 15:00 | Deploying...
  PR #3200 · Title | 0.0.0-snapshot.g… | Deployed  | Jul 21 12:00 | —
```

When the operator selects another version, move the arrow and tint to the new rollout target and reset the prior row to the default background.

Replace the prior wireframe that showed `Rollout Enrolling: …` above the table and a **Rolling out** status badge.

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
└── <a href="../project/changelog.md#changelog-21">21 — Rollout Phase Stepper and Deploying Row Affordance</a> (planned)
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

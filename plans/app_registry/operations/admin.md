# App Registry Administration

Operator-facing visibility for registry-only apps and app-scoped fleet version selection.

## Goals

Operators installing registry apps should answer these questions without reading pod logs or querying IndexedDB directly:

| Question                            | Admin surface                                                           |
| ----------------------------------- | ----------------------------------------------------------------------- |
| Which apps are registry-only?       | Embedded admin apps list                                                |
| What is the desired version?        | Desired version on app detail                                           |
| Is the rollout still in progress?   | Rollout phase stepper (`/apps/{app}/admin`); badge (embedded `/admin`)   |
| Is the current fleet healthy?       | Heartbeat-derived fleet status on both admin surfaces                   |
| What is each live replica serving?  | Fresh/stale replica observations on embedded app detail                 |
| How far did enrollment convergence get? | Replica pool totals and rollout-progress API                        |

App admins additionally select the fleet-wide desired version and inspect each candidate version's publication provenance.

---

## Terminology

Use the same names as [lifecycle.md](./lifecycle.md#runtime-version-invariants):

- **Fleet-known** — an accepted `(app, version)` projected from `app_version_change_requests`.
- **Desired version** — latest fleet-known version for an app (`LatestKnownVersion`).
- **Rollout** — fleet-wide execution record in `app_rollouts` (`enrolling` → `restarting` → `complete` | `failed`).
- **Target source version** — Toolshed `SOURCE_VERSION` whose replicas determine the rollout outcome.
- **Converged** — the poller recorded `restarted_at` for the replica and version. Rollout accounting, not proof the provider is still running that version.
- **Current fleet state** — a read-time projection over fresh heartbeats for the activated source and desired version.
- **Recovery** — an immutable observation that a failed desired-version rollout later became stably healthy.

Label **converged** as rollout progress, not current runtime state. Never relabel a failed rollout as complete because the current fleet later becomes healthy.

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

| App      | Registry | Desired version     | Current fleet | Rollout  | Cohort        |
| -------- | -------- | ------------------- | ------------- | -------- | ------------- |
| g-issues | toolshed | `0.0.0-snapshot.g…` | Healthy · 5/5 | Complete | 3/3 restarted |

Show configured registry-only apps even when the fleet catalog is empty (desired version "—", status "not installed").

Current fleet is `healthy` only when the live count meets the activated minimum and every fresh current-source replica reports the desired version. Show `converging`, `degraded`, or `unknown` independently from the rollout badge; insufficient fresh replicas is `unknown`, never healthy.

### App Detail (`/admin/registry/{app}`)

1. **Summary** — registry binding, desired version, latest published version (if available), install metadata (`installedBy`, `installedAt`).
2. **Current fleet** — health, current source, minimum/live/running counts, heartbeat lease, and evaluation time.
3. **Runtime replicas** — fresh observations first, with instance ID, source version, heartbeat age, state, running version, and error. Show expired and superseded-source rows separately as stale diagnostics.
4. **Rollout** — execution status, target source, and timestamps. Keep its terminal historical outcome visually distinct from current fleet health.
5. **Replica pool** — enrollment cohort totals for rollout-progress accounting. The API retains per-replica materialization details; the UI's per-replica table is the current runtime heartbeat view.

```text
┌─────────────────────────────────────────────────────────────┐
│ g-issues                              current fleet: Healthy │
│ registry: toolshed                                            │
├─────────────────────────────────────────────────────────────┤
│ Desired: 0.0.0-snapshot.gcd9d741…      installed 2026-07-21 … │
│ Published latest: 0.0.0-snapshot.gcd9d741… (same)             │
├─────────────────────────────────────────────────────────────┤
│ Source 61885be… · 3/3 live · lease 45s                        │
│ instanceId                 age   runtime   running version     │
│ gestaltd-…-ncnq6           4s    Running   gcd9d741…           │
│ gestaltd-…-hdnx2           9s    Running   gcd9d741…           │
│ gestaltd-…-smmq7           12s   Running   gcd9d741…           │
│ Stale: gestaltd-…-old · superseded source · process exited     │
└─────────────────────────────────────────────────────────────┘
```

Freshness is based on the configured lease, not the browser refresh time. Superseded-source and expired rows remain on app detail so an operator can explain Cloud Run overlap and replacement without counting them toward current health.

---

## App Admin UI (`/apps/{app}/admin`)

Fleet version selection for one registry-only app. Requires `admin` on `app/{app}`. API shapes and admission checks: [lifecycle.md](./lifecycle.md#app-admin-version-selection).

Implemented in `gestalt-providers` (default `/apps` UI), not the embedded `/admin` shell.

### Capabilities

- **Manage app** on the `/apps` catalog when the caller can administer that app.
- Select a never-deployed version before `expiresAt` or a historical version before `expiresAt` as the fleet-wide desired version.
- Show per-version `publishedAt`, linked source commit, triggering PR or commit, and publishing workflow run.
- Show in-flight (**Publishing**) and recent failed (**Failed**) publishes with elapsed or total publish time. See [pending-publish.md](./pending-publish.md).
- Show the permanent, read-only deploy chain in a **Revision history** tab with rollout status and duration on each admission.
- Show whether historical versions are still redeployable or permanently locked.
- Legacy published versions without workflow metadata still link the commit and show **not recorded** for workflow/PR fields.
- Disable deploy actions while a rollout is `enrolling` or `restarting`; poll registry state every **3s** and refresh until rollout is terminal.
- Show rollout progress and the admitted version on the snapshots tab. See [Rollout phase stepper](#rollout-phase-stepper) and [Selected version row](#selected-version-row).
- Show **Current fleet** separately from the rollout stepper, including minimum/live counts and source version. A healthy fleet after failure renders **Recovered after failed rollout** while the stepper remains **Failed**.
- Toggle **Automatically deploy new snapshots** on `/apps/{app}/admin`. When enabled, new published snapshots are admitted across the fleet without a manual **Deploy** click. See [Auto-deploy toggle](#auto-deploy-toggle).
- Render access denied on **403** without leaking registry metadata.

Selection is fleet-wide. It is not per-user or per-replica.

### App Admin Page

The page header shows the app name, **App management** label, registry binding, and current **Desired version**. Two tabs separate deployment from audit:

- **Published snapshots** — pending, failed, and published entries in one newest-first list. A published row has **Deploy** when the version is **Available** or **Redeployable**.
- **Revision history** — accepted fleet version changes in reverse chronological order with rollout status and duration. This tab is always read-only.

See [pending-publish.md](./pending-publish.md) for snapshot merge rules, **3s** registry polling during bootstrap and active publish/rollout, and live **Publishing** duration labels.

### Current Fleet and Recovery

The page header consumes `fleetState` from `GET /api/v1/apps/{app}/admin/registry` and renders it independently from `rollout`:

```text
Current fleet: Healthy · 5/5 running gcd9d741… on 61885be…
Last rollout: Failed after 15m · Recovered Jul 30 at 13:52
```

- `healthy` means every fresh current-source replica reports the desired version and the activated minimum capacity is present.
- `unknown` covers a missing source/minimum basis or too few fresh replicas.
- `degraded` covers fresh errors, unknown observations, missing apps, or version mismatches; `converging` applies while a matching rollout is active before its deadline.
- Recovery timing comes from `app_version_recovery_observations`. It does not alter the failed rollout row or its duration.
- During mixed-version deploys, older clients may omit these fields; the UI keeps rollout presentation functional and waits for the next poll.

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

### Auto-deploy toggle

Fleet-wide automatic admission when a new snapshot is published. API and coalescing behavior: [lifecycle.md](./lifecycle.md#auto-deploy-published-snapshots).

- **Automatically deploy new snapshots** — toggle with an explicit **On** / **Off** label beside the switch.
- When **On**, the Published snapshots description explains that new snapshots are admitted without manual deploy. Manual **Deploy** actions are disabled while auto-deploy is enabled.
- When **Off**, manual deploy remains available subject to rollout and retention rules.
- Show `autoDeploy.lastError` under the toggle until an app admin re-enables auto-deploy or deploys manually. Auto-deploy turns **Off** automatically on rollout `failed`.
- During an active rollout, show **Queued for deploy** on the snapshot row for `autoDeploy.pendingVersion`. This is distinct from **Deploying...** on the admitted version.
- Revision history shows `system:auto-deploy` for automatic admissions.

While auto-deploy is on and a newer snapshot is waiting behind an active rollout:

```text
Pull request     | Snapshot          | Status              | Action
---------------- | ----------------- | ------------------- | ------
PR #3872 · Title | 0.0.0-snapshot.g… | Queued for deploy   | —
→ PR #3860 · …   | 0.0.0-snapshot.g… | Deploying...        | —
```

### Revision History Tab

The Revision history tab displays `app_version_change_requests` as an immutable deploy ledger. It is not built from the deduplicated `knownVersions` projection: every accepted transition appears exactly once, including upgrades and downgrades that revisit an earlier version.

Fleet admission appends a change request immediately, but the version is not fleet-available until rollout reaches `complete`. Each row shows rollout status and duration labels that mirror the **Publishing** / **Published in** pattern on Published snapshots. See [pending-publish.md](./pending-publish.md#publish-duration) and [lifecycle.md](./lifecycle.md#revision-history).

| Row status | Start | End | Status label |
| --- | --- | --- | --- |
| **Rolling out** | `deployedAt` | `liveNow` (client clock, 1s) | `Rolling out` + `for 2m 14s` |
| **Available** | `deployedAt` | `rolloutCompletedAt` | `Available in 3m 08s` |
| **Failed** | `deployedAt` | `rolloutFailedAt` | `Failed after 12m 41s` |

```text
Revision history

Deployed at  | Transition                    | Status                    | Deployed by
------------ | ----------------------------- | ------------------------- | ---------------
2 minutes ago| gabc123 → gdef456 (upgrade)   | Rolling out · for 2m 14s  | alice@valon.com
Jul 24 16:42 | gdef456 → gabc123 (downgrade) | Available in 3m 08s       | alice@valon.com
Jul 21 12:00 | First deployment → gabc123    | Available in 1m 52s       | bob@valon.com
```

Each row links to the source commit and, when recorded, the triggering pull request and publish workflow. The newest revision row shows **Rolling out** while `registry.rollout` is `enrolling` or `restarting` for the current admission. Older rows for the same version omit live rollout decoration. Terminal durations come from the `app_version_rollout_outcomes` sidecar when present. Automatic admissions show **system:auto-deploy** as the deployer; rollout status is independent of actor.

Enable `useLiveNow` while the history table has at least one in-flight rollout row so **Rolling out** durations advance between registry polls. The parent registry query already polls every **3s** during active rollout; the history query does not need its own interval.

Load the newest page when the tab opens and paginate older entries with a cursor. An empty deploy chain renders **No deployments yet**. Registry retention permanently preserves the chain and version metadata. Artifacts for locked revisions may be pruned; see [retention.md](./retention.md).

Legacy revisions admitted before rollout-outcome persistence omit rollout status and duration; `deployedAt` only.

---

## Out of Scope

- Installing or upgrading from the embedded `/admin` UI
- Canceling or force-completing a rollout from either UI
- Publishing versions from either UI
- Granting or editing app authorization relationships
- Selecting a version for only one user or one replica
- Automatic admission for pending or failed publishes
- Cancelling an in-flight rollout to jump to a newer published version
- Automatic rollback on rollout failure
- Notifications on validation failure or rollout `failed`
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
├── <a href="../project/changelog.md#changelog-21">21 — Rollout Phase Stepper and Selected Version Row</a>
├── <a href="../project/changelog.md#changelog-25">25 — Auto-Deploy Published Snapshots</a>
├── <a href="../project/changelog.md#changelog-26">26 — Revision History Rollout Visibility</a>
└── <a href="../project/changelog.md#changelog-27">27 — Runtime Heartbeats and Fleet State</a>
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

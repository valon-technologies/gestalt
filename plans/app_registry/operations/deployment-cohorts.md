# Deployment-Scoped Rollout Cohorts

Proposed replacement for fleet-wide replica enrollment when `gestaltd` runs on Cloud Run.

## Problem

The current rollout cohort contains every process that acknowledges an app version before `enrollment_ends_at`. This assumes the set of processes remains stable until each member records `restarted_at`.

A Cloud Run deployment violates that assumption:

1. The serving revision has five instances.
2. Toolshed deploys a candidate revision with five minimum instances.
3. Both revisions run the catalog poller against shared IndexedDB.
4. An app rollout opens while both revisions exist, so ten processes acknowledge.
5. Cloud Run moves traffic to the candidate and terminates the old revision.
6. Terminated old-revision processes cannot record `restarted_at`.

The app can be healthy on every instance of the promoted revision while the rollout reports `5/10` or `6/10 restarted` and eventually becomes `failed`.

Extending the deadline does not solve the problem: the missing processes were intentionally removed and will never converge. Capping the cohort at the configured minimum instance count is also unsafe because Cloud Run instances have no stable ordinal and the service can autoscale.

## Decision

Track convergence by **deployment cohort**, not across every process that overlaps the enrollment window.

A deployment cohort is all `gestaltd` processes running the same Toolshed deployment. In production its stable identity is the Toolshed commit SHA already injected as:

```text
SOURCE_VERSION=${{ github.sha }}
```

Call this value `deployment_id`. Record `K_REVISION` separately as diagnostic metadata, but do not use it as the primary identity: Cloud Run can create another revision for the same Toolshed image or configuration, while the desired app compatibility boundary is the deployed Toolshed build.

Outside Toolshed, use an explicit `GESTALT_DEPLOYMENT_ID`. `K_REVISION` is an acceptable Cloud Run fallback. Local development may use a process-local value because it does not coordinate a production fleet.

## Target Deployment

An app rollout has one `target_deployment_id`. Only materialization rows with that deployment ID are members of the rollout cohort and participate in its terminal outcome.

Do not derive the target from the replica that handles the version-selection HTTP request. During traffic migration an old revision can still handle an in-flight request. If that replica selected its own deployment ID, the rollout could target the revision Cloud Run is removing.

Toolshed deployment orchestration is the authority for the target:

1. Resolve the candidate Cloud Run revision and Toolshed SHA.
2. After the candidate passes readiness, publish `{ deployment_id, cloud_run_revision, state: "candidate" }` to shared deployment state.
3. Shift 100% traffic to the candidate and activate it.
4. Atomically promote that record to `state: "current"`.
5. App rollout admission snapshots the current `deployment_id` into `app_rollouts.target_deployment_id`.

While deployment state is changing between candidate creation and promotion, version selection should return **409 Conflict** (`gestaltd deployment in progress`). This closes the small interval in which an old serving revision could admit a rollout immediately before promotion. Publishing registry artifacts remains allowed; only selecting the fleet desired version is gated.

If a new deployment is promoted while an app rollout is already active, move the active rollout target to the newly current deployment and start a fresh enrollment window for that deployment. Rows from the superseded deployment remain available for diagnostics but no longer block the outcome. The desired app version does not change.

## Data Model

### Deployment state

Store one shared current/candidate record:

```json
{
  "id": "gestaltd",
  "current_deployment_id": "574fe7704ed67fc15d44f76698755bb94ad33d43",
  "current_cloud_run_revision": "valon-tools-api-01234-abc",
  "candidate_deployment_id": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
  "candidate_cloud_run_revision": "valon-tools-api-01235-def",
  "state": "promoting",
  "updated_at": "2026-07-27T22:30:00Z"
}
```

The Toolshed deploy workflow owns candidate/promotion writes. `gestaltd` instances may verify that their local `SOURCE_VERSION` and `K_REVISION` match the record, but must not race to elect themselves current.

### `app_rollouts`

Add:

```json
{
  "target_deployment_id": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
  "target_cloud_run_revision": "valon-tools-api-01235-def"
}
```

`target_cloud_run_revision` is optional diagnostic metadata. Completion logic keys on `target_deployment_id`.

### `app_instance_materializations`

Add immutable process metadata when acknowledging:

```json
{
  "deployment_id": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
  "cloud_run_revision": "valon-tools-api-01235-def"
}
```

Keep `instance_id` unique per process. Do not replace it with the deployment ID; doing so would collapse five replicas into one row and falsely report fleet convergence after a single process restarts.

For repeated deployment of the same app version, reset progress using both the rollout creation epoch and target deployment ID.

## Enrollment and Completion

For an active rollout:

1. Every poller reads the rollout target.
2. A process whose local `deployment_id` differs from `target_deployment_id` still reconciles the durable desired app version, but its row is diagnostic and `inCohort` is false.
3. Matching processes acknowledge and materialize during enrollment.
4. After enrollment closes, matching processes restart the app.
5. Mark the rollout `complete` when every enrolled row for the target deployment records a current `restarted_at`.
6. Mark it `failed` when at least one enrolled target-deployment row misses the deadline.

Require at least one target-deployment acknowledgement before completion. A missing target cohort remains active until the deadline and then fails.

Never declare success because *any* deployment cohort completed. The old revision could converge while the promoted revision fails; success must be tied to the deployment Toolshed marked current.

Late target-deployment replicas continue to converge from the durable change request without reopening a terminal rollout, matching the existing late-replica behavior.

## Administration

Rollout summaries expose the target:

```json
{
  "rollout": {
    "version": "0.0.0-snapshot.gabc123",
    "state": "restarting",
    "targetDeploymentId": "61885becf49a25a4a8c0063a4d9dd9643b28c2a6",
    "targetCloudRunRevision": "valon-tools-api-01235-def"
  },
  "cohort": {
    "acknowledged": 5,
    "materialized": 5,
    "restarted": 5,
    "failed": 0
  }
}
```

The app list denominator counts only target-deployment members. The detail table shows all rows and adds **Deployment**, **Cloud Run revision**, and **In cohort** so operators can explain overlap:

```text
Deployment 574fe77… (superseded)   1/5 restarted
Deployment 61885be… (target)       5/5 restarted
```

A terminal `complete` badge means the selected app version converged on the target Toolshed deployment. It does not assert that terminated or superseded deployments restarted the app.

## Failure and Rollback

- If candidate promotion fails, Toolshed restores the previous deployment as current. An active app rollout retargets that deployment with a new enrollment epoch.
- If the target deployment is deleted before acknowledging, the rollout fails at its deadline with `target deployment unavailable`.
- If the app fails on one target replica, normal reconciliation attempts and the existing deadline behavior apply.
- If Toolshed rolls back after the app rollout completed, the restored deployment still reads the durable desired version and converges locally. A future enhancement may create an explicit verification rollout for deployment rollback.

## Tests

Add a multi-deployment poller test with five old and five candidate instances:

1. Both deployment cohorts acknowledge the same app version.
2. Promote the candidate deployment.
3. Only candidate rows have `inCohort: true`.
4. Candidate convergence produces `5/5 restarted` and `complete`.
5. Missing restarts from the old deployment do not fail the rollout.

Also cover:

- an old revision handling a request cannot choose itself as target
- no target acknowledgements cannot complete
- a completed old cohort cannot hide failure of the target cohort
- promotion during an active rollout retargets with a fresh enrollment epoch
- rollback restores the previous deployment target
- `SOURCE_VERSION` takes precedence over `K_REVISION`
- instance IDs remain distinct within one deployment


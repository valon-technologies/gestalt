# Gestalt App Preview Environments

## Decision

Create one IAM-protected Cloud Run `gestaltd` service per approved internal PR. Run branch backend code there with isolated database and workflow state. Keep the frontend local with normal HMR.

CI/CD is the first pilot, but the interface and deployment model must work for any Gestalt app. Do not use production Cloud Run revisions or reverse-provider shadowing because both risk production routing or shared state.

## Developer experience

Add a generic Gestalt CLI command:

```bash
gestalt app preview <app-path> --pr <number>
```

For example:

```bash
gestalt app preview ./valon-tools/apps/ci-cd --pr 123
```

The command reads the app manifest, verifies that the remote preview matches the repository, PR, and commit, obtains the developer's Gestalt bearer and Cloud Run identity, then starts the app's local UI command on loopback. Browser API requests are proxied to the remote branch backend; credentials remain server-side.

```text
Browser → local Gestalt UI proxy → PR Cloud Run gestaltd → branch provider
                                                     ├─ isolated database/workflows
                                                     └─ allowlisted production reads
```

## Safety requirements

- Previews never change production traffic, provider selection, app-registry state, authorization state, databases, or Temporal.
- Every preview has a unique service, runtime identity, database credential, encryption key, labels, and expiry.
- Artifacts are pinned by full commit SHA and verified digest.
- PR code packages without cloud credentials. Trusted default-branch automation validates the artifact before authenticating and deploying.
- The management listener and `/activate` are not public, and `authorizationStateApply` is false.
- The developer bearer is forwarded only to production identity and authorization.
- Dependency access uses a separate preview relay with exact read-operation allowlists and no write, impersonation, secret-reading, or token-minting authority.
- Production database, Temporal, and `service_account:ci-cd-sync` credentials are unavailable to previews.
- Workflow definitions and activations are paused before first reconciliation; authorized manual runs remain possible.
- Only approved internal PRs are eligible. Close cleanup and a maximum-age sweeper remove all resources.

Branch providers are arbitrary code beside `gestaltd`, not a hardened sandbox. Preview runtime and relay permissions must therefore be safe even if branch code can exercise them.

## Implementation

### 1. Gestalt primitives

- Add a `forwardBearer` remote-auth mode in `gestaltd/internal/config/remotes.go` and `gestaltd/internal/remote/remote.go`. Preserve static-token remotes for dependencies and test concurrent callers, validation, and redaction.
- Add `apps.<name>.workflowPolicy.forcePaused` and enforce it in `gestaltd/internal/bootstrap/workflow_app_definitions.go` before reconciliation.
- Extend `sdk/typescript-web/src/vite.js` to send the Gestalt bearer in `Authorization` and a refreshable Cloud Run token in `X-Serverless-Authorization`.
- Add `gestalt app preview <app-path> --pr <number>` to the Go CLI. It must be app-agnostic and use manifest `run` commands rather than app-specific scripts.

### 2. Secure preview deployment

Split Toolshed publication into an unprivileged PR packaging job and a trusted deployment job that validates checksums, provenance, app identity, and source SHA before obtaining GCP credentials.

Add reusable preview config, Terraform, deploy, update, close-cleanup, and TTL-sweeper workflows. Deploy one service per app and PR with min instances zero, max one, IAM-only invocation, isolated storage, and structured labels. Do not reuse or modify the production deployment workflow.

### 3. CI/CD pilot

Add a preview-only admin operation:

```text
preview.materializePullRequest({ prNumber })
```

It reads one Front Porch PR plus available checks, deployments, and Linear data through the read-only preview relay; reuses existing normalization and correlation code; writes only preview storage; and returns intermediate and final JSON. It must be idempotent and absent from production `allowedOperations`.

Install CI/CD workflow definitions in preview-local storage with automatic activations paused. Expose generic CLI commands to start a run manually and inspect events and output.

### 4. Generalize

After CI/CD succeeds, define a reusable preview manifest for dependency allowlists, storage, workflow policy, and health checks. Keep app-specific materialization inside each app. Validate the design with a second read-oriented app before broader self-service.

## Verification and success

Automated tests cover remote caller auth, token isolation, forced-paused workflows, dual proxy headers, artifact provenance, storage isolation, deterministic cleanup, and CI/CD preview authorization/idempotency.

The end-to-end pilot must attach the local CI/CD UI with the generic CLI, materialize one PR, verify admin and non-admin behavior, manually run a workflow, update and reset preview state, and delete all resources on PR close. Production routing and state must remain unchanged.

The work succeeds when an approved preview is ready within 10 minutes, any app can attach with one generic command, frontend changes remain immediate, backend and workflow output are useful for debugging, and teardown is deterministic.

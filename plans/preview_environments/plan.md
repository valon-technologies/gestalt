# Gestalt App Preview Environments

## Decision

Create one IAM-protected Cloud Run `gestaltd` service per approved internal PR. Run branch backend code there with isolated database and workflow state. Keep the frontend local with normal HMR.

CI/CD is the first pilot, but the interface and deployment model must work for any Gestalt app. Do not use production Cloud Run revisions or reverse-provider shadowing because both risk production routing or shared state.

## Developer experience

Extend the existing source-app runtime:

```bash
gestaltd serve <app-path> --remote-preview <url>
```

For example:

```bash
gestaltd serve ./valon-tools/apps/ci-cd --remote-preview https://gestalt-preview-ci-cd-pr-123.example
```

The trusted deployment workflow publishes the exact command for its commit. `gestaltd serve` remains the single owner of manifests, local `run` commands, HMR, credentials, and proxy lifecycle. In remote-preview mode it starts only the manifest command explicitly marked as the UI target plus a trusted loopback proxy; ambiguous manifests fail validation instead of relying on command heuristics.

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
- Dedicated identity and authorization connectors forward the developer bearer only to those upstream services.
- Dependency access uses a separate preview relay with exact read-operation allowlists and no write, impersonation, secret-reading, or token-minting authority.
- Production database, Temporal, and `service_account:ci-cd-sync` credentials are unavailable to previews.
- The workflow manager blocks every automatic activation; authorized manual runs remain possible.
- Only approved internal PRs are eligible. Close cleanup and a maximum-age sweeper remove all resources.

Branch providers are arbitrary code beside `gestaltd`, not a hardened sandbox. Preview runtime and relay permissions must therefore be safe even if branch code can exercise them.

## Implementation

### 1. Gestalt primitives

- Add dedicated remote identity and authorization connectors that forward the incoming bearer from private request context. Do not add bearer forwarding to generic remotes. Test concurrent callers, validation, and redaction.
- Add a workflow-manager execution policy such as `server.workflows.activationPolicy: manualOnly`. Keep declared schedules visible, block every automatic activation at the canonical execution boundary, and preserve authorized manual starts.
- Add an explicit `role: ui` to manifest `run` entries. Remote-preview mode requires exactly one UI target; normal `gestaltd serve` remains backward compatible with existing untyped run lists.
- Add `gestaltd serve <app-path> --remote-preview <url>`. A trusted loopback proxy owned by `gestaltd` refreshes Cloud Run identity and injects both Cloud Run and Gestalt authorization headers. Existing Vite integration targets this proxy unchanged.

### 2. Secure preview deployment

Split Toolshed publication into an unprivileged PR packaging job and a trusted deployment job that validates checksums, provenance, app identity, and source SHA before obtaining GCP credentials.

Add reusable preview config, Terraform, deploy, update, close-cleanup, and TTL-sweeper workflows. Deploy one service per app and PR with min instances zero, max one, IAM-only invocation, isolated storage, and structured labels. Do not reuse or modify the production deployment workflow.

### 3. CI/CD pilot

Extend the canonical CI/CD ingestion workflow to accept an optional bounded PR scope:

```text
sync({ pullRequestNumbers: [123] })
```

Scheduled production runs omit the scope and retain current behavior. A manual preview run uses the same orchestration, normalization, correlation, and persistence path for one PR. Isolation comes from preview storage and relay policy, not a preview-only application contract. The run must be idempotent and return inspectable intermediate and final JSON.

Install the normal CI/CD definitions in preview-local storage under the runtime's `manualOnly` activation policy. Use existing generic workflow commands to start the scoped run and inspect events and output.

### 4. Generalize

After CI/CD succeeds, define a reusable preview manifest for dependency allowlists, storage, workflow policy, and health checks. Keep app-specific materialization inside each app. Validate the design with a second read-oriented app before broader self-service.

## Verification and success

Automated tests cover remote caller auth, token isolation, manual-only workflow execution, loopback proxy auth injection, artifact provenance, storage isolation, deterministic cleanup, and CI/CD preview authorization/idempotency.

The end-to-end pilot must attach the local CI/CD UI through `gestaltd serve`, run the canonical sync workflow for one PR, verify admin and non-admin behavior, update and reset preview state, and delete all resources on PR close. Production routing and state must remain unchanged.

The work succeeds when an approved preview is ready within 10 minutes, any app can attach through the same `gestaltd serve` path, frontend changes remain immediate, backend and workflow output are useful for debugging, and teardown is deterministic.

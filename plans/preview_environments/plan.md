# Gestalt App Preview Environments

## Decision

Create one IAM-protected Cloud Run `gestaltd` service per approved internal PR. Run the branch app provider there with isolated database and workflow state, while the frontend stays local and uses Vite HMR.

CI/CD is the first pilot. Do not use production Cloud Run revisions or reverse-provider shadowing because both can affect production routing or shared state.

## Developer experience

```text
Browser → loopback Vite proxy → PR Cloud Run preview → branch app provider
                                                  ├─ preview-only database/workflows
                                                  └─ allowlisted production reads
```

The developer runs:

```bash
./valon-tools/apps/ci-cd/scripts/dev-ui-preview.sh <pr-number>
```

The script verifies the preview commit, obtains the developer's Gestalt bearer and a refreshable Cloud Run identity token, then starts Vite on `127.0.0.1`. UI changes remain immediate; backend changes publish a new immutable preview snapshot.

## Scope

The pilot must:

- Test a branch backend without changing production traffic, provider selection, or state.
- Exercise real caller authorization, including admin-only `pullRequests.mergeReadiness`.
- Materialize one representative PR into preview storage and return inspectable JSON.
- Support manual workflow runs while schedules and event activations remain paused.
- Provide deterministic deployment, attachment, reset, logs, expiry, and teardown.

It does not support production writes, migrations, notifications, merge actions, production database or Temporal sharing, untrusted forks, or indefinite previews.

## Required safety properties

1. Every preview has a unique service, runtime identity, database credential, encryption key, labels, and expiry.
2. Artifacts are selected by full commit SHA and verified digest.
3. Branch code packages without cloud credentials; trusted default-branch automation validates the artifact before authenticating and deploying.
4. Preview config disables authorization-state application and exposes no management or `/activate` route.
5. The developer bearer is forwarded only to production identity and authorization over HTTPS.
6. Integration reads use a separate preview relay with exact operation allowlists and no write, impersonation, secret-reading, or token-minting authority.
7. Production database, Temporal, and `service_account:ci-cd-sync` credentials are unavailable to previews.
8. Workflow definitions and activations are paused before their first reconciliation; authorized manual runs remain possible.
9. Only approved internal PRs are eligible.
10. PR-close cleanup and a maximum-age sweeper delete the complete resource set.

Branch providers are arbitrary code beside `gestaltd`, not a hardened sandbox. The preview runtime and relay permissions must therefore be safe even if branch code can exercise them.

## Implementation

### 1. Add reusable Gestalt primitives

**Caller-forwarding remotes:** Extend `gestaltd/internal/config/remotes.go`, `config_validate.go`, `gestaltd/internal/remote/remote.go`, and request authentication plumbing with an explicit `forwardBearer` mode. Capture the incoming bearer in private request context and forward it only to configured identity and authorization remotes. Preserve static-token remotes for dependencies. Test concurrency, redaction, validation, and backward compatibility.

**Forced-paused workflows:** Add an app deployment policy in `gestaltd/internal/config/config.go` and enforce it in `gestaltd/internal/bootstrap/workflow_app_definitions.go`:

```yaml
apps:
  ci-cd:
    workflowPolicy:
      forcePaused: true
```

Apply the policy before workflow reconciliation and prove that no first tick occurs while manual `StartRun` still works.

**Dual-auth Vite proxy:** Extend `sdk/typescript-web/src/vite.js` and its types/tests to keep the Gestalt bearer in `Authorization` and add a separately refreshed Cloud Run token in `X-Serverless-Authorization`. Neither token may reach browser JavaScript.

Document these capabilities in `docs/content/applications.mdx` and `docs/content/reference/config-file.mdx`.

### 2. Secure branch publication

Split `/Users/michael/valon/toolshed/.github/workflows/publish-app-snapshot.yml` into:

1. An unprivileged PR job that packages the exact SHA and uploads checksummed artifacts and provenance.
2. A trusted default-branch workflow that downloads bytes without executing PR code, validates the archive and manifest, then authenticates to GCP and publishes create-only immutable artifacts.

### 3. Deploy the CI/CD preview

Add trusted config and infrastructure under:

- `/Users/michael/valon/toolshed/valon-tools/deploy/preview/`
- `/Users/michael/valon/toolshed/valon-tools/terraform/preview/`
- `/Users/michael/valon/toolshed/.github/workflows/deploy-app-preview.yml`

Deploy `gestalt-preview-ci-cd-pr-<number>` with min instances zero, max one, IAM-only invocation, preview-specific storage, and repository/app/PR/SHA/expiry labels.

Use a curated config containing only the branch snapshot, preview-local IndexedDB and workflows, forced-paused activations, caller-forwarding identity/authorization, and allowlisted read dependencies. Do not modify or reuse `.github/workflows/deploy-valon-tools.yml`.

### 4. Add a useful CI/CD pilot operation

Add an admin-only preview operation in `valon-tools/apps/ci-cd/provider.ts`:

```text
preview.materializePullRequest({ prNumber })
```

Enable it only when `previewMode` is true and omit it from production `allowedOperations`. It reads one Front Porch PR plus available checks, deployments, and Linear data through the preview relay; reuses existing normalization/correlation logic; writes only preview storage; and returns intermediate and final JSON. Repeated calls must be idempotent.

Install normal CI/CD workflow definitions in preview storage with automatic activations paused. Provide commands to start one run manually, inspect events/output, open logs, and reset preview data.

### 5. Add attachment and lifecycle automation

Add `valon-tools/apps/ci-cd/scripts/dev-ui-preview.sh` to discover and verify the preview, resolve both credentials, start loopback Vite, and print data, workflow, logging, and reset commands.

Update approved commits with concurrency keyed by app and PR. Delete resources on PR close and with a 72-hour sweeper. Cleanup must remove the service, database, database user, credentials, encryption secret, and deployment metadata.

### 6. Generalize after the pilot

After CI/CD succeeds, extract a reusable preview manifest and app-independent publication, deployment, attachment, validation, and cleanup tooling. Keep materialization app-specific. Onboard a second read-oriented app before broader self-service; apps requiring writes need preview-owned resources or dry-run interfaces.

## Verification

Automated tests must cover remote auth and token isolation, forced-paused workflows, dual proxy headers, artifact provenance, preview naming/cleanup, CI/CD authorization/idempotency, isolated IndexedDB, and the absence of automatic workflow ticks.

The end-to-end pilot must:

1. Publish and deploy a controlled CI/CD branch.
2. Attach the local UI with one command.
3. Materialize one PR and verify list/detail pages.
4. Verify admin and non-admin `mergeReadiness`.
5. Run one workflow manually and inspect JSON.
6. Update and reset the preview without affecting production.
7. Close the PR and verify complete cleanup.

## Rollout and success

Ship the Gestalt safety primitives and secure artifact split first. Then run manual CI/CD previews for maintainers, enable opt-in deployment by PR label, and generalize only after CI/CD and a second read-oriented app pass the same invariants.

Success means an approved preview is ready within 10 minutes, attachment is one command, frontend changes remain immediate, preview data and workflow output are useful for debugging, teardown is deterministic, and preview activity causes zero production routing or state changes.

# gestaltd Artifact Infrastructure

This Terraform root owns the GCP resources used by `gestaltd` workflows to
publish Helm charts and immutable CI binary artifacts.

It creates:

- deployer IAM grants required by this Terraform root
- Artifact Registry Docker repository for OCI Helm charts
- public-read GCS bucket for commit-addressed `gestaltd` CI binary artifacts
- GitHub Actions Workload Identity Pool and provider
- chart publisher service account
- CI binary publisher service account
- Artifact Registry writer access for the publisher service account
- GCS object-create access for the CI binary publisher service account
- Artifact Registry reader access for configured `valon-tools` Terraform
  service accounts

The release workflow consumes the `github_actions_variables` output. Copy those
values into the `valon-technologies/gestalt` repository variables:

```text
GESTALTD_ARTIFACT_REGISTRY_HOST
GESTALTD_CHART_REPOSITORY
GESTALTD_CI_ARTIFACT_BASE_URL
GESTALTD_CI_ARTIFACT_BUCKET
GESTALTD_CI_GCP_SERVICE_ACCOUNT
GESTALTD_CI_GCP_WORKLOAD_IDENTITY_PROVIDER
GESTALTD_GCP_SERVICE_ACCOUNT
GESTALTD_GCP_WORKLOAD_IDENTITY_PROVIDER
```

CI binary artifacts are published under:

```text
${GESTALTD_CI_ARTIFACT_BASE_URL}/<40-character-commit-sha>/gestaltd-linux-x86_64.tar.gz
${GESTALTD_CI_ARTIFACT_BASE_URL}/<40-character-commit-sha>/gestaltd-linux-x86_64.tar.gz.sha256
${GESTALTD_CI_ARTIFACT_BASE_URL}/<40-character-commit-sha>/metadata.json
```

Consumers should pin full commit SHA paths. Mutable marker files, if added
later, should remain informational only.

The `Build Gestaltd` workflow publishes CI binary artifacts after its validation
jobs pass on `main`. To backfill an older commit, run that workflow manually
with `gestalt_ref` set to the full commit SHA. Manual backfill runs the
build/package/upload contract against that resolved SHA and loads the packaging
script from the current workflow checkout, so older commits can be backfilled
even if they predate current lint or docs workflow helpers.

Set `GESTALTD_CI_ARTIFACT_PUBLISH_ENABLED=true` only after the bucket, publisher
service account, Workload Identity binding, and repository variables are ready.
Until then, the publish job is skipped and the existing validation jobs continue
to run normally.

`valon-tools` environments should consume the chart repository location as input
variables instead of creating their own per-environment chart repository.
The `gestaltd_chart_reader_service_accounts` variable controls which
environment Terraform service accounts can read the chart repository. By
default this grants read access to the dev and stage `valon-tools` Terraform
service accounts.

## Continuous Deployment

`.github/workflows/deploy-gestaltd-artifacts.yml` runs Terraform for this root:

- pushes to `main` run `terraform apply -auto-approve`
- manual runs are supported with `workflow_dispatch`

Configure these GitHub repository secrets:

```text
GCP_PROJECT_ID
GCP_PROJECT_NUMBER
DOCS_TERRAFORM_STATE_BUCKET
```

Configure these GitHub repository variables:

```text
GCP_WIF_POOL_ID
GCP_WIF_PROVIDER_ID
```

The workflow authenticates as
`github-actions@${GCP_PROJECT_ID}.iam.gserviceaccount.com`.
That bootstrap identity, its GitHub Actions Workload Identity provider, and the
GCS backend bucket must exist before this Terraform root can deploy itself. The
root grants the deployer service account the additional project roles it needs
to manage gestaltd artifact resources.

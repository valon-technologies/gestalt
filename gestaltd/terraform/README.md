# gestaltd Artifact Infrastructure

This Terraform root owns the GCP resources used by `gestaltd` workflows to
publish Helm charts and immutable CI images.

It creates:

- deployer IAM grants required by this Terraform root
- Artifact Registry Docker repository for OCI Helm charts
- Artifact Registry Docker repository for commit-addressed `gestaltd` CI images
- existing GCS bucket retained for legacy CI binary artifacts
- GitHub Actions Workload Identity Pool and provider
- chart publisher service account
- CI image publisher service account
- Artifact Registry writer access for the publisher service account
- Artifact Registry writer access for the CI image publisher service account
- Artifact Registry reader access for configured `valon-tools` Terraform and
  GitHub deploy service accounts
- Artifact Registry reader access for the `valon-tools` GitHub Actions service
  account that pulls pinned `gestaltd` CI images during Docker builds

The release workflow consumes the `github_actions_variables` output. Copy those
values into the `valon-technologies/gestalt` repository variables:

```text
GESTALTD_ARTIFACT_REGISTRY_HOST
GESTALTD_CHART_REPOSITORY
GESTALTD_CI_GCP_SERVICE_ACCOUNT
GESTALTD_CI_GCP_WORKLOAD_IDENTITY_PROVIDER
GESTALTD_CI_IMAGE_PUBLISH_ENABLED
GESTALTD_CI_IMAGE_REPOSITORY
GESTALTD_GCP_SERVICE_ACCOUNT
GESTALTD_GCP_WORKLOAD_IDENTITY_PROVIDER
```

CI images are published under immutable tags:

```text
${GESTALTD_CI_IMAGE_REPOSITORY}:sha-<40-character-commit-sha>
```

Consumers should pin full image references with digests:

```text
${GESTALTD_CI_IMAGE_REPOSITORY}:sha-<40-character-commit-sha>@sha256:<manifest-digest>
```

The `Build Gestaltd` workflow publishes CI images after its validation jobs pass
on `main`. To backfill an older commit, run that workflow manually with
`gestalt_ref` set to the full commit SHA. Manual backfill runs the same
validation jobs before publishing the image.

Set `GESTALTD_CI_IMAGE_PUBLISH_ENABLED=true` only after the CI image repository,
publisher service account, Workload Identity binding, reader grants, and
repository variables are ready. Until then, the publish job is skipped and the
existing validation jobs continue to run normally.

The legacy GCS CI binary bucket remains managed so Terraform does not destroy
historical artifacts during this migration. Current workflows do not publish new
GCS binary artifacts.

`valon-tools` environments should consume the chart repository location as input
variables instead of creating their own per-environment chart repository.
The `gestaltd_chart_reader_service_accounts` variable controls which
environment Terraform and GitHub deploy service accounts can read the chart
repository. By default this grants read access to the dev and stage
`valon-tools` Terraform and GitHub deploy service accounts.

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

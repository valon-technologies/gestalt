# gestaltd Artifact Infrastructure

This Terraform root owns the GCP resources used by the `gestaltd` release
workflow to publish Helm charts.

It creates:

- deployer IAM grants required by this Terraform root
- Artifact Registry Docker repository for OCI Helm charts
- GitHub Actions Workload Identity Pool and provider
- chart publisher service account
- Artifact Registry writer access for the publisher service account

The release workflow consumes the `github_actions_variables` output. Copy those
values into the `valon-technologies/gestalt` repository variables:

```text
GESTALTD_ARTIFACT_REGISTRY_HOST
GESTALTD_CHART_REPOSITORY
GESTALTD_GCP_SERVICE_ACCOUNT
GESTALTD_GCP_WORKLOAD_IDENTITY_PROVIDER
```

`valon-tools` environments should consume the chart repository location as input
variables instead of creating their own per-environment chart repository.

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

# gestaltd Artifact Infrastructure

This Terraform root owns the GCP resources used by the `gestaltd` release
workflow to publish Helm charts.

It creates:

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

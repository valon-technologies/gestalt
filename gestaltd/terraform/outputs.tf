output "github_actions_variables" {
  description = "GitHub Actions repository variables required by .github/workflows/release-gestaltd.yml."
  value = {
    GESTALTD_ARTIFACT_REGISTRY_HOST         = local.artifact_registry_host
    GESTALTD_CHART_REPOSITORY               = local.chart_repository
    GESTALTD_GCP_SERVICE_ACCOUNT            = google_service_account.chart_publisher.email
    GESTALTD_GCP_WORKLOAD_IDENTITY_PROVIDER = "projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/${google_iam_workload_identity_pool.github_actions.workload_identity_pool_id}/providers/${google_iam_workload_identity_pool_provider.gestaltd_github_actions.workload_identity_pool_provider_id}"
  }
}

output "artifact_registry_host" {
  description = "Artifact Registry host for gestaltd Helm charts."
  value       = local.artifact_registry_host
}

output "chart_repository" {
  description = "OCI repository URL for gestaltd Helm charts."
  value       = local.chart_repository
}

output "chart_publisher_service_account" {
  description = "Service account used by GitHub Actions to publish gestaltd Helm charts."
  value       = google_service_account.chart_publisher.email
}

output "workload_identity_provider" {
  description = "Workload Identity provider resource name used by GitHub Actions."
  value       = "projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/${google_iam_workload_identity_pool.github_actions.workload_identity_pool_id}/providers/${google_iam_workload_identity_pool_provider.gestaltd_github_actions.workload_identity_pool_provider_id}"
}

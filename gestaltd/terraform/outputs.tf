output "github_actions_variables" {
  description = "GitHub Actions repository variables required by .github/workflows/release-gestaltd.yml."
  value = {
    GESTALTD_ARTIFACT_REGISTRY_HOST         = local.artifact_registry_host
    GESTALTD_CHART_REPOSITORY               = local.chart_repository
    GESTALTD_CI_ARTIFACT_BASE_URL           = local.ci_artifact_base_url
    GESTALTD_CI_ARTIFACT_BUCKET             = google_storage_bucket.gestaltd_ci_binaries.name
    GESTALTD_CI_GCP_SERVICE_ACCOUNT         = google_service_account.ci_binary_publisher.email
    GESTALTD_GCP_SERVICE_ACCOUNT            = google_service_account.chart_publisher.email
    GESTALTD_GCP_WORKLOAD_IDENTITY_PROVIDER = local.workload_identity_provider
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

output "ci_artifact_base_url" {
  description = "Base HTTPS URL for immutable gestaltd CI binary artifacts."
  value       = local.ci_artifact_base_url
}

output "ci_artifact_bucket" {
  description = "GCS bucket that stores immutable gestaltd CI binary artifacts."
  value       = google_storage_bucket.gestaltd_ci_binaries.name
}

output "ci_binary_publisher_service_account" {
  description = "Service account used by GitHub Actions to publish gestaltd CI binary artifacts."
  value       = google_service_account.ci_binary_publisher.email
}

output "workload_identity_provider" {
  description = "Workload Identity provider resource name used by GitHub Actions."
  value       = local.workload_identity_provider
}

output "github_actions_variables" {
  description = "GitHub Actions repository variables required by gestaltd release and CI image workflows."
  value = {
    GESTALTD_ARTIFACT_REGISTRY_HOST            = local.artifact_registry_host
    GESTALTD_CHART_REPOSITORY                  = local.chart_repository
    GESTALTD_CI_GCP_SERVICE_ACCOUNT            = google_service_account.ci_image_publisher.email
    GESTALTD_CI_GCP_WORKLOAD_IDENTITY_PROVIDER = local.ci_image_workload_identity_provider
    GESTALTD_CI_IMAGE_PUBLISH_ENABLED          = "false"
    GESTALTD_CI_IMAGE_REPOSITORY               = local.ci_image_repository
    GESTALTD_GCP_SERVICE_ACCOUNT               = google_service_account.chart_publisher.email
    GESTALTD_GCP_WORKLOAD_IDENTITY_PROVIDER    = local.workload_identity_provider
  }
}

output "artifact_registry_host" {
  description = "Artifact Registry host for gestaltd Helm charts and CI images."
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

output "ci_image_repository" {
  description = "Artifact Registry image repository for commit-addressed gestaltd CI images."
  value       = local.ci_image_repository
}

output "ci_image_publisher_service_account" {
  description = "Service account used by GitHub Actions to publish gestaltd CI images."
  value       = google_service_account.ci_image_publisher.email
}

output "workload_identity_provider" {
  description = "Workload Identity provider resource name used by GitHub Actions."
  value       = local.workload_identity_provider
}

output "ci_image_workload_identity_provider" {
  description = "Workload Identity provider resource name used by the gestaltd CI image workflow."
  value       = local.ci_image_workload_identity_provider
}

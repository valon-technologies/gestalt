variable "project_id" {
  description = "GCP project that owns gestaltd release artifacts."
  type        = string
}

variable "region" {
  description = "Artifact Registry region for gestaltd Helm charts."
  type        = string
  default     = "us-east4"
}

variable "artifact_registry_repository_id" {
  description = "Artifact Registry repository ID for gestaltd OCI Helm charts."
  type        = string
  default     = "gestaltd-charts"
}

variable "chart_publisher_service_account_id" {
  description = "Service account ID used by GitHub Actions to publish gestaltd charts."
  type        = string
  default     = "gestaltd-chart-publisher"
}

variable "github_actions_workload_identity_pool_id" {
  description = "Workload Identity Pool ID for GitHub Actions OIDC."
  type        = string
  default     = "github-actions"
}

variable "gestaltd_github_actions_provider_id" {
  description = "Workload Identity Pool provider ID for the gestaltd release workflow."
  type        = string
  default     = "gestaltd"
}

variable "github_repository" {
  description = "GitHub repository allowed to publish gestaltd charts."
  type        = string
  default     = "valon-technologies/gestalt"
}

variable "github_ref_prefix" {
  description = "Git ref prefix allowed to publish gestaltd charts."
  type        = string
  default     = "refs/tags/gestaltd/v"
}

variable "labels" {
  description = "Additional labels applied to gestaltd artifact resources."
  type        = map(string)
  default     = {}
}

variable "project_id" {
  description = "GCP project that owns gestaltd release artifacts."
  type        = string
}

variable "region" {
  description = "Artifact Registry region for gestaltd Helm charts."
  type        = string
  default     = "us-east1"
}

variable "artifact_registry_repository_id" {
  description = "Artifact Registry repository ID for gestaltd OCI Helm charts."
  type        = string
  default     = "gestaltd-charts"
}

variable "ci_image_repository_id" {
  description = "Artifact Registry repository ID for commit-addressed gestaltd CI images."
  type        = string
  default     = "gestaltd-ci"
}

variable "ci_binary_bucket_name" {
  description = "Existing GCS bucket name for legacy gestaltd CI binary artifacts."
  type        = string
  default     = "gitlab-peach-street-gestaltd-ci-artifacts"
}

variable "ci_binary_bucket_location" {
  description = "Existing GCS bucket location for legacy gestaltd CI binary artifacts."
  type        = string
  default     = "US"
}

variable "gestaltd_chart_reader_service_accounts" {
  description = "Service account emails allowed to read gestaltd Helm charts from Artifact Registry."
  type        = set(string)
  default = [
    "github-deploy-dev@valon-tools-dev.iam.gserviceaccount.com",
    "github-deploy-stage@valon-tools-stage.iam.gserviceaccount.com",
    "terraform-dev@valon-tools-dev.iam.gserviceaccount.com",
    "terraform-stage@valon-tools-stage.iam.gserviceaccount.com",
    "tools-dev-nodes@valon-tools-dev.iam.gserviceaccount.com",
    "tools-stage-nodes@valon-tools-stage.iam.gserviceaccount.com",
  ]
}

variable "gestaltd_ci_image_reader_service_accounts" {
  description = "Additional service account emails allowed to read commit-addressed gestaltd CI images from Artifact Registry."
  type        = set(string)
  default     = []
}

variable "deployer_service_account_id" {
  description = "Service account ID used by GitHub Actions to apply this Terraform root."
  type        = string
  default     = "github-actions"
}

variable "chart_publisher_service_account_id" {
  description = "Service account ID used by GitHub Actions to publish gestaltd charts."
  type        = string
  default     = "gestaltd-chart-publisher"
}

variable "ci_image_publisher_service_account_id" {
  description = "Existing service account ID used by GitHub Actions to publish gestaltd CI images."
  type        = string
  default     = "gestaltd-ci-binary-publisher"
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

variable "gestaltd_ci_image_github_actions_provider_id" {
  description = "Existing Workload Identity Pool provider ID for the gestaltd CI image workflow."
  type        = string
  default     = "gestaltd-ci-binary"
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

variable "ci_image_github_ref" {
  description = "Exact Git ref allowed to publish gestaltd CI images."
  type        = string
  default     = "refs/heads/main"
}

variable "labels" {
  description = "Additional labels applied to gestaltd artifact resources."
  type        = map(string)
  default     = {}
}

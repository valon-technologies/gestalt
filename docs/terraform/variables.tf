variable "project_id" {
  description = "GCP project hosting the Cloud Run service"
  type        = string
  default     = "REDACTED_GCP_PROJECT"
}

variable "project_number" {
  description = "GCP project number hosting the Cloud Run service"
  type        = string
}

variable "region" {
  description = "GCP region for Cloud Run"
  type        = string
  default     = "us-east4"
}

variable "dns_project_id" {
  description = "GCP project containing the DNS zone"
  type        = string
  default     = "serviceone"
}

variable "domain" {
  description = "Docs site domain"
  type        = string
  default     = "gestaltd.ai"
}

variable "docs_image" {
  description = "Container image for the docs service (required, no default to prevent accidental reverts)"
  type        = string
}

variable "wif_pool_id" {
  description = "Workload Identity Pool ID for GitHub Actions OIDC"
  type        = string
  default     = "github-pool"
}

variable "github_repository" {
  description = "GitHub repository (owner/repo) allowed to authenticate via WIF"
  type        = string
  default     = "valon-technologies/gestalt"
}

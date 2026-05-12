variable "project_id" {
  description = "GCP project hosting the Cloud Run service"
  type        = string
}

variable "project_number" {
  description = "GCP project number hosting the Cloud Run service"
  type        = string
}

variable "region" {
  description = "GCP region for Cloud Run"
  type        = string
}

variable "dns_project_id" {
  description = "GCP project containing the DNS zone"
  type        = string
}

variable "domain" {
  description = "Docs site domain"
  type        = string
  default     = "gestaltd.ai"
}

variable "registry_domain" {
  description = "Provider registry site domain"
  type        = string
  default     = "registry.gestaltd.ai"
}

variable "docs_image" {
  description = "Container image for the docs service (required, no default to prevent accidental reverts)"
  type        = string
}

variable "resource_prefix" {
  description = "Prefix used for docs infrastructure resource names"
  type        = string
}

variable "sdk_api_docs_bucket_name" {
  description = "Optional explicit GCS bucket name for versioned SDK API docs"
  type        = string
  default     = ""
}

variable "sdk_api_docs_bucket_location" {
  description = "Location for the versioned SDK API docs GCS bucket"
  type        = string
  default     = "US"
}

variable "wif_pool_id" {
  description = "Workload Identity Pool ID for GitHub Actions OIDC"
  type        = string
}

variable "github_repository" {
  description = "GitHub repository (owner/repo) allowed to authenticate via WIF"
  type        = string
  default     = "valon-technologies/gestalt"
}

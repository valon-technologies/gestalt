variable "project_id" {
  description = "GCP project hosting the Cloud Run service"
  type        = string
  default     = "REDACTED_GCP_PROJECT"
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

variable "dns_zone_name" {
  description = "Cloud DNS managed zone name"
  type        = string
  default     = "peachstreet-dev"
}

variable "domain" {
  description = "Docs site domain"
  type        = string
  default     = "docs.toolshed.peachstreet.dev"
}

variable "valon_office_ip_ranges" {
  description = "Office CIDR ranges (NY, AZ, SF, Jacksonville). Sourced from GSM secret valon-office-ip-ranges."
  type        = list(string)
}

variable "valon_vpn_ip_ranges" {
  description = "NordLayer VPN egress IPs. Sourced from GSM secret valon-vpn-ip-ranges."
  type        = list(string)
}

variable "docs_image" {
  description = "Initial container image for the docs service"
  type        = string
  default     = "us-docker.pkg.dev/cloudrun/container/hello"
}

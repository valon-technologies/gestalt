terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

data "google_project" "current" {
  project_id = var.project_id
}

locals {
  labels = merge(
    {
      component  = "gestaltd"
      managed_by = "terraform"
    },
    var.labels,
  )

  artifact_registry_host = "${var.region}-docker.pkg.dev"
  chart_repository       = "oci://${local.artifact_registry_host}/${var.project_id}/${var.artifact_registry_repository_id}"

  workload_identity_provider = "projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/${google_iam_workload_identity_pool.github_actions.workload_identity_pool_id}/providers/${google_iam_workload_identity_pool_provider.gestaltd_github_actions.workload_identity_pool_provider_id}"
}

resource "google_project_service" "required" {
  for_each = toset([
    "artifactregistry.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "sts.googleapis.com",
  ])

  project = var.project_id
  service = each.value

  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "gestaltd_charts" {
  project       = var.project_id
  location      = var.region
  repository_id = var.artifact_registry_repository_id
  format        = "DOCKER"
  description   = "OCI Helm charts for gestaltd"
  labels        = local.labels

  depends_on = [
    google_project_service.required,
  ]
}

resource "google_service_account" "chart_publisher" {
  project      = var.project_id
  account_id   = var.chart_publisher_service_account_id
  display_name = "gestaltd chart publisher"
  description  = "Publishes gestaltd Helm charts to Artifact Registry from GitHub Actions."

  depends_on = [
    google_project_service.required,
  ]
}

resource "google_iam_workload_identity_pool" "github_actions" {
  project                   = var.project_id
  workload_identity_pool_id = var.github_actions_workload_identity_pool_id
  display_name              = "GitHub Actions"
  description               = "GitHub Actions identities for publishing gestaltd artifacts."

  depends_on = [
    google_project_service.required,
  ]
}

resource "google_iam_workload_identity_pool_provider" "gestaltd_github_actions" {
  project                            = var.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github_actions.workload_identity_pool_id
  workload_identity_pool_provider_id = var.gestaltd_github_actions_provider_id
  display_name                       = "gestaltd"
  description                        = "OIDC provider for ${var.github_repository} gestaltd release workflows."

  attribute_mapping = {
    "google.subject"             = "assertion.sub"
    "attribute.actor"            = "assertion.actor"
    "attribute.repository"       = "assertion.repository"
    "attribute.repository_owner" = "assertion.repository_owner"
    "attribute.ref"              = "assertion.ref"
  }

  attribute_condition = "assertion.repository == \"${var.github_repository}\" && assertion.ref.startsWith(\"${var.github_ref_prefix}\")"

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_service_account_iam_member" "github_actions_workload_identity_user" {
  service_account_id = google_service_account.chart_publisher.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github_actions.name}/attribute.repository/${var.github_repository}"
}

resource "google_artifact_registry_repository_iam_member" "chart_publisher" {
  project    = var.project_id
  location   = google_artifact_registry_repository.gestaltd_charts.location
  repository = google_artifact_registry_repository.gestaltd_charts.repository_id
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.chart_publisher.email}"
}

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

moved {
  from = google_service_account.ci_binary_publisher
  to   = google_service_account.ci_image_publisher
}

moved {
  from = google_iam_workload_identity_pool_provider.gestaltd_ci_binary_github_actions
  to   = google_iam_workload_identity_pool_provider.gestaltd_ci_image_github_actions
}

moved {
  from = google_service_account_iam_member.ci_binary_github_actions_workload_identity_user
  to   = google_service_account_iam_member.ci_image_github_actions_workload_identity_user
}

moved {
  from = google_storage_bucket_iam_member.ci_binary_publisher
  to   = google_storage_bucket_iam_member.legacy_ci_binary_publisher
}

moved {
  from = google_storage_bucket_iam_member.ci_binary_public_readers
  to   = google_storage_bucket_iam_member.legacy_ci_binary_public_readers
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

  artifact_registry_host              = "${var.region}-docker.pkg.dev"
  chart_repository                    = "oci://${local.artifact_registry_host}/${var.project_id}/${var.artifact_registry_repository_id}"
  ci_image_repository                 = "${local.artifact_registry_host}/${var.project_id}/${google_artifact_registry_repository.gestaltd_ci_images.repository_id}/gestaltd"
  deployer_member                     = "serviceAccount:${var.deployer_service_account_id}@${var.project_id}.iam.gserviceaccount.com"
  chart_ref_condition                 = "assertion.ref.startsWith(\"${var.github_ref_prefix}\")"
  ci_image_ref_condition              = "assertion.ref == \"${var.ci_image_github_ref}\""
  workload_identity_provider          = "projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/${google_iam_workload_identity_pool.github_actions.workload_identity_pool_id}/providers/${google_iam_workload_identity_pool_provider.gestaltd_github_actions.workload_identity_pool_provider_id}"
  ci_image_workload_identity_provider = "projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/${google_iam_workload_identity_pool.github_actions.workload_identity_pool_id}/providers/${google_iam_workload_identity_pool_provider.gestaltd_ci_image_github_actions.workload_identity_pool_provider_id}"
}

resource "google_project_iam_member" "deployer_permissions" {
  for_each = toset([
    "roles/artifactregistry.admin",
    "roles/iam.workloadIdentityPoolAdmin",
    "roles/serviceusage.serviceUsageAdmin",
    "roles/storage.admin",
  ])

  project = var.project_id
  role    = each.value
  member  = local.deployer_member
}

resource "google_project_service" "required" {
  for_each = toset([
    "artifactregistry.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "storage.googleapis.com",
    "sts.googleapis.com",
  ])

  project = var.project_id
  service = each.value

  disable_on_destroy = false

  depends_on = [
    google_project_iam_member.deployer_permissions,
  ]
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

resource "google_artifact_registry_repository" "gestaltd_ci_images" {
  project       = var.project_id
  location      = var.region
  repository_id = var.ci_image_repository_id
  format        = "DOCKER"
  description   = "Commit-addressed gestaltd CI images"
  labels        = local.labels

  docker_config {
    immutable_tags = true
  }

  depends_on = [
    google_project_service.required,
  ]
}

resource "google_storage_bucket" "gestaltd_ci_binaries" {
  project                     = var.project_id
  name                        = var.ci_binary_bucket_name
  location                    = var.ci_binary_bucket_location
  uniform_bucket_level_access = true
  force_destroy               = false
  labels                      = local.labels

  versioning {
    enabled = true
  }

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

resource "google_service_account" "ci_image_publisher" {
  project      = var.project_id
  account_id   = var.ci_image_publisher_service_account_id
  display_name = "gestaltd CI binary publisher"
  description  = "Publishes immutable gestaltd CI binary artifacts to GCS from GitHub Actions."

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
    "attribute.publisher"        = "\"gestaltd-chart\""
    "attribute.repository"       = "assertion.repository"
    "attribute.repository_owner" = "assertion.repository_owner"
    "attribute.ref"              = "assertion.ref"
  }

  attribute_condition = "assertion.repository == \"${var.github_repository}\" && ${local.chart_ref_condition}"

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_iam_workload_identity_pool_provider" "gestaltd_ci_image_github_actions" {
  project                            = var.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github_actions.workload_identity_pool_id
  workload_identity_pool_provider_id = var.gestaltd_ci_image_github_actions_provider_id
  display_name                       = "gestaltd CI image"
  description                        = "OIDC provider for ${var.github_repository} gestaltd CI image workflows."

  attribute_mapping = {
    "google.subject"             = "assertion.sub"
    "attribute.actor"            = "assertion.actor"
    "attribute.publisher"        = "\"gestaltd-ci-image\""
    "attribute.repository"       = "assertion.repository"
    "attribute.repository_owner" = "assertion.repository_owner"
    "attribute.ref"              = "assertion.ref"
  }

  attribute_condition = "assertion.repository == \"${var.github_repository}\" && ${local.ci_image_ref_condition}"

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_service_account_iam_member" "github_actions_workload_identity_user" {
  service_account_id = google_service_account.chart_publisher.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github_actions.name}/attribute.publisher/gestaltd-chart"
}

resource "google_service_account_iam_member" "ci_image_github_actions_workload_identity_user" {
  service_account_id = google_service_account.ci_image_publisher.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github_actions.name}/attribute.publisher/gestaltd-ci-image"
}

resource "google_artifact_registry_repository_iam_member" "chart_publisher" {
  project    = var.project_id
  location   = google_artifact_registry_repository.gestaltd_charts.location
  repository = google_artifact_registry_repository.gestaltd_charts.repository_id
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.chart_publisher.email}"
}

resource "google_artifact_registry_repository_iam_member" "chart_readers" {
  for_each = var.gestaltd_chart_reader_service_accounts

  project    = var.project_id
  location   = google_artifact_registry_repository.gestaltd_charts.location
  repository = google_artifact_registry_repository.gestaltd_charts.repository_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${each.value}"
}

resource "google_artifact_registry_repository_iam_member" "ci_image_publisher" {
  project    = var.project_id
  location   = google_artifact_registry_repository.gestaltd_ci_images.location
  repository = google_artifact_registry_repository.gestaltd_ci_images.repository_id
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.ci_image_publisher.email}"
}

resource "google_artifact_registry_repository_iam_member" "ci_image_readers" {
  for_each = setunion(
    var.gestaltd_ci_image_reader_service_accounts,
    toset(["github-actions@${var.project_id}.iam.gserviceaccount.com"]),
  )

  project    = var.project_id
  location   = google_artifact_registry_repository.gestaltd_ci_images.location
  repository = google_artifact_registry_repository.gestaltd_ci_images.repository_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${each.value}"
}

resource "google_storage_bucket_iam_member" "legacy_ci_binary_publisher" {
  bucket = google_storage_bucket.gestaltd_ci_binaries.name
  role   = "roles/storage.objectCreator"
  member = "serviceAccount:${google_service_account.ci_image_publisher.email}"
}

resource "google_storage_bucket_iam_member" "legacy_ci_binary_public_readers" {
  bucket = google_storage_bucket.gestaltd_ci_binaries.name
  role   = "roles/storage.objectViewer"
  member = "allUsers"
}

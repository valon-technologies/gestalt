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

provider "google" {
  alias   = "dns"
  project = var.dns_project_id
}

# ---------- Cloud Run ----------

resource "google_cloud_run_v2_service" "docs" {
  name     = "toolshed-docs"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"

  template {
    containers {
      image = var.docs_image
      ports {
        container_port = 8080
      }
      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }
    }
    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }
  }

}

resource "google_cloud_run_v2_service_iam_member" "public" {
  name     = google_cloud_run_v2_service.docs.name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# ---------- Load Balancer ----------

resource "google_compute_region_network_endpoint_group" "docs" {
  name                  = "toolshed-docs-neg"
  region                = var.region
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = google_cloud_run_v2_service.docs.name
  }
}

resource "google_compute_backend_service" "docs" {
  name                  = "toolshed-docs-backend"
  load_balancing_scheme = "EXTERNAL_MANAGED"

  backend {
    group = google_compute_region_network_endpoint_group.docs.id
  }
}

resource "google_compute_url_map" "docs" {
  name            = "toolshed-docs-url-map"
  default_service = google_compute_backend_service.docs.id
}

resource "google_compute_managed_ssl_certificate" "docs" {
  name = "toolshed-docs-cert"

  managed {
    domains = [var.domain]
  }
}

resource "google_compute_target_https_proxy" "docs" {
  name             = "toolshed-docs-https-proxy"
  url_map          = google_compute_url_map.docs.id
  ssl_certificates = [google_compute_managed_ssl_certificate.docs.id]
}

resource "google_compute_global_address" "docs" {
  name = "toolshed-docs-ip"
}

resource "google_compute_global_forwarding_rule" "docs" {
  name                  = "toolshed-docs-forwarding-rule"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  target                = google_compute_target_https_proxy.docs.id
  port_range            = "443"
  ip_address            = google_compute_global_address.docs.address
}

# ---------- DNS ----------

resource "google_dns_record_set" "docs" {
  provider     = google.dns
  managed_zone = var.dns_zone_name
  name         = "${var.domain}."
  type         = "A"
  ttl          = 300
  rrdatas      = [google_compute_global_address.docs.address]
}

# ---------- Workload Identity Federation ----------

data "google_project" "current" {}

resource "google_service_account_iam_member" "github_actions_wif" {
  service_account_id = "projects/${var.project_id}/serviceAccounts/github-actions@${var.project_id}.iam.gserviceaccount.com"
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/${var.wif_pool_id}/attribute.repository/${var.github_repository}"
}

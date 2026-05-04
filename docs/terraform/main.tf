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

locals {
  docs_cert_name = "${var.resource_prefix}-cert-${replace(var.domain, ".", "-")}-${replace(var.registry_domain, ".", "-")}"
}

# ---------- Cloud Run ----------

resource "google_cloud_run_v2_service" "docs" {
  name     = var.resource_prefix
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
  name                  = "${var.resource_prefix}-neg"
  region                = var.region
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = google_cloud_run_v2_service.docs.name
  }
}

resource "google_compute_backend_service" "docs" {
  name                  = "${var.resource_prefix}-backend"
  load_balancing_scheme = "EXTERNAL_MANAGED"

  backend {
    group = google_compute_region_network_endpoint_group.docs.id
  }
}

resource "google_compute_url_map" "docs" {
  name            = "${var.resource_prefix}-url-map"
  default_service = google_compute_backend_service.docs.id
}

resource "google_compute_managed_ssl_certificate" "docs" {
  name = local.docs_cert_name

  managed {
    domains = [var.domain, var.registry_domain]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "google_compute_target_https_proxy" "docs" {
  name             = "${var.resource_prefix}-https-proxy"
  url_map          = google_compute_url_map.docs.id
  ssl_certificates = [google_compute_managed_ssl_certificate.docs.id]
}

resource "google_compute_global_address" "docs" {
  name = "${var.resource_prefix}-ip"
}

resource "google_compute_global_forwarding_rule" "docs" {
  name                  = "${var.resource_prefix}-forwarding-rule"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  target                = google_compute_target_https_proxy.docs.id
  port_range            = "443"
  ip_address            = google_compute_global_address.docs.address
}

# ---------- HTTP-to-HTTPS Redirect ----------

resource "google_compute_url_map" "docs_http_redirect" {
  name = "${var.resource_prefix}-http-redirect"

  default_url_redirect {
    https_redirect         = true
    strip_query            = false
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
  }
}

resource "google_compute_target_http_proxy" "docs_redirect" {
  name    = "${var.resource_prefix}-http-proxy"
  url_map = google_compute_url_map.docs_http_redirect.id
}

resource "google_compute_global_forwarding_rule" "docs_http" {
  name                  = "${var.resource_prefix}-http-forwarding-rule"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  target                = google_compute_target_http_proxy.docs_redirect.id
  port_range            = "80"
  ip_address            = google_compute_global_address.docs.address
}

# ---------- DNS ----------

resource "google_dns_managed_zone" "docs" {
  provider    = google.dns
  name        = replace(var.domain, ".", "-")
  dns_name    = "${var.domain}."
  description = "DNS zone for ${var.domain}"
}

resource "google_dns_record_set" "docs" {
  provider     = google.dns
  managed_zone = google_dns_managed_zone.docs.name
  name         = "${var.domain}."
  type         = "A"
  ttl          = 300
  rrdatas      = [google_compute_global_address.docs.address]
}

resource "google_dns_record_set" "registry" {
  provider     = google.dns
  managed_zone = google_dns_managed_zone.docs.name
  name         = "${var.registry_domain}."
  type         = "A"
  ttl          = 300
  rrdatas      = [google_compute_global_address.docs.address]
}

# ---------- Workload Identity Federation ----------

resource "google_service_account_iam_member" "github_actions_wif" {
  service_account_id = "projects/${var.project_id}/serviceAccounts/github-actions@${var.project_id}.iam.gserviceaccount.com"
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/projects/${var.project_number}/locations/global/workloadIdentityPools/${var.wif_pool_id}/attribute.repository/${var.github_repository}"
}

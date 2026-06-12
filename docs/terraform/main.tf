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
  www_domain = "www.${var.domain}"
  # GCP SSL certificate names must be <=63 chars and match [a-z]([-a-z0-9]*[a-z0-9])?
  docs_cert_with_www_name   = "${var.resource_prefix}-cert-with-www"
  github_actions_email      = "github-actions@${var.project_id}.iam.gserviceaccount.com"
  sdk_api_docs_bucket_name  = var.sdk_api_docs_bucket_name != "" ? var.sdk_api_docs_bucket_name : "${var.resource_prefix}-sdk-api-docs"
  sdk_api_docs_backend_name = "${var.resource_prefix}-sdk-api-docs-backend"
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

resource "google_storage_bucket" "sdk_api_docs" {
  name                        = local.sdk_api_docs_bucket_name
  location                    = var.sdk_api_docs_bucket_location
  uniform_bucket_level_access = true

  # Public objects are required for the external HTTPS load-balancer backend
  # bucket serving model used by gestaltd.ai/api/* in the follow-up PR.
  public_access_prevention = "inherited"
}

resource "google_storage_bucket_iam_member" "sdk_api_docs_public_read" {
  bucket = google_storage_bucket.sdk_api_docs.name
  role   = "roles/storage.objectViewer"
  member = "allUsers"
}

resource "google_storage_bucket_iam_member" "sdk_api_docs_github_actions_upload" {
  bucket = google_storage_bucket.sdk_api_docs.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${local.github_actions_email}"
}

resource "google_compute_backend_bucket" "sdk_api_docs" {
  name        = local.sdk_api_docs_backend_name
  bucket_name = google_storage_bucket.sdk_api_docs.name
  enable_cdn  = false
}

resource "google_compute_url_map" "docs" {
  name            = "${var.resource_prefix}-url-map"
  default_service = google_compute_backend_service.docs.id

  host_rule {
    hosts        = [local.www_domain]
    path_matcher = "www-redirect"
  }

  host_rule {
    hosts        = ["*"]
    path_matcher = "docs"
  }

  path_matcher {
    name = "www-redirect"

    default_url_redirect {
      host_redirect          = var.domain
      https_redirect         = false
      strip_query            = false
      redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
    }
  }

  path_matcher {
    name            = "docs"
    default_service = google_compute_backend_service.docs.id

    path_rule {
      paths = [
        "/api/go",
        "/api/go/*",
        "/api/python",
        "/api/python/*",
        "/api/rust",
        "/api/rust/*",
        "/api/typescript",
        "/api/typescript/*",
      ]
      service = google_compute_backend_bucket.sdk_api_docs.id
    }
  }
}

resource "google_compute_managed_ssl_certificate" "docs_with_www" {
  name = local.docs_cert_with_www_name

  managed {
    domains = [var.domain, local.www_domain, var.registry_domain]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "google_compute_target_https_proxy" "docs" {
  name    = "${var.resource_prefix}-https-proxy"
  url_map = google_compute_url_map.docs.id
  ssl_certificates = [google_compute_managed_ssl_certificate.docs_with_www.id]
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

resource "google_dns_record_set" "www" {
  provider     = google.dns
  managed_zone = google_dns_managed_zone.docs.name
  name         = "${local.www_domain}."
  type         = "CNAME"
  ttl          = 300
  rrdatas      = ["${var.domain}."]
}

# ---------- Workload Identity Federation ----------

resource "google_service_account_iam_member" "github_actions_wif" {
  service_account_id = "projects/${var.project_id}/serviceAccounts/${local.github_actions_email}"
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/projects/${var.project_number}/locations/global/workloadIdentityPools/${var.wif_pool_id}/attribute.repository/${var.github_repository}"
}

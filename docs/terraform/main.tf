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
          memory = "256Mi"
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

# ---------- Cloud Armor ----------

resource "google_compute_security_policy" "docs" {
  name = "toolshed-docs-policy"

  rule {
    action   = "allow"
    priority = 1000
    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = var.valon_office_ip_ranges
      }
    }
    description = "Allow Valon office IPs"
  }

  rule {
    action   = "allow"
    priority = 1100
    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = var.valon_vpn_ip_ranges
      }
    }
    description = "Allow Valon VPN IPs"
  }

  rule {
    action   = "deny(403)"
    priority = 2147483647
    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = ["*"]
      }
    }
    description = "Default deny"
  }
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
  security_policy       = google_compute_security_policy.docs.id

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

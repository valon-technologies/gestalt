output "docs_url" {
  description = "Public URL for the docs site"
  value       = "https://${var.domain}"
}

output "registry_url" {
  description = "Public URL for the provider registry"
  value       = "https://${var.registry_domain}"
}

output "load_balancer_ip" {
  description = "Global IP address of the load balancer"
  value       = google_compute_global_address.docs.address
}

output "cloud_run_url" {
  description = "Direct Cloud Run URL (not publicly reachable due to ingress restriction)"
  value       = google_cloud_run_v2_service.docs.uri
}

output "sdk_api_docs_bucket_name" {
  description = "GCS bucket for versioned SDK API docs"
  value       = google_storage_bucket.sdk_api_docs.name
}

output "dns_zone_nameservers" {
  description = "Nameservers for the DNS zone - set these in your domain registrar"
  value       = google_dns_managed_zone.docs.name_servers
}

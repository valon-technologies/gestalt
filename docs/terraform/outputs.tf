output "docs_url" {
  description = "Public URL for the docs site"
  value       = "https://${var.domain}"
}

output "load_balancer_ip" {
  description = "Global IP address of the load balancer"
  value       = google_compute_global_address.docs.address
}

output "cloud_run_url" {
  description = "Direct Cloud Run URL (not publicly reachable due to ingress restriction)"
  value       = google_cloud_run_v2_service.docs.uri
}

output "dns_zone_nameservers" {
  description = "Nameservers for the DNS zone - set these in your domain registrar"
  value       = google_dns_managed_zone.docs.name_servers
}

# Soft-skip: run via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set
# and /_noctaxris-gcp/ready succeeds. See docs/configuration.md.
#
# Cloud DNS: hashicorp/google dns_custom_endpoint
# (default BaseUrl https://dns.googleapis.com/dns/v1/).
# Managed zone plus google_dns_record_set (Changes.create/get).
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 5.0"
    }
  }
}

variable "endpoint" {
  type        = string
  description = "Noctaxris-GCP API base URL (no trailing slash required)"
}

variable "project" {
  type    = string
  default = "noctaxris-gcp-local"
}

variable "name_prefix" {
  type        = string
  description = "Unique prefix for resource names"
}

locals {
  ep = trimsuffix(var.endpoint, "/")
}

provider "google" {
  project = var.project
  region  = "us-central1"

  user_project_override = false

  dns_custom_endpoint = "${local.ep}/dns/v1/"
}

resource "google_dns_managed_zone" "lab" {
  name        = "${var.name_prefix}-zone"
  dns_name    = "${var.name_prefix}.noctaxris-gcp.lab."
  description = "Noctaxris-GCP Terraform lab managed zone"
  visibility  = "public"

  force_destroy = false
}

resource "google_dns_record_set" "lab_a" {
  name         = "www.${var.name_prefix}.noctaxris-gcp.lab."
  managed_zone = google_dns_managed_zone.lab.name
  type         = "A"
  ttl          = 300
  rrdatas      = ["10.0.0.1"]
}

output "zone_name" {
  value = google_dns_managed_zone.lab.name
}

output "dns_name" {
  value = google_dns_managed_zone.lab.dns_name
}

output "name_servers" {
  value = google_dns_managed_zone.lab.name_servers
}

output "record_fqdn" {
  value = google_dns_record_set.lab_a.name
}

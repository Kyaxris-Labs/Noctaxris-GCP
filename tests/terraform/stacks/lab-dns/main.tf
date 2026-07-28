# Soft-skip: run via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set
# and /_noctaxris-gcp/ready succeeds. See docs/configuration.md.
#
# Cloud DNS: hashicorp/google dns_custom_endpoint
# (default BaseUrl https://dns.googleapis.com/dns/v1/).
# Managed zone create/get/delete only. google_dns_record_set uses Changes.create,
# which this lab does not implement — do not add record-set resources here.
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

  # Lab delete cascades stored rrsets (including seeded NS/SOA). force_destroy
  # would call Changes.create, which the lab does not expose.
  force_destroy = false
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

# Soft-skip: run via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set
# and /_noctaxris-gcp/ready succeeds. See docs/configuration.md.
#
# Compute Engine: hashicorp/google compute_custom_endpoint
# (default BaseUrl https://compute.googleapis.com/compute/v1/).
# VPC network only. Insert returns a completed compute#operation, so the
# provider's OperationWait short-circuits without Operations GET (not mounted).
#
# Skipped here (honest gaps):
# - google_compute_instance: ResolveImage needs Images API (not in lab)
# - google_bigtable_*: provider uses gRPC InstanceAdminClient; lab is REST-only
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

  compute_custom_endpoint = "${local.ep}/compute/v1/"
}

resource "google_compute_network" "lab" {
  name                    = "${var.name_prefix}-vpc"
  auto_create_subnetworks = false
  description             = "Noctaxris-GCP Terraform lab VPC"

  # Avoid Routes.List (lab has no routes API).
  delete_default_routes_on_create = false
}

output "network_name" {
  value = google_compute_network.lab.name
}

output "network_self_link" {
  value = google_compute_network.lab.self_link
}

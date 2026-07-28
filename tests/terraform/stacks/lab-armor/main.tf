# Soft-skip: run via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set
# and /_noctaxris-gcp/ready succeeds. See docs/configuration.md.
#
# Cloud Armor: google_compute_security_policy via compute_custom_endpoint
# (default BaseUrl https://compute.googleapis.com/compute/v1/).
# Lab insert/delete return DONE compute#operation, so OperationWait
# short-circuits without Operations GET (not mounted).
#
# add_terraform_attribution_label=false: provider otherwise POSTs setLabels
# after create (lab has no setLabels).
#
# Skipped elsewhere (honest gaps):
# - google_certificate_manager_*: create returns resource (with name); provider
#   CertificateManagerOperationWaitTime treats name as LRO and polls forever
# - google_filestore_instance: same LRO wait; lab is synchronous under /file/v1/
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

  user_project_override            = false
  add_terraform_attribution_label  = false

  compute_custom_endpoint = "${local.ep}/compute/v1/"
}

resource "google_compute_security_policy" "lab" {
  name        = "${var.name_prefix}-armor"
  description = "Noctaxris-GCP Terraform lab Cloud Armor policy"
  type        = "CLOUD_ARMOR"

  rule {
    action      = "deny(403)"
    priority    = "1000"
    description = "lab deny CIDR"

    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = ["203.0.113.0/24"]
      }
    }
  }

  rule {
    action      = "allow"
    priority    = "2147483647"
    description = "default rule"

    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = ["*"]
      }
    }
  }
}

output "security_policy_name" {
  value = google_compute_security_policy.lab.name
}

output "security_policy_self_link" {
  value = google_compute_security_policy.lab.self_link
}

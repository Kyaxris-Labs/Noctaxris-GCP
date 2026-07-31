# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is unset.
# Minimal Cloud Armor attach: security policy + backend service with security_policy.
# Opt-in: STACK=lab-lb-armor or TF_GCP_PARITY=1 (not in default STACKS).
# No target HTTPS proxy, Certificate Manager, or ssl_certificates.
#
# Lab backend insert/delete and setSecurityPolicy return DONE compute#operation;
# global Operations.get satisfies provider waiters (same pattern as Cloud Armor).
#
# add_terraform_attribution_label=false: provider otherwise POSTs setLabels after policy create.
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
  description = "Noctaxris-GCP Terraform lab LB Armor policy"
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

resource "google_compute_backend_service" "lab" {
  name            = "${var.name_prefix}-bs"
  description     = "Noctaxris-GCP Terraform lab backend with Armor attach"
  protocol        = "HTTP"
  security_policy = google_compute_security_policy.lab.self_link
}

output "security_policy_self_link" {
  value = google_compute_security_policy.lab.self_link
}

output "backend_service_self_link" {
  value = google_compute_backend_service.lab.self_link
}

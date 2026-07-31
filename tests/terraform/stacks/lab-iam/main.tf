# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set.
# IAM SA CRUD is under /v1/projects/.../serviceAccounts.
# hashicorp/google >=6 often uses IAM Admin gRPC (ignores REST custom endpoint).
# Pin ~> 5.45 so iam_custom_endpoint is REST; BasePath already includes /v1/, so
# the custom endpoint must be the listener root (not .../v1/).
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.45"
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
  # Account id must be 6-30 chars, lowercase alphanumeric + hyphen.
  account_id = substr(replace(var.name_prefix, "_", "-"), 0, 30)
}

provider "google" {
  project               = var.project
  region                = "us-central1"
  user_project_override = false
  iam_custom_endpoint   = "${local.ep}/"
}

resource "google_service_account" "lab" {
  account_id   = local.account_id
  display_name = "Noctaxris-GCP lab SA"
  description  = "Terraform lab-iam stack"
}

output "email" {
  value = google_service_account.lab.email
}

output "name" {
  value = google_service_account.lab.name
}

# Soft-skip: run via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set
# and /_noctaxris-gcp/ready succeeds. See docs/configuration.md.
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

  # Lab Bearer is supplied via GOOGLE_OAUTH_ACCESS_TOKEN by run.sh.
  user_project_override = false

  storage_custom_endpoint        = "${local.ep}/storage/v1/"
  pubsub_custom_endpoint         = "${local.ep}/"
  secret_manager_custom_endpoint = "${local.ep}/"
}

# Pub/Sub lab surface is gRPC-only today; google_pubsub_* resources need REST
# and are omitted until that mirror exists.

resource "google_storage_bucket" "lab" {
  name                        = "${var.name_prefix}-bucket"
  location                    = "US"
  force_destroy               = true
  uniform_bucket_level_access = true
}

resource "google_secret_manager_secret" "lab" {
  secret_id = "${var.name_prefix}-secret"
  replication {
    auto {}
  }
}

output "bucket_name" {
  value = google_storage_bucket.lab.name
}

output "secret_id" {
  value = google_secret_manager_secret.lab.secret_id
}

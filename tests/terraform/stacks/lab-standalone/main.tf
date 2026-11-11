# Opt-in standalone project stack. Not in the default STACKS list.
# STACK=lab-standalone after the seed in README.md.
# Soft-skip: tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set
# and /_noctaxris-gcp/ready succeeds.
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
  type        = string
  description = "Seeded standalone project id. run.sh passes NOCTAXRIS_GCP_PROJECT."
}

variable "name_prefix" {
  type        = string
  description = "Unique prefix for resource names"
}

variable "access_token" {
  type        = string
  sensitive   = true
  description = "Standalone project access token. Set TF_VAR_access_token from the seed accessToken field."

  validation {
    condition     = length(var.access_token) > 0
    error_message = "Set TF_VAR_access_token to the accessToken returned by the standalone project seed."
  }
}

locals {
  ep = trimsuffix(var.endpoint, "/")
}

provider "google" {
  project = var.project
  region  = "us-central1"

  # Provider config wins over GOOGLE_OAUTH_ACCESS_TOKEN (run.sh sets that from the process root).
  # Requires hashicorp/google >= 4.0. This stack requires >= 5.0.
  access_token          = var.access_token
  user_project_override = false

  storage_custom_endpoint = "${local.ep}/storage/v1/"
}

resource "google_storage_bucket" "lab" {
  name                        = "${var.name_prefix}-bucket"
  project                     = var.project
  location                    = "US"
  force_destroy               = true
  uniform_bucket_level_access = true
}

output "bucket_name" {
  value = google_storage_bucket.lab.name
}

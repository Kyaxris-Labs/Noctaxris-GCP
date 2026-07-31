# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set.
# KMS lab paths are /v1/projects/... (provider BaseUrl includes /v1/).
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
  project               = var.project
  region                = "us-central1"
  user_project_override = false
  kms_custom_endpoint   = "${local.ep}/v1/"
}

resource "google_kms_key_ring" "lab" {
  name     = "${var.name_prefix}-ring"
  location = "global"
}

resource "google_kms_crypto_key" "lab" {
  name     = "${var.name_prefix}-key"
  key_ring = google_kms_key_ring.lab.id
  purpose  = "ENCRYPT_DECRYPT"

  version_template {
    algorithm = "GOOGLE_SYMMETRIC_ENCRYPTION"
  }
}

output "key_ring" {
  value = google_kms_key_ring.lab.id
}

output "crypto_key" {
  value = google_kms_crypto_key.lab.id
}

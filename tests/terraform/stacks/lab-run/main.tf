# Soft-skip: run via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set
# and /_noctaxris-gcp/ready succeeds. See docs/configuration.md.
#
# Cloud Run Admin API v2: hashicorp/google cloud_run_v2_custom_endpoint
# (default BaseUrl https://run.googleapis.com/v2/). Lab create/update returns
# a completed Operation so OpAsync wait treats the call as done.
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

  cloud_run_v2_custom_endpoint = "${local.ep}/v2/"
}

resource "google_cloud_run_v2_service" "lab" {
  name     = "${var.name_prefix}-svc"
  location = "us-central1"

  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"

  template {
    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello"
    }
  }

  traffic {
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
    percent = 100
  }
}

output "service_name" {
  value = google_cloud_run_v2_service.lab.name
}

output "service_uri" {
  value = google_cloud_run_v2_service.lab.uri
}

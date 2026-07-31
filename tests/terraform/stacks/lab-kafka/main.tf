# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is unset.
# Managed Kafka is location-scoped under /v1/projects/.../locations/.../clusters.
# Opt-in: STACK=lab-kafka or TF_GCP_PARITY=1 (not in default STACKS until Compose nested fail-closed is green).
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
  managed_kafka_custom_endpoint = "${local.ep}/v1/"
}

resource "google_managed_kafka_cluster" "lab" {
  cluster_id = "${var.name_prefix}-kafka"
  location   = "us-central1"

  capacity_config {
    vcpu_count   = 3
    memory_bytes = 3221225472
  }

  gcp_config {
    access_config {
      network_configs {
        subnet = "projects/${var.project}/regions/us-central1/subnetworks/default"
      }
    }
  }
}

output "cluster_id" {
  value = google_managed_kafka_cluster.lab.cluster_id
}

output "name" {
  value = google_managed_kafka_cluster.lab.name
}

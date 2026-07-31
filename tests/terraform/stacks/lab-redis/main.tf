# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set.
# Memorystore Redis is location-scoped under /v1/projects/.../locations/.../instances.
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
  redis_custom_endpoint = "${local.ep}/v1/"
}

resource "google_redis_instance" "lab" {
  name           = "${var.name_prefix}-redis"
  tier           = "BASIC"
  memory_size_gb = 1
  region         = "us-central1"
  redis_version  = "REDIS_7_0"
}

output "host" {
  value = google_redis_instance.lab.host
}

output "port" {
  value = google_redis_instance.lab.port
}

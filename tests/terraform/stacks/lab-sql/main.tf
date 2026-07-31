# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set.
# Cloud SQL Admin mounts /sql/v1 and /sql/v1beta4 (DONE Operations theatre + optional nested).
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
  sql_custom_endpoint   = "${local.ep}/sql/v1beta4/"
}

resource "google_sql_database_instance" "lab" {
  name             = "${var.name_prefix}-pg"
  database_version = "POSTGRES_15"
  region           = "us-central1"

  settings {
    tier = "db-f1-micro"
  }

  deletion_protection = false
}

output "instance_name" {
  value = google_sql_database_instance.lab.name
}

output "connection_name" {
  value = google_sql_database_instance.lab.connection_name
}

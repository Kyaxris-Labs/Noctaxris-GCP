# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is set.
# Lab BigQuery mounts under /bigquery/v2/...
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
  project                  = var.project
  region                   = "us-central1"
  user_project_override    = false
  big_query_custom_endpoint = "${local.ep}/bigquery/v2/"
}

resource "google_bigquery_dataset" "lab" {
  dataset_id                  = replace("${var.name_prefix}_ds", "-", "_")
  friendly_name               = "Noctaxris-GCP lab dataset"
  location                    = "US"
  delete_contents_on_destroy  = true
}

resource "google_bigquery_table" "lab" {
  dataset_id          = google_bigquery_dataset.lab.dataset_id
  table_id            = "lab_table"
  deletion_protection = false

  schema = jsonencode([
    {
      name = "id"
      type = "STRING"
      mode = "REQUIRED"
    },
    {
      name = "note"
      type = "STRING"
      mode = "NULLABLE"
    },
  ])
}

output "dataset_id" {
  value = google_bigquery_dataset.lab.dataset_id
}

output "table_id" {
  value = google_bigquery_table.lab.table_id
}

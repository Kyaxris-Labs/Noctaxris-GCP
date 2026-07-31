# Soft-skip via tests/terraform/run.sh when NOCTAXRIS_GCP_ENDPOINT is unset.
# Compute Engine VM with boot disk (metadata theatre; no guest OS).
# Opt-in: STACK=lab-compute-instance or TF_GCP_PARITY=1 (not in default STACKS).
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

  compute_custom_endpoint = "${local.ep}/compute/v1/"
}

resource "google_compute_network" "lab" {
  name                    = "${var.name_prefix}-vpc"
  auto_create_subnetworks = false
  description             = "Noctaxris-GCP Terraform lab VPC for VM stack"

  delete_default_routes_on_create = false
}

resource "google_compute_subnetwork" "lab" {
  name          = "${var.name_prefix}-subnet"
  ip_cidr_range = "10.42.0.0/24"
  region        = "us-central1"
  network       = google_compute_network.lab.id
}

resource "google_compute_instance" "lab" {
  name         = "${var.name_prefix}-vm"
  machine_type = "e2-micro"
  zone         = "us-central1-a"

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = 10
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.lab.id
  }

  allow_stopping_for_update = true
}

output "instance_name" {
  value = google_compute_instance.lab.name
}

output "instance_self_link" {
  value = google_compute_instance.lab.self_link
}

output "network_name" {
  value = google_compute_network.lab.name
}

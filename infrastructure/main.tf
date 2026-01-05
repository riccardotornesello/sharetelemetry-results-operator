terraform {
  backend "gcs" {
    bucket = "terraform-state-results-operator"
    prefix = "terraform/state"
  }
}


provider "google" {
  project         = var.project_id
  region          = var.region
  request_timeout = "60s"
}


locals {
  environment_variables = merge(
    {
      "MONGODB_URI"               = var.database_url
      "MONGODB_SCRAPER_DATABASE"  = var.scraper_database_name
      "MONGODB_OPERATOR_DATABASE" = var.operator_database_name
    },
  )
}


// REQUIREMENTS

resource "google_project_service" "cloudbuild" {
  service            = "cloudbuild.googleapis.com"
  disable_on_destroy = false
}
resource "google_project_service" "cloudfunctions" {
  service            = "cloudfunctions.googleapis.com"
  disable_on_destroy = false
}
resource "google_project_service" "eventarc" {
  service            = "eventarc.googleapis.com"
  disable_on_destroy = false
}
resource "google_project_service" "cloudrun" {
  service            = "run.googleapis.com"
  disable_on_destroy = false
}


// PUB/SUB

resource "google_pubsub_topic" "results_operator_topic" {
  name = "results_operator_topic"
}


// SOURCE CODE

resource "random_id" "bucket_prefix" {
  byte_length = 8
}

data "archive_file" "source_code" {
  type        = "zip"
  output_path = "/tmp/gcf-source-${random_id.bucket_prefix.hex}.zip"
  source_dir  = "${path.module}/.."
  excludes = [
    "**/*.tf",
    "**/.terraform/**",
    "**/infrastructure/**",
    "**/*.zip",
    "**/.git/**"
  ]
}

resource "google_storage_bucket" "source" {
  name                        = "gcf-source-${random_id.bucket_prefix.hex}" # Every bucket name must be globally unique
  location                    = "us-east1"                                  # Free tier requirement
  uniform_bucket_level_access = true
  force_destroy               = true
}

resource "google_storage_bucket_object" "default" {
  name   = "gcf-source-${data.archive_file.source_code.output_md5}.zip"
  bucket = google_storage_bucket.source.name
  source = data.archive_file.source_code.output_path
}


// SERVICE ACCOUNTS

resource "google_service_account" "runner" {
  account_id = "results-operator-runner"
}

resource "google_service_account" "cloudbuild" {
  account_id = "results-operator-build"
}

resource "google_service_account" "invoker" {
  account_id = "results-operator-invoker"
}

resource "google_project_iam_member" "builder_log_writer" {
  project = google_service_account.cloudbuild.project
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.cloudbuild.email}"
}

resource "google_project_iam_member" "builder_artifact_registry_writer" {
  project = google_service_account.cloudbuild.project
  role    = "roles/artifactregistry.writer"
  member  = "serviceAccount:${google_service_account.cloudbuild.email}"
}

resource "google_project_iam_member" "builder_storage_object_admin" {
  project = google_service_account.cloudbuild.project
  role    = "roles/storage.objectAdmin"
  member  = "serviceAccount:${google_service_account.cloudbuild.email}"
}

resource "google_project_iam_member" "runner_pubsub_publisher" {
  project = google_service_account.runner.project
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.runner.email}"
}

resource "google_project_iam_member" "runner_log_writer" {
  project = google_service_account.runner.project
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.runner.email}"
}


// CLOUD FUNCTION

resource "google_cloudfunctions2_function" "api" {
  name     = "results-operator"
  location = var.region

  depends_on = [
    google_project_service.cloudbuild,
    google_project_service.cloudfunctions,
    google_project_service.eventarc,
    google_project_service.cloudrun,
    google_storage_bucket_object.default,
  ]

  build_config {
    runtime         = "go125"
    entry_point     = "ProcessCompetition"
    service_account = google_service_account.cloudbuild.id

    source {
      storage_source {
        bucket = google_storage_bucket.source.name
        object = google_storage_bucket_object.default.name
      }
    }
  }

  service_config {
    max_instance_count               = 1
    min_instance_count               = 0
    max_instance_request_concurrency = 80
    available_memory                 = "256M"
    available_cpu                    = "1"
    timeout_seconds                  = 540
    environment_variables            = local.environment_variables
    ingress_settings                 = "ALLOW_INTERNAL_ONLY"
    all_traffic_on_latest_revision   = true
    service_account_email            = google_service_account.runner.email
  }

  event_trigger {
    trigger_region        = var.region
    event_type            = "google.cloud.pubsub.topic.v1.messagePublished"
    pubsub_topic          = google_pubsub_topic.results_operator_topic.id
    retry_policy          = "RETRY_POLICY_RETRY"
    service_account_email = google_service_account.invoker.email
  }
}

resource "google_cloudfunctions2_function_iam_member" "invoker_api" {
  project        = google_cloudfunctions2_function.api.project
  location       = google_cloudfunctions2_function.api.location
  cloud_function = google_cloudfunctions2_function.api.name
  role           = "roles/cloudfunctions.invoker"
  member         = "serviceAccount:${google_service_account.invoker.email}"
}

resource "google_cloud_run_service_iam_member" "invoker_api" {
  project  = google_cloudfunctions2_function.api.project
  location = google_cloudfunctions2_function.api.location
  service  = google_cloudfunctions2_function.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.invoker.email}"
}

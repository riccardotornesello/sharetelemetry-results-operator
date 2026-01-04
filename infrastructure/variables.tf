// PROJECT

variable "region" {
  description = "The Google Cloud region where the function will be deployed."
  type        = string
  default     = "europe-west1"
}

variable "project_id" {
  description = "The Google Cloud project ID."
  type        = string
}


// DATABASE

variable "database_url" {
  description = "The url for the database."
  type        = string
}

variable "scraper_database_name" {
  description = "The name of the scraper database."
  type        = string
}

variable "operator_database_name" {
  description = "The name of the operator database."
  type        = string
}

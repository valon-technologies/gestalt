terraform {
  backend "gcs" {
    bucket = "gestalt-terraform-state"
    prefix = "gestalt-docs"
  }
}

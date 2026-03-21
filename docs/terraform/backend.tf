terraform {
  backend "gcs" {
    bucket = "toolshed-terraform-state"
    prefix = "toolshed-docs"
  }
}

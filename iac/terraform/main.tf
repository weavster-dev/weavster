# Sample Terraform module provisioning Weavster as a single static binary on a
# small VM with a managed Postgres. The platform config is applied *as data*
# by the same GitOps pipeline (config push -> plan -> apply).

variable "database_url" {
  type        = string
  description = "Postgres connection URL (or sqlite:// path for local DX)"
}

variable "config_repo" {
  type        = string
  description = "Git URL of the config-as-code (flows) repository"
}

variable "tls_cert_arn" {
  type        = string
  description = "TLS certificate ARN for the API listener"
}

variable "enable_mtls" {
  type    = bool
  default = true
}

variable "metrics_enabled" {
  type    = bool
  default = true
}

locals {
  image = "weavster:latest"
}

resource "docker_container" "weavster" {
  name  = "weavster"
  image = local.image
  ports {
    internal = 8080
    external = 8080
  }
  env = [
    "WEAVSTER_DATABASE_URL=${var.database_url}",
    "WEAVSTER_CONFIG_REPO=${var.config_repo}",
    "WEAVSTER_TLS_CERT_ARN=${var.tls_cert_arn}",
    "WEAVSTER_ENABLE_MTLS=${var.enable_mtls}",
    "WEAVSTER_METRICS_ENABLED=${var.metrics_enabled}",
  ]
}

terraform {
  required_version = ">= 1.6.0"
}

variable "project_id" {
  type    = string
  default = "contract-radar-demo"
}

locals {
  nat_addresses = ["34.120.10.10", "34.120.10.11"]
  ingress_ip    = "34.98.20.5"
}

# Deliberately modeled as local state for the POC: no cloud credentials or
# apply step is required to demonstrate contract checking.
output "declared_platform_state" {
  value = {
    nat_ip_mode      = "static"
    addresses        = local.nat_addresses
    ingress_address  = local.ingress_ip
    provisioned_iops = 3000
  }
}


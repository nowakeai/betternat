packer {
  required_plugins {
    amazon = {
      version = ">= 1.3.0"
      source  = "github.com/hashicorp/amazon"
    }
  }
}

variable "aws_region" {
  type    = string
  default = "us-west-2"
}

variable "ami_regions" {
  type    = list(string)
  default = []
}

variable "ami_users" {
  type    = list(string)
  default = []
}

variable "ami_groups" {
  type    = list(string)
  default = []
}

variable "snapshot_users" {
  type    = list(string)
  default = []
}

variable "snapshot_groups" {
  type    = list(string)
  default = []
}

variable "prefix" {
  type    = string
  default = "betternat"
}

variable "flavor" {
  type    = string
  default = "al2023"
}

variable "virtualization_type" {
  type    = string
  default = "hvm"
}

variable "suffix" {
  type    = string
  default = "ebs"
}

variable "architecture" {
  type    = string
  default = "arm64"
}

variable "instance_type" {
  type = map(string)
  default = {
    arm64  = "t4g.small"
    x86_64 = "t3.small"
  }
}

variable "base_image_name" {
  type    = string
  default = "*al2023-ami-minimal-*-kernel-6.12-*"
}

variable "base_image_owner" {
  type    = string
  default = "amazon"
}

variable "ssh_username" {
  type    = string
  default = "ec2-user"
}

variable "version" {
  type = string
}

variable "agent_binary_path" {
  type = string
}

variable "cli_binary_path" {
  type = string
}

variable "loxilb_image" {
  type    = string
  default = "ghcr.io/loxilb-io/loxilb@sha256:dacc9b21688d4042b768f2cbc5968360b8753cf92f926ee288346153a23f3052"
}

variable "loxilb_deb_url" {
  type    = string
  default = ""
}

variable "loxilb_deb_sha256" {
  type    = string
  default = ""
}

variable "loxilb_runtime" {
  type    = string
  default = "docker"
}

variable "provision_script" {
  type    = string
  default = "scripts/ami/provision-betternat-ami.sh"
}

variable "manifest_output" {
  type    = string
  default = "tmp/packer/betternat-manifest.json"
}

locals {
  build_date    = formatdate("YYYYMMDD", timestamp())
  instance_type = lookup(var.instance_type, var.architecture, "error")
}

source "amazon-ebs" "betternat" {
  region                    = var.aws_region
  ami_regions               = var.ami_regions
  ami_users                 = var.ami_users
  ami_groups                = var.ami_groups
  snapshot_users            = var.snapshot_users
  snapshot_groups           = var.snapshot_groups
  instance_type             = local.instance_type
  ssh_username              = var.ssh_username
  ssh_clear_authorized_keys = true
  temporary_key_pair_type   = "ed25519"

  ami_name                = "${var.prefix}-${var.flavor}-${var.virtualization_type}-${var.version}-${local.build_date}-${var.architecture}-${var.suffix}"
  ami_virtualization_type = var.virtualization_type
  ami_description         = "BetterNAT ${var.version} ${var.architecture} AMI"

  source_ami_filter {
    filters = {
      name                = var.base_image_name
      architecture        = var.architecture
      root-device-type    = "ebs"
      virtualization-type = var.virtualization_type
    }
    owners      = [var.base_image_owner]
    most_recent = true
  }

  launch_block_device_mappings {
    device_name           = "/dev/xvda"
    volume_size           = 8
    volume_type           = "gp3"
    delete_on_termination = true
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  tags = {
    Name             = "BetterNAT ${var.version}"
    BetterNATVersion = var.version
    BetterNATFlavor  = var.flavor
    Architecture     = var.architecture
    ManagedBy        = "betternat"
  }

  run_tags = {
    Name      = "betternat-ami-build-${var.version}-${var.architecture}"
    ManagedBy = "betternat"
    Purpose   = "ami-build"
  }

  snapshot_tags = {
    Name             = "BetterNAT ${var.version} ${var.architecture}"
    BetterNATVersion = var.version
    ManagedBy        = "betternat"
  }
}

build {
  sources = ["source.amazon-ebs.betternat"]

  provisioner "file" {
    source      = var.agent_binary_path
    destination = "/tmp/betternat-agent"
  }

  provisioner "file" {
    source      = var.cli_binary_path
    destination = "/tmp/betternat"
  }

  provisioner "file" {
    source      = var.provision_script
    destination = "/tmp/provision-betternat-ami.sh"
  }

  provisioner "file" {
    source      = "LICENSE"
    destination = "/tmp/betternat-LICENSE"
  }

  provisioner "file" {
    source      = "THIRD_PARTY_NOTICES.md"
    destination = "/tmp/betternat-THIRD_PARTY_NOTICES.md"
  }

  provisioner "shell" {
    environment_vars = [
      "BETTERNAT_VERSION=${var.version}",
      "BETTERNAT_LOXILB_IMAGE=${var.loxilb_image}",
      "BETTERNAT_LOXILB_DEB_URL=${var.loxilb_deb_url}",
      "BETTERNAT_LOXILB_DEB_SHA256=${var.loxilb_deb_sha256}",
      "BETTERNAT_LOXILB_RUNTIME=${var.loxilb_runtime}",
    ]
    inline = [
      "chmod +x /tmp/provision-betternat-ami.sh",
      "sudo -E /tmp/provision-betternat-ami.sh",
    ]
  }

  post-processor "manifest" {
    output     = var.manifest_output
    strip_path = true
  }
}

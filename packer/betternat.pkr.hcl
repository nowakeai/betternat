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

variable "ami_name_prefix" {
  type    = string
  default = "betternat-al2023-hvm"
}

variable "architecture" {
  type    = string
  default = "arm64"
}

variable "instance_type" {
  type    = string
  default = "t4g.small"
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

locals {
  timestamp = regex_replace(timestamp(), "[- TZ:]", "")
}

source "amazon-ebs" "betternat" {
  region        = var.aws_region
  instance_type = var.instance_type
  ssh_username  = var.ssh_username

  ami_name        = "${var.ami_name_prefix}-${var.version}-${local.timestamp}-${var.architecture}-ebs"
  ami_description = "BetterNAT ${var.version} ${var.architecture} AMI"

  source_ami_filter {
    filters = {
      name                = "al2023-ami-*-kernel-*-hvm-*-gp3"
      architecture        = var.architecture
      root-device-type    = "ebs"
      virtualization-type = "hvm"
    }
    owners      = ["amazon"]
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
    source      = "scripts/ami/provision-betternat-ami.sh"
    destination = "/tmp/provision-betternat-ami.sh"
  }

  provisioner "shell" {
    environment_vars = [
      "BETTERNAT_VERSION=${var.version}",
      "BETTERNAT_LOXILB_IMAGE=${var.loxilb_image}",
    ]
    inline = [
      "chmod +x /tmp/provision-betternat-ami.sh",
      "sudo -E /tmp/provision-betternat-ami.sh",
    ]
  }
}

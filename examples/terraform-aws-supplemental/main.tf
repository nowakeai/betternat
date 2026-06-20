terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
    betternat = {
      source = "betternat/betternat"
    }
  }
}

variable "region" {
  type    = string
  default = "us-west-2"
}

variable "az" {
  type    = string
  default = "us-west-2a"
}

variable "run_id" {
  type = string
}

variable "instance_type" {
  type    = string
  default = "t4g.small"
}

variable "agent_binary_url" {
  type      = string
  sensitive = true
}

variable "loxicmd_binary_url" {
  type      = string
  default   = ""
  sensitive = true
}

provider "aws" {
  region = var.region
}

provider "betternat" {}

locals {
  tags = {
    Name           = var.run_id
    ManagedBy      = "betternat-supplemental-test"
    BetterNATRunId = var.run_id
  }
}

data "aws_ami" "al2023_arm64" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-arm64"]
  }

  filter {
    name   = "architecture"
    values = ["arm64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_vpc" "main" {
  cidr_block           = "10.88.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(local.tags, {
    Name = "${var.run_id}-vpc"
  })
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = merge(local.tags, {
    Name = "${var.run_id}-igw"
  })
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.88.1.0/24"
  availability_zone       = var.az
  map_public_ip_on_launch = true

  tags = merge(local.tags, {
    Name = "${var.run_id}-public-a"
  })
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  tags = merge(local.tags, {
    Name = "${var.run_id}-public"
  })
}

resource "aws_route" "public_default" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

resource "aws_subnet" "private" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.88.11.0/24"
  availability_zone = var.az

  tags = merge(local.tags, {
    Name = "${var.run_id}-private-a"
  })
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  tags = merge(local.tags, {
    Name = "${var.run_id}-private"
  })
}

resource "aws_route_table_association" "private" {
  subnet_id      = aws_subnet.private.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route" "initial_private_default" {
  route_table_id         = aws_route_table.private.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "betternat_gateway" "egress" {
  name   = var.run_id
  region = var.region
  vpc_id = aws_vpc.main.id

  ami_id        = data.aws_ami.al2023_arm64.id
  instance_type = var.instance_type
  use_spot      = true

  agent_binary_url   = var.agent_binary_url
  loxicmd_binary_url = var.loxicmd_binary_url

  public_subnet_ids = {
    (var.az) = aws_subnet.public.id
  }

  private_route_table_ids = {
    (var.az) = [aws_route_table.private.id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]

  stable_egress_ip    = true
  rollback_on_destroy = true

  tags = local.tags

  depends_on = [
    aws_route.public_default,
    aws_route.initial_private_default,
  ]
}

output "run_id" {
  value = var.run_id
}

output "private_route_table_id" {
  value = aws_route_table.private.id
}

output "private_subnet_id" {
  value = aws_subnet.private.id
}

output "betternat_status" {
  value = betternat_gateway.egress.status
}

output "egress_public_ips" {
  value = betternat_gateway.egress.egress_public_ips
}

output "active_instance_ids" {
  value = betternat_gateway.egress.active_instance_ids
}

output "standby_instance_ids" {
  value = betternat_gateway.egress.standby_instance_ids
}

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0"
    }
  }
}

# Disposable AWS validation fixture for the BetterNAT Quick Start.
#
# This creates a disposable VPC, one BetterNAT gateway HA group, and one private
# client instance for live egress/failover validation. It intentionally uses Spot
# instances to keep test cost low. It is intentionally more complete than the
# minimal shape in examples/terraform/main.tf.

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

variable "min_size" {
  type    = number
  default = 1
}

variable "desired_capacity" {
  type    = number
  default = 2
}

variable "max_size" {
  type    = number
  default = 3
}

variable "stable_egress_ip" {
  type    = bool
  default = true
}

variable "ha_profile" {
  type    = string
  default = "default"
}

variable "betternat_version" {
  type    = string
  default = "v0.1.0"
}

variable "agent_binary_url" {
  type      = string
  default   = null
  sensitive = true
}

variable "agent_binary_sha256" {
  type    = string
  default = null
}

variable "cli_binary_url" {
  type      = string
  default   = null
  sensitive = true
}

variable "cli_binary_sha256" {
  type    = string
  default = null
}

variable "loxicmd_binary_url" {
  type      = string
  default   = null
  sensitive = true
}

variable "loxicmd_binary_sha256" {
  type    = string
  default = null
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

resource "terraform_data" "initial_private_default" {
  triggers_replace = {
    route_table_id = aws_route_table.private.id
    gateway_id     = aws_internet_gateway.main.id
    region         = var.region
  }

  provisioner "local-exec" {
    command = "aws ec2 create-route --region ${self.triggers_replace.region} --route-table-id ${self.triggers_replace.route_table_id} --destination-cidr-block 0.0.0.0/0 --gateway-id ${self.triggers_replace.gateway_id} || aws ec2 replace-route --region ${self.triggers_replace.region} --route-table-id ${self.triggers_replace.route_table_id} --destination-cidr-block 0.0.0.0/0 --gateway-id ${self.triggers_replace.gateway_id}"
  }

  depends_on = [
    aws_internet_gateway.main,
    aws_route_table_association.private,
  ]
}

resource "aws_security_group" "private_client" {
  name        = "${var.run_id}-private-client"
  description = "BetterNAT supplemental private client"
  vpc_id      = aws_vpc.main.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(local.tags, {
    Name = "${var.run_id}-private-client"
  })
}

resource "aws_iam_role" "private_client" {
  name = "${var.run_id}-private-client"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = merge(local.tags, {
    Name = "${var.run_id}-private-client"
  })
}

resource "aws_iam_role_policy_attachment" "private_client_ssm" {
  role       = aws_iam_role.private_client.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "private_client" {
  name = "${var.run_id}-private-client"
  role = aws_iam_role.private_client.name

  tags = merge(local.tags, {
    Name = "${var.run_id}-private-client"
  })
}

resource "betternat_aws_gateway" "egress" {
  name   = var.run_id
  region = var.region
  vpc_id = aws_vpc.main.id

  ami_id           = data.aws_ami.al2023_arm64.id
  instance_type    = var.instance_type
  use_spot         = true
  min_size         = var.min_size
  desired_capacity = var.desired_capacity
  max_size         = var.max_size

  betternat_version     = var.betternat_version
  agent_binary_url      = var.agent_binary_url
  agent_binary_sha256   = var.agent_binary_sha256
  cli_binary_url        = var.cli_binary_url
  cli_binary_sha256     = var.cli_binary_sha256
  loxicmd_binary_url    = var.loxicmd_binary_url
  loxicmd_binary_sha256 = var.loxicmd_binary_sha256

  public_subnet_ids = {
    (var.az) = aws_subnet.public.id
  }

  private_route_table_ids = {
    (var.az) = [aws_route_table.private.id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]

  stable_egress_ip    = var.stable_egress_ip
  ha_profile          = var.ha_profile
  rollback_on_destroy = true

  tags = local.tags

  depends_on = [
    aws_route.public_default,
    terraform_data.initial_private_default,
  ]
}

resource "aws_instance" "private_client" {
  ami                         = data.aws_ami.al2023_arm64.id
  instance_type               = "t4g.small"
  subnet_id                   = aws_subnet.private.id
  vpc_security_group_ids      = [aws_security_group.private_client.id]
  iam_instance_profile        = aws_iam_instance_profile.private_client.name
  associate_public_ip_address = false

  instance_market_options {
    market_type = "spot"

    spot_options {
      spot_instance_type             = "one-time"
      instance_interruption_behavior = "terminate"
    }
  }

  tags = merge(local.tags, {
    Name = "${var.run_id}-private-client"
  })

  depends_on = [
    betternat_aws_gateway.egress,
    aws_iam_role_policy_attachment.private_client_ssm,
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

output "asg_name" {
  value = "betternat-${var.run_id}-${var.az}"
}

output "private_client_instance_id" {
  value = aws_instance.private_client.id
}

output "betternat_status" {
  value = betternat_aws_gateway.egress.status
}

output "egress_public_ips" {
  value = betternat_aws_gateway.egress.egress_public_ips
}

output "active_instance_ids" {
  value = betternat_aws_gateway.egress.active_instance_ids
}

output "standby_instance_ids" {
  value = betternat_aws_gateway.egress.standby_instance_ids
}

output "aws_cli_context" {
  value = {
    region                 = var.region
    az                     = var.az
    run_id                 = var.run_id
    asg_name               = "betternat-${var.run_id}-${var.az}"
    private_route_table_id = aws_route_table.private.id
    private_client_id      = aws_instance.private_client.id
    stable_egress_ip       = var.stable_egress_ip
  }
}

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0-alpha.2"
    }
  }
}

provider "aws" {
  access_key                  = "test"
  secret_key                  = "test"
  region                      = "us-east-1"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    autoscaling = "http://localhost:4566"
    dynamodb    = "http://localhost:4566"
    ec2         = "http://localhost:4566"
    iam         = "http://localhost:4566"
    sts         = "http://localhost:4566"
  }
}

provider "betternat" {
  aws_endpoint_url = "http://localhost:4566"
}

resource "aws_vpc" "main" {
  cidr_block = "10.99.0.0/16"

  tags = {
    Name      = "betternat-localstack"
    ManagedBy = "betternat-localstack"
  }
}

resource "aws_subnet" "public" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.99.1.0/24"
  availability_zone = "us-east-1a"

  tags = {
    Name      = "betternat-localstack-public-a"
    ManagedBy = "betternat-localstack"
  }
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name      = "betternat-localstack-private-a"
    ManagedBy = "betternat-localstack"
  }
}

resource "aws_internet_gateway" "rollback_target" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name      = "betternat-localstack-rollback-target"
    ManagedBy = "betternat-localstack"
  }
}

resource "aws_route" "initial_private_default" {
  route_table_id         = aws_route_table.private.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.rollback_target.id
}

resource "betternat_gateway" "egress" {
  name   = "localstack-egress"
  region = "us-east-1"
  vpc_id = aws_vpc.main.id

  ami_id           = "ami-localstack"
  instance_type    = "t3.small"
  min_size         = 1
  desired_capacity = 2
  max_size         = 3

  public_subnet_ids = {
    us-east-1a = aws_subnet.public.id
  }

  private_route_table_ids = {
    us-east-1a = [aws_route_table.private.id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]

  stable_egress_ip    = true
  ha_profile          = "stable"
  rollback_on_destroy = true

  depends_on = [
    aws_route.initial_private_default
  ]
}

output "betternat_status" {
  value = betternat_gateway.egress.status
}

output "betternat_control_plane_status_json" {
  value = betternat_gateway.egress.control_plane_status_json
}

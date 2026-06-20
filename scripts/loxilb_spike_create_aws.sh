#!/usr/bin/env bash
set -euo pipefail

export AWS_PAGER=""
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy

PROFILE="${AWS_PROFILE:-default}"
REGION="${AWS_REGION:-us-west-2}"
AZ="${AWS_AZ:-us-west-2a}"
SPIKE_ID="${SPIKE_ID:-betternat-loxilb-20260619T154348Z}"
STATE_FILE="${STATE_FILE:-tmp/${SPIKE_ID}.json}"

VPC_CIDR="10.77.0.0/16"
PUBLIC_CIDR="10.77.1.0/24"
PRIVATE_CIDR="10.77.2.0/24"

awsx() {
  aws --profile "$PROFILE" --region "$REGION" "$@"
}

tag_spec() {
  local type="$1"
  printf 'ResourceType=%s,Tags=[{Key=Project,Value=BetterNAT},{Key=Purpose,Value=LoxiLBSpike},{Key=SpikeId,Value=%s},{Key=Owner,Value=Codex}]' "$type" "$SPIKE_ID"
}

write_state() {
  local key="$1"
  local value="$2"
  tmp_file="${STATE_FILE}.tmp"
  jq --arg key "$key" --arg value "$value" '. + {($key): $value}' "$STATE_FILE" > "$tmp_file"
  mv "$tmp_file" "$STATE_FILE"
}

mkdir -p "$(dirname "$STATE_FILE")"
if [[ -e "$STATE_FILE" ]]; then
  echo "State file already exists: $STATE_FILE" >&2
  exit 1
fi
printf '{"spike_id":"%s","region":"%s","az":"%s"}\n' "$SPIKE_ID" "$REGION" "$AZ" > "$STATE_FILE"

echo "Creating VPC"
VPC_ID="$(awsx ec2 create-vpc \
  --cidr-block "$VPC_CIDR" \
  --tag-specifications "$(tag_spec vpc)" \
  --query 'Vpc.VpcId' \
  --output text)"
write_state vpc_id "$VPC_ID"
awsx ec2 modify-vpc-attribute --vpc-id "$VPC_ID" --enable-dns-support '{"Value":true}'
awsx ec2 modify-vpc-attribute --vpc-id "$VPC_ID" --enable-dns-hostnames '{"Value":true}'

echo "Creating subnets"
PUBLIC_SUBNET_ID="$(awsx ec2 create-subnet \
  --vpc-id "$VPC_ID" \
  --cidr-block "$PUBLIC_CIDR" \
  --availability-zone "$AZ" \
  --tag-specifications "$(tag_spec subnet)" \
  --query 'Subnet.SubnetId' \
  --output text)"
write_state public_subnet_id "$PUBLIC_SUBNET_ID"
awsx ec2 modify-subnet-attribute --subnet-id "$PUBLIC_SUBNET_ID" --map-public-ip-on-launch

PRIVATE_SUBNET_ID="$(awsx ec2 create-subnet \
  --vpc-id "$VPC_ID" \
  --cidr-block "$PRIVATE_CIDR" \
  --availability-zone "$AZ" \
  --tag-specifications "$(tag_spec subnet)" \
  --query 'Subnet.SubnetId' \
  --output text)"
write_state private_subnet_id "$PRIVATE_SUBNET_ID"

echo "Creating internet gateway and routes"
IGW_ID="$(awsx ec2 create-internet-gateway \
  --tag-specifications "$(tag_spec internet-gateway)" \
  --query 'InternetGateway.InternetGatewayId' \
  --output text)"
write_state igw_id "$IGW_ID"
awsx ec2 attach-internet-gateway --vpc-id "$VPC_ID" --internet-gateway-id "$IGW_ID"

PUBLIC_RT_ID="$(awsx ec2 create-route-table \
  --vpc-id "$VPC_ID" \
  --tag-specifications "$(tag_spec route-table)" \
  --query 'RouteTable.RouteTableId' \
  --output text)"
write_state public_route_table_id "$PUBLIC_RT_ID"
awsx ec2 create-route --route-table-id "$PUBLIC_RT_ID" --destination-cidr-block 0.0.0.0/0 --gateway-id "$IGW_ID" >/dev/null
PUBLIC_RTA_ID="$(awsx ec2 associate-route-table \
  --route-table-id "$PUBLIC_RT_ID" \
  --subnet-id "$PUBLIC_SUBNET_ID" \
  --query 'AssociationId' \
  --output text)"
write_state public_route_table_association_id "$PUBLIC_RTA_ID"

PRIVATE_RT_ID="$(awsx ec2 create-route-table \
  --vpc-id "$VPC_ID" \
  --tag-specifications "$(tag_spec route-table)" \
  --query 'RouteTable.RouteTableId' \
  --output text)"
write_state private_route_table_id "$PRIVATE_RT_ID"
PRIVATE_RTA_ID="$(awsx ec2 associate-route-table \
  --route-table-id "$PRIVATE_RT_ID" \
  --subnet-id "$PRIVATE_SUBNET_ID" \
  --query 'AssociationId' \
  --output text)"
write_state private_route_table_association_id "$PRIVATE_RTA_ID"

echo "Creating security groups"
LOXILB_SG_ID="$(awsx ec2 create-security-group \
  --group-name "${SPIKE_ID}-loxilb" \
  --description "BetterNAT LoxiLB spike appliance" \
  --vpc-id "$VPC_ID" \
  --tag-specifications "$(tag_spec security-group)" \
  --query 'GroupId' \
  --output text)"
write_state loxilb_security_group_id "$LOXILB_SG_ID"
awsx ec2 authorize-security-group-ingress --group-id "$LOXILB_SG_ID" --ip-permissions "IpProtocol=-1,IpRanges=[{CidrIp=$VPC_CIDR,Description=VPC-forwarded-traffic}]" >/dev/null

CLIENT_SG_ID="$(awsx ec2 create-security-group \
  --group-name "${SPIKE_ID}-client" \
  --description "BetterNAT LoxiLB spike private client" \
  --vpc-id "$VPC_ID" \
  --tag-specifications "$(tag_spec security-group)" \
  --query 'GroupId' \
  --output text)"
write_state client_security_group_id "$CLIENT_SG_ID"

echo "Creating IAM role and instance profile"
ROLE_NAME="${SPIKE_ID}-ssm-role"
PROFILE_NAME="${SPIKE_ID}-instance-profile"
write_state iam_role_name "$ROLE_NAME"
write_state instance_profile_name "$PROFILE_NAME"

TRUST_FILE="tmp/${SPIKE_ID}-trust.json"
cat > "$TRUST_FILE" <<'JSON'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"Service": "ec2.amazonaws.com"},
      "Action": "sts:AssumeRole"
    }
  ]
}
JSON
awsx iam create-role \
  --role-name "$ROLE_NAME" \
  --assume-role-policy-document "file://${TRUST_FILE}" \
  --tags "Key=Project,Value=BetterNAT" "Key=Purpose,Value=LoxiLBSpike" "Key=SpikeId,Value=${SPIKE_ID}" "Key=Owner,Value=Codex" >/dev/null
awsx iam attach-role-policy \
  --role-name "$ROLE_NAME" \
  --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
awsx iam create-instance-profile --instance-profile-name "$PROFILE_NAME" >/dev/null
awsx iam add-role-to-instance-profile --instance-profile-name "$PROFILE_NAME" --role-name "$ROLE_NAME"
sleep 10

echo "Resolving AMIs"
UBUNTU_AMI="$(awsx ssm get-parameter \
  --name /aws/service/canonical/ubuntu/server/20.04/stable/current/amd64/hvm/ebs-gp2/ami-id \
  --query 'Parameter.Value' \
  --output text)"
write_state ubuntu_ami "$UBUNTU_AMI"
AL2023_AMI="$(awsx ssm get-parameter \
  --name /aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 \
  --query 'Parameter.Value' \
  --output text)"
write_state al2023_ami "$AL2023_AMI"

echo "Writing user data"
LOXILB_USER_DATA="tmp/${SPIKE_ID}-loxilb-user-data.sh"
cat > "$LOXILB_USER_DATA" <<'SCRIPT'
#!/usr/bin/env bash
set -euxo pipefail
exec > >(tee /var/log/betternat-loxilb-user-data.log | logger -t betternat-loxilb-user-data -s 2>/dev/console) 2>&1

sysctl -w net.ipv4.ip_forward=1
printf 'net.ipv4.ip_forward=1\n' >/etc/sysctl.d/99-betternat.conf

if command -v snap >/dev/null 2>&1; then
  snap start amazon-ssm-agent || true
fi
systemctl enable amazon-ssm-agent || true
systemctl start amazon-ssm-agent || true

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io curl iproute2 iptables nftables conntrack jq
systemctl enable docker
systemctl start docker

docker pull ghcr.io/loxilb-io/loxilb:latest
docker rm -f loxilb || true
docker run -d \
  --name loxilb \
  --restart unless-stopped \
  --privileged \
  --network host \
  ghcr.io/loxilb-io/loxilb:latest

sleep 10
docker logs loxilb || true
docker exec loxilb loxicmd --help || true
SCRIPT

CLIENT_USER_DATA="tmp/${SPIKE_ID}-client-user-data.sh"
cat > "$CLIENT_USER_DATA" <<'SCRIPT'
#!/usr/bin/env bash
set -euxo pipefail
exec > >(tee /var/log/betternat-client-user-data.log | logger -t betternat-client-user-data -s 2>/dev/console) 2>&1

systemctl enable amazon-ssm-agent || true
systemctl start amazon-ssm-agent || true

for i in $(seq 1 20); do
  date
  curl -4 --connect-timeout 5 --max-time 15 https://checkip.amazonaws.com || true
  curl -4 --connect-timeout 5 --max-time 15 -I https://example.com || true
  sleep 15
done
SCRIPT

echo "Launching LoxiLB spot instance"
LOXILB_INSTANCE_ID="$(awsx ec2 run-instances \
  --image-id "$UBUNTU_AMI" \
  --instance-type t3.small \
  --subnet-id "$PUBLIC_SUBNET_ID" \
  --security-group-ids "$LOXILB_SG_ID" \
  --iam-instance-profile "Name=${PROFILE_NAME}" \
  --instance-market-options 'MarketType=spot,SpotOptions={InstanceInterruptionBehavior=terminate}' \
  --user-data "file://${LOXILB_USER_DATA}" \
  --tag-specifications "$(tag_spec instance)" "$(tag_spec spot-instances-request)" "$(tag_spec volume)" \
  --query 'Instances[0].InstanceId' \
  --output text)"
write_state loxilb_instance_id "$LOXILB_INSTANCE_ID"

awsx ec2 wait instance-running --instance-ids "$LOXILB_INSTANCE_ID"
awsx ec2 modify-instance-attribute --instance-id "$LOXILB_INSTANCE_ID" --no-source-dest-check
LOXILB_ENI_ID="$(awsx ec2 describe-instances \
  --instance-ids "$LOXILB_INSTANCE_ID" \
  --query 'Reservations[0].Instances[0].NetworkInterfaces[0].NetworkInterfaceId' \
  --output text)"
write_state loxilb_eni_id "$LOXILB_ENI_ID"

echo "Allocating and associating EIP"
EIP_ALLOC_ID="$(awsx ec2 allocate-address \
  --domain vpc \
  --tag-specifications "$(tag_spec elastic-ip)" \
  --query 'AllocationId' \
  --output text)"
write_state eip_allocation_id "$EIP_ALLOC_ID"
EIP_ASSOC_ID="$(awsx ec2 associate-address \
  --instance-id "$LOXILB_INSTANCE_ID" \
  --allocation-id "$EIP_ALLOC_ID" \
  --query 'AssociationId' \
  --output text)"
write_state eip_association_id "$EIP_ASSOC_ID"
EIP_PUBLIC_IP="$(awsx ec2 describe-addresses \
  --allocation-ids "$EIP_ALLOC_ID" \
  --query 'Addresses[0].PublicIp' \
  --output text)"
write_state eip_public_ip "$EIP_PUBLIC_IP"

echo "Creating private route to LoxiLB instance"
awsx ec2 create-route \
  --route-table-id "$PRIVATE_RT_ID" \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id "$LOXILB_INSTANCE_ID" >/dev/null
write_state private_default_route_target "$LOXILB_INSTANCE_ID"

echo "Launching private client spot instance"
CLIENT_INSTANCE_ID="$(awsx ec2 run-instances \
  --image-id "$AL2023_AMI" \
  --instance-type t3.micro \
  --subnet-id "$PRIVATE_SUBNET_ID" \
  --security-group-ids "$CLIENT_SG_ID" \
  --iam-instance-profile "Name=${PROFILE_NAME}" \
  --instance-market-options 'MarketType=spot,SpotOptions={InstanceInterruptionBehavior=terminate}' \
  --user-data "file://${CLIENT_USER_DATA}" \
  --tag-specifications "$(tag_spec instance)" "$(tag_spec spot-instances-request)" "$(tag_spec volume)" \
  --query 'Instances[0].InstanceId' \
  --output text)"
write_state client_instance_id "$CLIENT_INSTANCE_ID"

awsx ec2 wait instance-running --instance-ids "$CLIENT_INSTANCE_ID"

echo "Created spike resources."
echo "State: $STATE_FILE"
jq . "$STATE_FILE"

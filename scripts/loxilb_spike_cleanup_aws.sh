#!/usr/bin/env bash
set -euo pipefail

export AWS_PAGER=""
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy

PROFILE="${AWS_PROFILE:-default}"
REGION="${AWS_REGION:-us-west-2}"
SPIKE_ID="${SPIKE_ID:-betternat-loxilb-20260619T154348Z}"
STATE_FILE="${STATE_FILE:-tmp/${SPIKE_ID}.json}"

awsx() {
  aws --profile "$PROFILE" --region "$REGION" "$@"
}

IDS=()
EIPS=()

state_get() {
  local key="$1"
  if [[ -f "$STATE_FILE" ]]; then
    jq -r --arg key "$key" '.[$key] // empty' "$STATE_FILE"
  fi
}

echo "Cleaning BetterNAT LoxiLB spike resources for SpikeId=${SPIKE_ID}"

LOXILB_INSTANCE_ID="$(state_get loxilb_instance_id)"
CLIENT_INSTANCE_ID="$(state_get client_instance_id)"
EIP_ASSOC_ID="$(state_get eip_association_id)"
EIP_ALLOC_ID="$(state_get eip_allocation_id)"
PUBLIC_RTA_ID="$(state_get public_route_table_association_id)"
PRIVATE_RTA_ID="$(state_get private_route_table_association_id)"
PUBLIC_RT_ID="$(state_get public_route_table_id)"
PRIVATE_RT_ID="$(state_get private_route_table_id)"
PUBLIC_SUBNET_ID="$(state_get public_subnet_id)"
PRIVATE_SUBNET_ID="$(state_get private_subnet_id)"
IGW_ID="$(state_get igw_id)"
VPC_ID="$(state_get vpc_id)"
LOXILB_SG_ID="$(state_get loxilb_security_group_id)"
CLIENT_SG_ID="$(state_get client_security_group_id)"
ROLE_NAME="$(state_get iam_role_name)"
PROFILE_NAME="$(state_get instance_profile_name)"

echo "Terminating instances"
IDS_TEXT="$(awsx ec2 describe-instances \
  --filters "Name=tag:SpikeId,Values=${SPIKE_ID}" "Name=instance-state-name,Values=pending,running,stopping,stopped" \
  --query 'Reservations[].Instances[].InstanceId' \
  --output text | tr '\t' '\n' | sed '/^$/d')"
if [[ -z "$IDS_TEXT" && -n "$LOXILB_INSTANCE_ID" && -n "$CLIENT_INSTANCE_ID" ]]; then
  IDS_TEXT="${LOXILB_INSTANCE_ID}
${CLIENT_INSTANCE_ID}"
fi
if [[ -n "$IDS_TEXT" ]]; then
  # shellcheck disable=SC2086
  awsx ec2 terminate-instances --instance-ids $IDS_TEXT >/dev/null || true
  # shellcheck disable=SC2086
  awsx ec2 wait instance-terminated --instance-ids $IDS_TEXT || true
fi

if [[ -n "$EIP_ASSOC_ID" ]]; then
  echo "Disassociating EIP"
  awsx ec2 disassociate-address --association-id "$EIP_ASSOC_ID" || true
fi
if [[ -n "$EIP_ALLOC_ID" ]]; then
  echo "Releasing EIP"
  awsx ec2 release-address --allocation-id "$EIP_ALLOC_ID" || true
else
  EIPS_TEXT="$(awsx ec2 describe-addresses \
    --filters "Name=tag:SpikeId,Values=${SPIKE_ID}" \
    --query 'Addresses[].AllocationId' \
    --output text | tr '\t' '\n' | sed '/^$/d')"
  for alloc in $EIPS_TEXT; do
    awsx ec2 release-address --allocation-id "$alloc" || true
  done
fi

echo "Removing IAM resources"
if [[ -n "$ROLE_NAME" ]]; then
  awsx iam detach-role-policy \
    --role-name "$ROLE_NAME" \
    --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore || true
fi
if [[ -n "$PROFILE_NAME" && -n "$ROLE_NAME" ]]; then
  awsx iam remove-role-from-instance-profile \
    --instance-profile-name "$PROFILE_NAME" \
    --role-name "$ROLE_NAME" || true
fi
if [[ -n "$PROFILE_NAME" ]]; then
  awsx iam delete-instance-profile --instance-profile-name "$PROFILE_NAME" || true
fi
if [[ -n "$ROLE_NAME" ]]; then
  awsx iam delete-role --role-name "$ROLE_NAME" || true
fi

echo "Deleting route table associations"
if [[ -n "$PUBLIC_RTA_ID" ]]; then
  awsx ec2 disassociate-route-table --association-id "$PUBLIC_RTA_ID" || true
fi
if [[ -n "$PRIVATE_RTA_ID" ]]; then
  awsx ec2 disassociate-route-table --association-id "$PRIVATE_RTA_ID" || true
fi

echo "Deleting route tables"
if [[ -n "$PRIVATE_RT_ID" ]]; then
  awsx ec2 delete-route-table --route-table-id "$PRIVATE_RT_ID" || true
fi
if [[ -n "$PUBLIC_RT_ID" ]]; then
  awsx ec2 delete-route-table --route-table-id "$PUBLIC_RT_ID" || true
fi

echo "Deleting security groups"
if [[ -n "$CLIENT_SG_ID" ]]; then
  awsx ec2 delete-security-group --group-id "$CLIENT_SG_ID" || true
fi
if [[ -n "$LOXILB_SG_ID" ]]; then
  awsx ec2 delete-security-group --group-id "$LOXILB_SG_ID" || true
fi

echo "Deleting subnets"
if [[ -n "$PRIVATE_SUBNET_ID" ]]; then
  awsx ec2 delete-subnet --subnet-id "$PRIVATE_SUBNET_ID" || true
fi
if [[ -n "$PUBLIC_SUBNET_ID" ]]; then
  awsx ec2 delete-subnet --subnet-id "$PUBLIC_SUBNET_ID" || true
fi

echo "Deleting internet gateway and VPC"
if [[ -n "$IGW_ID" && -n "$VPC_ID" ]]; then
  awsx ec2 detach-internet-gateway --internet-gateway-id "$IGW_ID" --vpc-id "$VPC_ID" || true
fi
if [[ -n "$IGW_ID" ]]; then
  awsx ec2 delete-internet-gateway --internet-gateway-id "$IGW_ID" || true
fi
if [[ -n "$VPC_ID" ]]; then
  awsx ec2 delete-vpc --vpc-id "$VPC_ID" || true
fi

echo "Cleanup verification"
awsx ec2 describe-instances \
  --filters "Name=tag:SpikeId,Values=${SPIKE_ID}" \
  --query 'Reservations[].Instances[].{InstanceId:InstanceId,State:State.Name}' \
  --output table || true
awsx ec2 describe-vpcs \
  --filters "Name=tag:SpikeId,Values=${SPIKE_ID}" \
  --query 'Vpcs[].VpcId' \
  --output table || true
awsx ec2 describe-addresses \
  --filters "Name=tag:SpikeId,Values=${SPIKE_ID}" \
  --query 'Addresses[].AllocationId' \
  --output table || true

echo "Cleanup script finished."

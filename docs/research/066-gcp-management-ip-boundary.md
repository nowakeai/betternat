# GCP Management IP Boundary

Date: 2026-06-26

## Summary

GCP cannot directly copy the AWS gateway public-IP model in the current
single-NIC BetterNAT design.

AWS can keep an appliance instance's ordinary public IP separate from an
Elastic IP used as the stable egress identity. GCE instance external IPv4
addresses are represented as `accessConfig` entries on a network interface.
For the current BetterNAT GCP gateway, both the ordinary gateway external IP and
the stable egress identity would occupy the same `nic0` external IPv4 slot.
Moving the stable egress address therefore replaces or removes the gateway's
ordinary management public IP.

## Decision

Do not promise AWS-like "gateway public IP plus separate stable egress public
IP" semantics for GCP single-NIC gateways.

The supported design choices are:

- Route-only GCP mode: each gateway keeps its own ephemeral public IP, and
  failover changes the observed egress source IP.
- Single-NIC stable egress mode: the active gateway owns the stable external
  IP, but standby gateways should not rely on persistent management public IPs.
- Future multi-NIC management mode: add a separate management interface in a
  separate VPC with its own external IP, while keeping the dataplane interface
  in the workload VPC. This needs explicit VPC design, firewalling, and guest
  OS route-policy configuration before it is a product feature.

## Implementation Implication

The existing GCP gateway creation path already gives gateway instances an
ephemeral external IP by default. That behavior is useful for route-only
bootstrap, artifact download, and disposable diagnostics.

When `stable_public_identity_address_name` is enabled, the GCP runtime must
treat the configured static address as the egress identity and must not assume
that a second persistent management public IP can remain on the same gateway
NIC. Test harnesses that need SSH to standby gateways must use one of these
paths:

- IAP, when the operator account has permission.
- A temporary external access config during testing, removed immediately after
  diagnostics.
- A future multi-NIC management design, after it is explicitly implemented and
  validated.

## References

- GCE static external IP limitation: each network interface can be assigned
  only one external IPv4 address:
  <https://docs.cloud.google.com/compute/docs/ip-addresses/configure-static-external-ip-address>
- GCE multiple network interfaces: each interface is attached to a separate VPC
  network, and guest OS route policies/local route tables may be required:
  <https://docs.cloud.google.com/vpc/docs/multiple-interfaces-concepts>

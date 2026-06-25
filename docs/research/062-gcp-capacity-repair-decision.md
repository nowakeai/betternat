# GCP Capacity Repair Decision

Date: 2026-06-25

## Decision

GCP alpha may continue to use provider-owned unmanaged GCE gateway instances.
GCP GA should use Managed Instance Groups for capacity repair unless a later
architecture decision proves a smaller repair loop is safer.

The current `internal/install/gcp` applier creates fixed gateway VMs directly
and deletes those VMs on cleanup. That is acceptable for disposable alpha
validation, but it is not a production capacity-repair contract.

## Rationale

BetterNAT HA has two separate loops:

- fast failover: the standby agent acquires the Firestore lease and moves the
  tagged static route,
- slow capacity repair: the cloud platform replaces failed gateway capacity so
  the HA group returns to a warm active/standby shape.

The current GCP implementation has the fast route-only failover loop, but it
does not repair a failed gateway VM after the failover. That means a second
failure can leave the deployment without a warm candidate.

Google Compute Engine Managed Instance Groups provide automated instance
management, autohealing, controlled updates, and multi-zone deployment options.
The official stateful MIG documentation also describes preserving per-instance
state such as instance names, IP addresses, metadata, and disks across restart,
recreation, autohealing, and update events. BetterNAT should evaluate whether
stateless or stateful MIGs are the better fit, but unmanaged VMs should not be
the default GA model.

## Current Product Contract

For GCP alpha:

- unmanaged gateway VMs are allowed,
- `gateway_count >= 2` is required for HA evidence,
- replacement after VM loss is not automatic,
- capacity repair must be documented as an alpha limitation,
- residual scans must still prove unmanaged VMs are cleaned up.

For GCP GA:

- provider/module should create instance templates and MIGs instead of fixed
  unmanaged gateway VMs,
- replacement nodes must boot with the same BetterNAT config and register as
  standby without stealing ownership,
- active owner termination must fail over through the existing lease-fenced
  route path before or while the MIG replaces capacity,
- non-owner replacement must not change the current route target,
- cleanup must remove MIGs, templates, health checks, routes, IAM, Firestore
  records, and any reserved addresses.

## Required Validation Before GA

- Terminate a non-owner gateway and prove the MIG replaces it while the active
  route target remains unchanged.
- Terminate the active gateway and prove standby takeover happens before
  capacity repair is treated as healthy.
- Prove the replacement node joins the registry as `STANDBY`.
- Prove route target and Firestore lease generation do not regress when the old
  active name or replacement name differs from the previous VM.
- Test zonal and regional MIG behavior if multi-zone GCP GA is in scope.

## References

- Google Cloud, "Instance groups":
  <https://docs.cloud.google.com/compute/docs/instance-groups>
- Google Cloud, "Stateful managed instance groups":
  <https://docs.cloud.google.com/compute/docs/instance-groups/stateful-migs>
- Google Cloud, "Set up an application-based health check and autohealing":
  <https://docs.cloud.google.com/compute/docs/instance-groups/autohealing-instances-in-migs>

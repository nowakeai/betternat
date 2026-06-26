# GCP Protocol Failover Results

Date: 2026-06-25

## Summary

Disposable GCP protocol failover validation passed in
`smooth-calling-490406-d9` for run `bnat-gcp-proto-20260625114222`.

The run used a private client VM with no public IP, two BetterNAT GCE gateway
VMs, Firestore coordination in the `(default)` database, LoxiLB as the only
datapath, and a provider-owned runtime service account plus per-gateway custom
IAM role.

This test validates route-only GCP alpha behavior for new flows across a
proactive handover. It does not prove connection preservation and it does not
provide stable shared public identity. The observed source IP changed from the
old active gateway public IP to the new active gateway public IP.

## Environment

- Project: `smooth-calling-490406-d9`
- Region: `us-west2`
- Zone: `us-west2-a`
- Run ID: `bnat-gcp-proto-20260625114222`
- Private CIDR: `10.98.0.0/24`
- Firestore database: `(default)`
- Gateway count: `2`
- Client: `bnat-gcp-proto-20260625114222-client`
- Initial active route target: `bnat-gcp-proto-20260625114222-gw-b`
- Handover target: `bnat-gcp-proto-20260625114222-gw-a`
- Initial public egress IP: `34.20.164.68`
- Post-handover public egress IP: `34.94.153.80`

The client had no external IP. Operator SSH to the private client used the
active gateway as a temporary SSH proxy. The actual protocol probes ran on the
private client and egressed through the BetterNAT route.

## Validation

Command:

```sh
scripts/gcp-protocol-failover-smoke.sh \
  --project smooth-calling-490406-d9 \
  --zone us-west2-a \
  --name bnat-gcp-proto-20260625114222 \
  --ssh-mode external \
  --client-access proxy-gateway \
  --samples 80 \
  --interval 0.5 \
  --output-dir tmp/bnat-gcp-proto-20260625114222/protocol-smoke-3
```

The script verified these protocol checks before and after handover:

- TCP public-IP probe through `https://checkip.amazonaws.com`,
- HTTPS status probe through `https://example.com`,
- UDP DNS query to `8.8.8.8:53`,
- 1 MiB HTTPS download from `https://speed.cloudflare.com`.

Baseline private-client result:

```text
tcp_checkip=34.20.164.68
tcp_https_status=200
udp_dns=ok answers=2 responder=8.8.8.8
download_bytes=1048576
```

Handover result:

```text
handover completed: bnat-gcp-proto-20260625114222-gw-b -> bnat-gcp-proto-20260625114222-gw-a generation=2 handover completed
```

Post-handover private-client result:

```text
tcp_checkip=34.94.153.80
tcp_https_status=200
udp_dns=ok answers=2 responder=8.8.8.8
download_bytes=1048576
```

Continuous TCP public-IP probe summary:

```text
samples=80
ok=74
failed=6
first_ip=34.20.164.68
last_ip=34.94.153.80
ip_switches=1
longest_consecutive_failures=6
```

After handover, live doctor on `gw-a` reported datapath, lease, route,
Prometheus, and source-IP probe checks as `ok`. It also reported the expected
route-only public identity status:

```text
public_identity: GCP route-only HA has no shared public identity configured
source_ip_probe: observed source IP 34.94.153.80
```

## Findings

- New TCP/HTTPS/UDP DNS/download flows work before and after proactive
  route-only handover.
- Route-only GCP failover caused a public source-IP switch from `gw-b` to
  `gw-a`; this is expected until a stable public identity design is validated.
- The 80-sample new-flow probe observed 6 failed samples in one consecutive
  run during failover. GCP route-only alpha should document this as a new-flow
  recovery window, not connection preservation.
- During initial bootstrap, the active owner briefly reported degraded route
  mutation errors while GCP route delete/insert operations were still settling:
  `resourceNotReady` and operation wait deadline exceeded. The system later
  converged to a single active owner with route-target match.
- Standby LoxiLB warm-up briefly emitted non-JSON `loxicmd` output while the
  datapath was starting. The standby recovered to healthy state before the
  protocol smoke.

## Cleanup

Terraform destroy removed the provider-owned gateway, client, route, network,
runtime service account, IAM binding, and runtime custom role. The temporary
operator SSH firewall rule and public artifact bucket were deleted manually.

Because this run used the shared `(default)` Firestore database,
run-scoped handover records were deleted explicitly. Final residual scan
passed:

```text
instances: 0
routes: 0
firewall-rules: 0
addresses: 0
service-accounts: 0
firestore records: 0
GCP residual scan passed
```

## Gate Impact

This closes the GCP protocol/new-flow handover validation item for route-only
alpha. It does not close the remaining GCP GA gaps:

- stable shared public identity,
- multi-zone behavior,
- GKE/private-node topology,
- explicit split-brain and route-operation failure injection,
- production migration from Cloud NAT.

Raw LoxiLB-on-GCE HA baseline comparison is explicitly not a GCP GA gate. The
release bar is direct validation of BetterNAT-owned HA behavior: lease-fenced
route and public-identity ownership, rollback, status, cleanup, and install UX.

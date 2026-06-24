# Alpha Low-Cost Soak Results

Date: 2026-06-24

Region: `us-west-2`

Environment:

- Gateway: `bnat-lifecycle-20260623023753`
- HA group: `bnat-lifecycle-20260623023753-us-west-2a`
- ASG: `betternat-bnat-lifecycle-20260623023753-us-west-2a`
- Launch template: `lt-0e610263c6ef023f7` version `16`
- Mode: stable shared EIP, no per-node public IP on the standby
- Shared EIP: `52.24.117.43`
- Private client: `i-0ec999731bb6cb25b`
- Gateway nodes:
  - `i-048fd34e26867122f`, private IP `10.88.1.135`
  - `i-073ab0073edde40ba`, private IP `10.88.1.85`
- BetterNAT version observed through daemon status: `v0.1.0-alpha.2`

This was a low-cost alpha soak smoke, not a production long soak or SLO proof.

## Soak Probe With Controlled Events

Client-side probe command:

```bash
BETTERNAT_PROBE_SAMPLES=2400 \
BETTERNAT_PROBE_INTERVAL_SECONDS=0.25 \
BETTERNAT_EXPECTED_IP=52.24.117.43 \
BETTERNAT_PROBE_OUTPUT=/tmp/lowcost-soak-20260624T030037Z.tsv \
  /tmp/betternat-egress-probe-monitor.sh
```

Actual probe wall time was about 18 minutes because each public HTTP probe took
roughly 0.45 seconds including curl/runtime overhead.

Events during the probe:

1. Restarted standby `betternat-agent` on `i-073ab0073edde40ba`.
2. Ran manual proactive handover from `i-048fd34e26867122f` to
   `i-073ab0073edde40ba`.
3. Restarted LoxiLB on the active node `i-073ab0073edde40ba`.

Results:

- samples: `2400`
- ok: `2396`
- failed: `4`
- unexpected public IP samples: `0`
- longest consecutive failure run: `1`
- first public IP: `52.24.117.43`
- last public IP: `52.24.117.43`
- public IP switches: `0`

Failure rows:

```text
295   2026-06-24T03:02:50.946Z  fail  rc=28  curl: (28) Connection timed out after 1001 milliseconds
298   2026-06-24T03:02:53.910Z  fail  rc=28  curl: (28) Operation timed out after 2001 milliseconds with 0 bytes received
450   2026-06-24T03:04:04.922Z  fail  rc=28  curl: (28) Operation timed out after 2002 milliseconds with 0 bytes received
1675  2026-06-24T03:13:17.347Z  fail  rc=28  curl: (28) Connection timed out after 1000 milliseconds
```

Interpretation:

- The first two failures align with the manual proactive handover that completed
  at `2026-06-24T03:02:50Z`.
- The third failure aligns with the LoxiLB restart / automatic handover that
  completed at `2026-06-24T03:04:05Z`.
- The fourth failure was isolated and not adjacent to an operator-triggered
  event.
- No probe observed a non-shared public IP.

Post-test daemon status from both gateway nodes agreed:

- active: `i-048fd34e26867122f`
- standby: `i-073ab0073edde40ba`
- lease generation: `19`
- route target match: true on the active daemon
- public IP match: true on the active daemon
- both registry records fresh and healthy

Cloud-side verification after the first probe:

- route `0.0.0.0/0` in `rtb-0d856d0e37e48523e` pointed to
  `i-048fd34e26867122f`,
- shared EIP `52.24.117.43` was associated to `i-048fd34e26867122f`.

## Standalone Active Agent Systemd Stop

To directly validate standalone active `betternat-agent` stop/restart behavior,
a second short client probe ran while restarting the active daemon.

Client-side probe command:

```bash
BETTERNAT_PROBE_SAMPLES=360 \
BETTERNAT_PROBE_INTERVAL_SECONDS=0.25 \
BETTERNAT_EXPECTED_IP=52.24.117.43 \
BETTERNAT_PROBE_OUTPUT=/tmp/systemd-stop-active-20260624T032032Z.tsv \
  /tmp/betternat-egress-probe-monitor.sh
```

Event:

```bash
sudo systemctl restart betternat-agent
```

The active daemon on `i-048fd34e26867122f` recorded:

```text
systemd-stop-1782271270264168584 completed i-048fd34e26867122f -> i-073ab0073edde40ba generation 20 updated 2026-06-24T03:21:12Z
```

Probe results:

- samples: `360`
- ok: `359`
- failed: `1`
- unexpected public IP samples: `0`
- longest consecutive failure run: `1`
- first public IP: `52.24.117.43`
- last public IP: `52.24.117.43`
- public IP switches: `0`

Failure row:

```text
87  2026-06-24T03:21:12.548Z  fail  rc=28  curl: (28) Connection timed out after 1001 milliseconds
```

Post-test daemon status from both gateway nodes agreed:

- active: `i-073ab0073edde40ba`
- standby: `i-048fd34e26867122f`
- lease generation: `20`
- route target match: true on the active daemon
- public IP match: true on the active daemon
- both registry records fresh and healthy

Cloud-side verification after the second probe:

- route `0.0.0.0/0` in `rtb-0d856d0e37e48523e` pointed to
  `i-073ab0073edde40ba`,
- shared EIP `52.24.117.43` was associated to `i-073ab0073edde40ba`.

## Non-Stable Route-Only Handover Comparison

A separate 2026-06-24 validation temporarily switched the retained environment
to `stable_egress_ip=false`, with per-node public IPv4 enabled and no
`ha.public_identity` in the agent config. The environment was restored to
stable/no-public-IP mode after the validation.

The manual proactive handover
`i-0a89f292e07b04460 -> i-0d08059b2f4708db6` completed at lease generation
`15`.

Client probe result during that route-only handover:

- samples: `240`
- failed: `0`
- public source IP changed from `52.24.117.43` to `52.24.240.255`
- last old-IP sample: `2026-06-24T02:06:34.767Z`
- first new-IP sample: `2026-06-24T02:06:35.202Z`
- visible switch window: about `435 ms` at client probe sampling granularity

Conclusion: non-stable route-only handover was materially faster than stable
shared-EIP handover in this AWS probe because it avoids EIP reassociation and
public-identity verification. The tradeoff is explicit: the public source IP
changes after handover, so this mode is unsuitable for destinations that require
a fixed allowlisted egress IP.

## Additional Observation

The standby agent restart created a rejected `systemd-stop-*` operation:

```text
systemd-stop-1782270089079184305 rejected ... prepare handover target "i-073ab0073edde40ba": dial tcp 10.88.1.85:9109: connect: connection refused
```

This did not break egress and the later manual handover completed, but it is a
record hygiene issue. Follow-up code now filters and best-effort deletes expired
handover records, and `betternat handover history` hides stale non-terminal
records from older lease generations by default. Use `--include-stale` when the
raw intermediate records are needed for support evidence.

## Conclusion

This pass is sufficient alpha evidence for:

- low-cost soak smoke with periodic private-client egress probes,
- stable shared-EIP preservation during controlled events,
- standalone active systemd-stop graceful handover,
- live LoxiLB restart recovery in AWS.
- non-stable route-only handover being faster than stable EIP handover in the
  measured AWS alpha environment, with the expected source-IP change.

It is not sufficient for production long-soak, throughput, or SLO claims.

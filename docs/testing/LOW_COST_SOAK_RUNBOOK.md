# Low-Cost Soak Runbook

This runbook validates that a disposable BetterNAT alpha environment can keep
egress working through a modest amount of time and controlled gateway restarts.
It is intentionally low cost: it uses small instances, periodic HTTP probes,
and short daemon/datapath restart events instead of sustained throughput tests.

This runbook does not create or destroy cloud resources. Use an existing
disposable BetterNAT test environment, and keep the environment owner
responsible for cleanup.

## Scope

Use this runbook to collect alpha evidence for:

- private client egress continuity,
- public source IP behavior in stable and non-stable modes,
- `betternat-agent` restart behavior,
- LoxiLB restart reconciliation behavior,
- handover history and daemon status visibility after controlled events.

Do not use this runbook as a production benchmark, throughput test, SLA proof,
or long-duration reliability claim.

## Inputs

Record these before starting:

- gateway name,
- AWS region and availability zone,
- ASG name,
- launch template version,
- BetterNAT version,
- LoxiLB image tag and digest when available,
- whether `stable_egress_ip` is enabled,
- expected public source IP, if stable EIP mode is enabled,
- private client instance ID,
- active gateway node ID,
- standby gateway node IDs.

## Client Probe

Copy the repository script to the private client or run it from a checked-out
copy of this repository on the client:

```bash
BETTERNAT_PROBE_SAMPLES=7200 \
BETTERNAT_PROBE_INTERVAL_SECONDS=0.25 \
BETTERNAT_EXPECTED_IP=<stable-eip-or-empty> \
BETTERNAT_PROBE_OUTPUT=/tmp/betternat-egress-probe.tsv \
  scripts/egress-probe-monitor.sh
```

At the default `0.25` second interval, `7200` samples is about 30 minutes.
For non-stable mode, leave `BETTERNAT_EXPECTED_IP` unset and compare the
observed IP switches with the handover or restart timeline.

The sample output is tab-separated:

```text
timestamp	status	observed_ip	rc	error
```

The script prints a summary to stderr with total samples, failures, unexpected
IP samples, longest consecutive failure run, first/last IP, and IP switch count.

## Gateway Baseline

On a gateway node, collect:

```bash
sudo betternat status
sudo betternat datapath status
sudo betternat handover current
sudo betternat handover history --limit 20
sudo betternat support bundle
```

If the active node is known, also record the cloud-side route and public identity
state using the existing AWS supplemental runbook commands.

## Controlled Events

Run one event at a time. Wait for the probe and CLI status to settle before
starting the next event.

1. Restart the standby agent:

   ```bash
   sudo systemctl restart betternat-agent
   sudo betternat status
   ```

2. Restart the active agent only when proactive handover or graceful lease
   release behavior is intentionally being exercised:

   ```bash
   sudo systemctl restart betternat-agent
   sudo betternat status
   sudo betternat handover history --limit 20
   ```

3. Restart LoxiLB on the active node to validate datapath reconciliation:

   ```bash
   sudo systemctl restart loxilb || sudo docker restart loxilb
   sudo betternat datapath status
   sudo betternat status
   ```

4. Run a manual proactive handover:

   ```bash
   sudo betternat handover start --to auto --reason low-cost-soak
   sudo betternat status
   sudo betternat handover history --limit 20
   ```

## Success Criteria

For an alpha low-cost soak pass:

- the probe has no sustained egress outage,
- active owner, route target, and daemon status agree after each event,
- LoxiLB datapath status returns ready after restart,
- handover records reach a terminal state for manual handover attempts,
- stable mode has `0` unexpected public IP samples,
- non-stable mode only changes public IP at expected route-owner changes,
- support bundles can be collected after the test.

For the current alpha, short transient probe failures during handover are
acceptable if they are recorded with timestamps, bounded, and explained by the
event timeline. Do not convert this runbook result into a public SLO.

## Evidence To Save

Save these artifacts under `tmp/` during execution and summarize durable
results in `docs/release/RELEASE_CHECKLIST.md` or a research result document:

- probe TSV output,
- probe summary,
- `betternat status` before and after each event,
- `betternat handover history --limit 20`,
- support bundle file names,
- AWS route/EIP owner snapshots when relevant,
- exact event timestamps.

## Cleanup

Stop the probe after the run. This runbook intentionally leaves the BetterNAT
environment lifecycle to the test owner. If the environment should be destroyed,
follow the Terraform destroy and residual resource scan in the AWS supplemental
runbook.

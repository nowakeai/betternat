# Failover Stability Patterns In Network Infrastructure

Date: 2026-06-21

## Question

If BetterNAT should prioritize stability and avoid outages caused by its own keepalive mechanism, how do other network and distributed infrastructure systems design failover?

## Short Answer

Do not make BetterNAT's production default a pure "short TTL expires, immediately mutate AWS route/EIP" design.

The more stable pattern is:

1. renew frequently while healthy,
2. require a longer loss window before another node takes over,
3. use multiple signals when available,
4. make takeover conditional on local datapath readiness,
5. avoid automatic preemption after the old owner returns,
6. separate fast explicit failure from slow ambiguous failure.

For BetterNAT, that means the default production profile should be conservative:

```text
ha_profile = "default"
lease_ttl = 10s
renew_interval = 1s
takeover_after = lease expiry, plus jitter/backoff
preempt = false
```

BetterNAT originally considered separate `stable`, `balanced`, and `fast` timing profiles. For the alpha surface, the product now keeps one `default` profile with `ttl=10s, renew=1s`; legacy profile names are aliases.

## Industry Patterns

### 1. Kubernetes Leader Election

Kubernetes controllers use lease-based leader election with separate concepts for:

- lease duration,
- renew deadline,
- retry period.

The usual shape is not "renew once, miss once, fail over." The leader retries renewals within a deadline, and other candidates wait for the lease duration before taking over.

Typical defaults in Kubernetes control-plane flags are:

```text
lease duration: 15s
renew deadline: 10s
retry period: 2s
```

Design lesson for BetterNAT:

- separate `renew_interval` from `takeover_after`,
- renew more frequently than the takeover timeout,
- do not let one missed DynamoDB/API call trigger route mutation,
- use leader-election-style fencing, not peer-to-peer heartbeats alone.

Useful source:

- Kubernetes `kube-controller-manager` options and leader-election configuration: https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/
- Kubernetes client-go leader election package: https://pkg.go.dev/k8s.io/client-go/tools/leaderelection

### 2. Keepalived / VRRP

Keepalived/VRRP is optimized for L2/L3 local failover. It sends periodic advertisements. Backup routers declare the master down only after a derived master-down interval, usually multiple advertisement intervals plus skew based on priority.

Common behavior:

- advert every 1 second by default in many deployments,
- backup waits for several missed adverts,
- priority and preemption behavior matter.

Design lesson for BetterNAT:

- VRRP can fail over very quickly on a LAN, but AWS route/EIP failover is not a LAN multicast problem,
- copying VRRP's 3-4 second failover behavior into AWS+DynamoDB would be unsafe,
- the useful part is "several missed signals before failover" and explicit preemption control.

Useful source:

- Keepalived man page / configuration reference: https://www.keepalived.org/manpage.html

### 3. AWS Load Balancer Target Health

AWS load balancers use health-check intervals and consecutive success/failure thresholds.

For Network Load Balancer target groups, health checks have configurable interval and unhealthy threshold values. The default posture is intentionally not sub-second. A target must fail more than one health check before it is treated as unhealthy.

Design lesson for BetterNAT:

- production network infrastructure usually requires consecutive failures,
- defaults trade recovery speed for reduced flapping,
- BetterNAT should treat "ambiguous silence" differently from explicit instance termination.

Useful source:

- AWS Network Load Balancer target group health checks: https://docs.aws.amazon.com/elasticloadbalancing/latest/network/target-group-health-checks.html

### 4. HAProxy Active Health Checks

HAProxy health checks use repeated checks with `fall` and `rise` counters:

- `fall` controls how many consecutive failures mark a server down,
- `rise` controls how many consecutive successes mark it back up.

This prevents one bad sample from changing routing state.

Design lesson for BetterNAT:

- use hysteresis,
- require a candidate to prove readiness before it can become active,
- require a returning node to prove stability before it can become standby,
- do not preempt the current active just because a previously failed node is back.

Useful source:

- HAProxy configuration manual: https://docs.haproxy.org/3.0/configuration.html

### 5. BFD

BFD is designed for fast network failure detection. It can detect path failure quickly by using negotiated transmit intervals and a detection multiplier.

Even in this fast-failover protocol, detection is not "one packet lost." The receiver considers the session down after no control packets are received for the detection time, which is based on interval multiplied by multiplier.

Design lesson for BetterNAT:

- fast failover protocols still use multipliers,
- if BetterNAT later adds peer heartbeats, it should still require N missed beats,
- BFD-style timing is useful for appliance-to-appliance liveness, but cloud route mutation still needs lease fencing.

Useful source:

- RFC 5880, Bidirectional Forwarding Detection: https://www.rfc-editor.org/rfc/rfc5880

### 6. etcd / Raft

Raft-based systems use a heartbeat interval and an election timeout. The election timeout is intentionally larger than heartbeat interval and should account for network latency and variance.

etcd documentation recommends tuning heartbeat and election timeout according to round-trip time and warns that election timeout must be long enough for network variance.

Design lesson for BetterNAT:

- if the control-plane dependency is DynamoDB + EC2 API, timeouts must account for cloud API latency variance, not only local process liveness,
- too-short election windows create false elections,
- false elections are especially bad for BetterNAT because they mutate shared cloud resources.

Useful source:

- etcd tuning guide: https://etcd.io/docs/v3.5/tuning/

## BetterNAT-Specific Implications

### 1. Use Explicit-Failure Fast Path

Not all failures are equal.

Fast takeover is reasonable when the signal is explicit and authoritative:

- ASG/EC2 says owner is `shutting-down`, `terminated`, or `stopped`,
- IMDS or systemd on the owner performs a graceful release before shutdown,
- active agent voluntarily steps down during planned maintenance.

For these cases, BetterNAT can move faster than the generic lease TTL.

### 2. Use Slow Path For Ambiguous Silence

Ambiguous silence includes:

- missed DynamoDB renew due to SDK hang,
- temporary network path issue,
- brief CPU stall,
- DynamoDB throttling,
- transient AWS SDK retry delay,
- short LoxiLB/container restart.

For these cases, failover should be conservative. The current `ttl=10s` is useful for tests, but aggressive for production.

### 3. Avoid Preemption By Default

Once a standby becomes active, the old owner or a new ASG replacement should not automatically take ownership back just because it has a higher priority or earlier identity.

Default:

```text
preempt = false
```

This reduces route/EIP churn and matches the product's stability-first goal.

### 4. Add Hysteresis

BetterNAT should have separate thresholds:

```text
owner_suspect_after = 2 missed renew windows
owner_dead_after = lease expiry
candidate_ready_after = local datapath ready for N consecutive checks
returning_node_standby_after = service healthy for N checks
```

This mirrors HAProxy `fall/rise` and AWS health-check thresholds.

### 5. Separate Lease TTL From Local Step Timeout

The bug found during AWS testing was a hung SDK call that kept the process alive while HA state went stale. The fix added step timeouts.

The stable design should keep:

- short bounded operation timeouts,
- frequent renew attempts,
- longer takeover TTL.

Example:

```text
aws_operation_timeout = 2s to 5s
renew_interval = 5s
lease_ttl = 30s
```

This lets the active retry renewals multiple times before another node takes over, while still preventing a single blocked call from freezing the HA loop.

## Recommended BetterNAT Profile

### `default`

Use this as the only public alpha profile.

```text
lease_ttl = 10s
renew_interval = 1s
operation_timeout = 2s
takeover_jitter = 0s to 1s
preempt = false
candidate_readiness_successes = 1
```

Expected behavior:

- current tested behavior, about 12 seconds client-visible outage in owner-termination runs,
- graceful release can be detected by standby in about one polling interval,
- higher risk of false failover during transient AWS/DynamoDB stalls than a slower profile would have.

## Product Recommendation

Keep one public profile for alpha and call it `default`.

Expose the UX as:

```hcl
ha_profile = "default"
```

Avoid exposing raw TTL first. Advanced users can override low-level timing later:

```hcl
ha_timing = {
  lease_ttl        = "30s"
  renew_interval   = "5s"
  operation_timeout = "3s"
}
```

## Next Engineering Work

1. Keep `default` in AWS supplemental tests to control cost and runtime.
2. Add a longer timing soak later if false-failover evidence appears:
   - apply,
   - verify baseline,
   - terminate owner,
   - confirm no false preemption by replacement,
   - destroy.
3. Add explicit-failure fast path research:
   - can standby check EC2 owner state before waiting full TTL,
   - can active release lease on shutdown through systemd `ExecStop`,
   - can ASG lifecycle hooks reduce ambiguous failure windows.

Implemented after this research:

- Terraform provider exposes `ha_profile`,
- Terraform provider exposes advanced overrides `ha_lease_ttl_seconds` and `ha_renew_interval_seconds`,
- production examples use `ha_profile = "default"`,
- the AWS supplemental fixture defaults to `ha_profile = "default"` to keep disposable test runtime low.

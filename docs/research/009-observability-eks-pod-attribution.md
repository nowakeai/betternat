# Observability: EKS Pod Attribution

Date: 2026-06-19

## Question

If a VPC contains both VMs and EKS clusters, can BetterNAT identify which Kubernetes pod originated an outbound connection?

## Short Answer

Sometimes, but not universally.

BetterNAT can attribute traffic to a pod only if the packet arriving at the NAT appliance still has the pod IP as its source, or if the appliance can enrich node-level traffic with Kubernetes metadata from another signal.

For EKS with the Amazon VPC CNI:

- Pods are assigned VPC private IPs.
- By default, outbound IPv4 traffic to destinations outside the VPC can be SNATed on the worker node before it reaches the NAT Gateway/appliance.
- If node-level SNAT happens first, BetterNAT sees the node IP, not the pod IP.
- If external SNAT is enabled so pod source IPs are preserved until the external NAT device, BetterNAT can map source IP -> pod metadata.

Therefore, pod attribution should be a feature with clearly documented prerequisites.

## Traffic Identity Layers

There are three levels of attribution:

```text
Level 1: VM / node IP
  source_ip = 10.0.10.25

Level 2: Kubernetes pod IP
  source_ip = 10.0.42.113
  pod = namespace/name

Level 3: Kubernetes workload identity
  deployment/statefulset/daemonset
  namespace
  labels
  service account
  team/cost center
```

BetterNAT can always try to report Level 1.

Level 2 and Level 3 need Kubernetes integration and correct CNI/SNAT behavior.

## EKS With Amazon VPC CNI

Amazon EKS commonly uses the Amazon VPC CNI. AWS docs say the VPC CNI assigns private IPv4/IPv6 addresses from the VPC to pods.

This is good for BetterNAT because pod IPs are real VPC IPs. In principle, a NAT appliance can see:

```text
src = pod IP
dst = internet IP
```

and then enrich `src` through the Kubernetes API:

```text
10.0.42.113 -> pod checkout/api-7d9f...
```

However, the default outbound behavior matters.

AWS EKS external SNAT docs describe the external SNAT setting. When external SNAT is not enabled, pod traffic to external destinations may be SNATed by the worker node. In that case, the NAT appliance sees:

```text
src = node primary private IP
dst = internet IP
```

not:

```text
src = pod IP
dst = internet IP
```

## Mode A: Default EKS CNI, Node SNAT Before NAT Appliance

Flow:

```text
pod IP
  -> node SNAT to node private IP
  -> BetterNAT appliance
  -> EIP/public internet
```

What BetterNAT sees:

```text
source = EKS node private IP
```

Attribution quality:

- Can identify EKS node.
- Can identify node group/ASG if AWS metadata/tags are available.
- Cannot reliably identify exact pod from NAT appliance traffic alone.

Possible enrichment:

- Kubernetes node name from EC2 private IP.
- Workload guesses from per-node scheduling data.
- VPC Flow Logs and Kubernetes events correlation.

But this is not exact per-pod attribution.

Product wording:

> In default EKS node-SNAT mode, BetterNAT attributes egress to the worker node. Pod-level attribution requires preserving pod source IPs or installing Kubernetes-side telemetry.

## Mode B: EKS External SNAT Enabled, Pod IP Preserved To NAT Appliance

Flow:

```text
pod IP
  -> BetterNAT appliance
  -> SNAT to EIP/public internet
```

What BetterNAT sees:

```text
source = pod private IP
```

Attribution quality:

- Can map source IP to pod.
- Can enrich metrics with namespace, pod, workload, labels, service account, team.
- Best fit for the product.

Required integration:

- Kubernetes API watcher.
- Pod IP -> pod metadata cache.
- OwnerReferences traversal to Deployment/StatefulSet/DaemonSet/Job.
- Optional namespace/label/team mapping.

Suggested metric labels:

```text
source_type="pod"
cluster="prod-eks"
namespace="checkout"
workload_kind="Deployment"
workload="api"
pod="api-7d9f..."
node="ip-10-0-10-25"
team="payments"
```

Important warning:

High-cardinality labels like exact pod name and destination IP can overload Prometheus. The product should aggregate for dashboards and expose detailed top-N through the CLI or a flow store.

## Kubernetes Metadata Mapping

Kubernetes Pod status includes `podIP` and `podIPs`. The agent can watch pods and maintain an in-memory cache:

```text
pod_ip -> {
  cluster,
  namespace,
  pod,
  node,
  labels,
  owner_kind,
  owner_name,
  service_account
}
```

Data collection options:

### Option 1: NAT appliance talks to Kubernetes API

Pros:

- Single BetterNAT agent can enrich flows.
- No DaemonSet required for metadata.

Cons:

- NAT appliance needs kubeconfig/IAM auth.
- Multi-cluster VPC means multiple API credentials.
- Security review required.

### Option 2: Kubernetes-side metadata exporter

Run a small DaemonSet or Deployment in each cluster:

```text
cluster exporter watches pods
exports pod_ip -> metadata to BetterNAT
```

Pros:

- Better Kubernetes-native RBAC.
- Works across multiple clusters.
- NAT appliance does not need broad Kubernetes API access.

Cons:

- Additional component to install.
- Need secure channel from cluster to NAT appliance or metrics backend.

Recommended product approach:

- MVP: support static CIDR/subnet/team attribution and node-level EKS attribution.
- v1: add optional Kubernetes metadata integration.
- v1 requirement for pod-level accuracy: pod source IP must reach the NAT appliance.

## What About Non-AWS CNIs?

If EKS uses another CNI or overlay, behavior varies:

- Some CNIs use overlay pod CIDRs not directly visible in the VPC.
- Some SNAT at the node.
- Some preserve pod IP only inside the cluster.
- Cilium can provide its own flow identity and Hubble context, but that is a separate integration path.

Product rule:

> Pod-level attribution is supported when the NAT appliance sees routable pod source IPs or when a Kubernetes-side integration reports pod-level egress flows.

## DNS and Domain Attribution For Pods

If pod IP is preserved, DNS correlation can map:

```text
pod IP -> DNS query -> destination IP/domain
```

But domain attribution is best-effort:

- DNS cache can hide queries.
- DoH/DoT bypasses normal DNS.
- Multiple domains can share IPs.
- IPs can change quickly.
- Service mesh/proxy egress can hide original pod identity.

Do not promise exact domain-level billing attribution.

## Service Mesh / Egress Gateway Interaction

If the cluster uses a service mesh or Kubernetes egress gateway:

```text
pod -> sidecar/egress gateway -> BetterNAT
```

BetterNAT may see the egress gateway IP, not the original pod IP.

In that case, exact pod attribution needs integration with the mesh/egress gateway telemetry, not only NAT appliance observation.

## Product UX

Suggested CLI behavior:

```sh
betternat top sources
```

Output can mix source types:

```text
SOURCE TYPE   NAME                         BYTES
pod           prod/checkout/api            1.2 TB
node          ip-10-0-10-25                800 GB
vm            i-abc123 / batch-worker-7    500 GB
subnet        analytics-subnet             300 GB
unknown       10.0.99.44                   50 GB
```

Suggested doctor check:

```sh
betternat doctor eks --cluster prod
```

Checks:

- Can reach Kubernetes API or metadata exporter.
- Pod IP cache is populated.
- Sample pod source IP is visible at NAT appliance.
- EKS CNI external SNAT setting is compatible with pod attribution.
- Prometheus cardinality settings are sane.

## Decision

Pod-level attribution should be an optional but important feature.

Default claim:

> BetterNAT attributes egress by source IP and can enrich traffic with Kubernetes pod/workload metadata when pod source IPs are preserved to the NAT appliance.

Do not claim:

> Always identifies the originating pod in any EKS cluster.

Recommended roadmap:

1. v0: VM/node/subnet/team attribution.
2. v1: Kubernetes metadata integration for pod IP -> workload mapping.
3. v1: EKS doctor check for source IP preservation.
4. v2: optional Kubernetes DaemonSet/exporter for clusters where NAT appliance cannot see pod IPs.
5. Later: Cilium/Hubble or service mesh integrations for richer workload identity.

## Sources

- Amazon EKS external SNAT docs: https://docs.aws.amazon.com/eks/latest/userguide/external-snat.html
- Amazon EKS VPC CNI best practices: https://docs.aws.amazon.com/eks/latest/best-practices/vpc-cni.html
- Amazon EKS managing VPC CNI add-on, including pod IP assignment from VPC: https://docs.aws.amazon.com/eks/latest/userguide/managing-vpc-cni.html
- AWS blog on VPC CNI custom networking, stating the plugin assigns VPC private IPv4/IPv6 addresses to pods: https://aws.amazon.com/blogs/containers/leveraging-cni-custom-networking-alongside-security-groups-for-pods-in-amazon-eks/
- Kubernetes Pod API reference, including `podIP` and `podIPs`: https://kubernetes.io/docs/reference/kubernetes-api/core/pod-v1/
- Kubernetes field selectors, including `status.podIP` support for pods: https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/

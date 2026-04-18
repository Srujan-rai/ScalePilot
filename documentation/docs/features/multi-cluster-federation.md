---
id: multi-cluster-federation
title: Multi-Cluster Workload Federation
sidebar_label: Multi-Cluster Federation
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Multi-Cluster Workload Federation

`FederatedScaledObject` monitors a metric on your primary cluster and automatically spills workloads to overflow clusters when a threshold is exceeded. This enables geographic scale-out, burst-to-cloud, or active-active deployments driven by real observed load.

## How It Works

```
┌───────────────────────────────────────────────────────────┐
│ Every reconcile cycle (30s):                              │
│   1. Reads kubeconfig Secrets, registers cluster clients  │
│   2. Queries spillover metric via Prometheus              │
│   3. If metric > threshold AND not in cooldown:           │
│      - Reads primary Deployment spec (pod template)       │
│      - Creates <name>-overflow Deployment on overflow     │
│        clusters via server-side apply (ordered by prio)   │
│      - Respects per-cluster maxCapacity + priority        │
│   4. If metric ≤ threshold AND not in cooldown:           │
│      - Scales overflow Deployments to 0                   │
│   5. Updates per-cluster health, replicas, spillover flag │
│                                                           │
│ Every 30s (background goroutine):                         │
│   - Health-checks each cluster by calling /version API    │
│   - Sets cluster.healthy=false if API server unreachable  │
└───────────────────────────────────────────────────────────┘
```

### Priority-Based Cluster Selection

Overflow clusters are used in priority order (lowest `priority` value first). If the first cluster is at `maxCapacity`, the controller moves to the next. Unhealthy clusters are skipped entirely.

```
Primary cluster: metric = 75 (threshold = 50, exceeded)

Overflow selection order:
  1. eu-west-1 (priority=1, maxCapacity=20)  → create 20 replicas
  2. ap-south-1 (priority=2, maxCapacity=10) → create 5 replicas (remainder)
  3. us-west-2 (priority=3) [healthy=false]  → SKIPPED
```

## Prerequisites

Before creating a `FederatedScaledObject`, you need:

1. Kubeconfig files for each overflow cluster
2. RBAC in each overflow cluster allowing Deployment create/update
3. Kubeconfig Secrets in `scalepilot-system` namespace

### Step 1: Create kubeconfig Secrets

For each overflow cluster, create a minimal kubeconfig with only the permissions ScalePilot needs:

```yaml title="minimal-rbac.yaml (apply on each overflow cluster)"
apiVersion: v1
kind: ServiceAccount
metadata:
  name: scalepilot-remote
  namespace: production
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: scalepilot-remote
  namespace: production
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "create", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: scalepilot-remote
  namespace: production
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: scalepilot-remote
subjects:
  - kind: ServiceAccount
    name: scalepilot-remote
    namespace: production
```

Store the kubeconfig as a Secret (must be in the `scalepilot-system` namespace and labeled):

```bash
# For each overflow cluster
kubectl create secret generic cluster-eu-west-1 \
  --from-file=kubeconfig=./eu-west-1.kubeconfig \
  --namespace=scalepilot-system

# Required label
kubectl label secret cluster-eu-west-1 \
  scalepilot.io/cluster="true" \
  --namespace=scalepilot-system
```

Verify the Secret:

```bash
kubectl get secret -n scalepilot-system -l scalepilot.io/cluster=true
# NAME                 TYPE     DATA   AGE
# cluster-eu-west-1   Opaque   1      30s
# cluster-ap-south-1  Opaque   1      25s
```

## Complete Example

```yaml title="federated-scaled-object.yaml"
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: FederatedScaledObject
metadata:
  name: order-processor-federation
  namespace: production
spec:
  # Primary cluster where the workload normally runs
  primaryCluster:
    name: us-east-1
    secretRef:
      name: cluster-us-east-1
      namespace: scalepilot-system

  # Overflow clusters (ordered by priority, lowest first)
  overflowClusters:
    - name: eu-west-1
      secretRef:
        name: cluster-eu-west-1
        namespace: scalepilot-system
      namespace: production       # target namespace on this cluster
      maxCapacity: 20             # max replicas to create on this cluster
      priority: 1                 # use first

    - name: ap-south-1
      secretRef:
        name: cluster-ap-south-1
        namespace: scalepilot-system
      namespace: production
      maxCapacity: 10
      priority: 2                 # use second if eu-west-1 is full

  # Trigger metric: spill when this PromQL query exceeds thresholdValue
  metric:
    prometheusAddress: http://prometheus.monitoring.svc:9090
    query: 'sum(kube_deployment_status_replicas{deployment="order-processor"})'
    thresholdValue: "50"          # spill when replica count exceeds 50

  # Which Deployment to replicate
  workload:
    deploymentName: order-processor
    namespace: production

  # Cooldown between scale events (prevents thrashing)
  cooldownSeconds: 120            # 2 minutes between scale events

  # Optional: cap total replicas across all clusters
  maxTotalReplicas: 80
```

```bash
kubectl apply -f federated-scaled-object.yaml
```

## What Gets Created on Overflow Clusters

When spillover activates, ScalePilot reads the primary Deployment's pod template and creates a Deployment named `<original-name>-overflow` on each overflow cluster using **server-side apply**:

```yaml
# Created on eu-west-1 / ap-south-1 automatically by ScalePilot:
apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-processor-overflow   # original name + "-overflow"
  namespace: production
  annotations:
    scalepilot.io/managed-by: "scalepilot"
    scalepilot.io/primary-cluster: "us-east-1"
    scalepilot.io/fso: "order-processor-federation"
spec:
  replicas: 20                     # up to maxCapacity
  # ...pod template copied from primary Deployment...
```

Using `FieldManager: "scalepilot"` with server-side apply means repeated reconciles are idempotent — no conflicts if the Deployment already exists.

## Monitoring Federation Status

```bash
kubectl get federatedscaledobject order-processor-federation -n production -o wide
# NAME                          PRIMARY     PRIMARY-REPLICAS  TOTAL-REPLICAS  SPILLOVER  AGE
# order-processor-federation    us-east-1   50                75              true       5m
```

Detailed status:

```bash
kubectl describe federatedscaledobject order-processor-federation -n production
```

```yaml
Status:
  Primary Replicas: 50
  Total Replicas: 75
  Current Metric Value: "62"
  Spillover Active: true
  Overflow Clusters:
    - Name: eu-west-1
      Replicas: 20
      Healthy: true
      Last Probe Time: 2026-04-18T14:32:15Z
      Last Spill Time: 2026-04-18T14:30:45Z
    - Name: ap-south-1
      Replicas: 5
      Healthy: true
      Last Probe Time: 2026-04-18T14:32:15Z
      Last Spill Time: 2026-04-18T14:30:45Z
```

Using the CLI:

```bash
scalepilot clusters list --namespace production
```

```
FSO                          CLUSTER     ROLE      HEALTHY  REPLICAS  PRIORITY  LAST PROBE
order-processor-federation   us-east-1   primary   ✓        50        0         -
order-processor-federation   eu-west-1   overflow  ✓        20        1         14:32:15
order-processor-federation   ap-south-1  overflow  ✓        5         2         14:32:15
```

## Cooldown Behavior

The `cooldownSeconds` field prevents rapid scale-down after a spike subsides. This is important for workloads that need time to drain in-flight requests.

```
t=0:00  Metric spikes to 62 → Spillover activates → create overflow Deployments
t=0:30  Metric drops to 45 (below threshold=50) → cooldown check
t=0:30  Last scale event was at t=0:00, cooldown=120s not yet elapsed → NO scale-down
t=2:30  Cooldown elapsed, metric still 45 → scale overflow Deployments to 0
```

## Cluster Health Checking

A background goroutine probes each registered cluster every 30 seconds by calling the `/version` endpoint of its API server. If a cluster fails the health check:

- `status.overflowClusters[*].healthy` is set to `false`
- The cluster is skipped during the next spillover decision
- Replicas already running on the cluster are NOT automatically removed (they continue serving traffic)

When the cluster becomes healthy again, it re-enters the rotation on the next reconcile.

## Network Requirements

The ScalePilot operator pod must be able to reach each overflow cluster's Kubernetes API server. Common network configurations:

| Setup | Notes |
|-------|-------|
| VPN or private peering | Recommended for production — kubeconfig uses internal API server URL |
| Public API server | Kubeconfig uses public endpoint; ensure firewall allows operator egress |
| Cloud load balancer | EKS/GKE/AKS public endpoints work out of the box |

:::warning Secret Security
Kubeconfig Secrets contain cluster credentials. Apply appropriate Kubernetes RBAC to limit which pods can read them. Consider using a dedicated ServiceAccount for each overflow cluster with minimal permissions.
:::

## Scaling Down

When the metric drops below the threshold (and cooldown has elapsed), ScalePilot sets overflow Deployment `replicas` to 0 — it does NOT delete the Deployment. This is intentional:

- Faster scale-up next time (Deployment object already exists)
- Kubernetes scheduler keeps the replica set object for historical info
- Avoids repeated create/delete cycles for frequently triggered spillover

If you want to fully remove overflow Deployments, delete the `FederatedScaledObject`:

```bash
kubectl delete federatedscaledobject order-processor-federation -n production
# ScalePilot's finalizer will scale all overflow Deployments to 0
# You must manually delete the overflow Deployments on remote clusters
```

## Related Resources

- **[FederatedScaledObject CRD Reference](../reference/federatedscaledobject)** — Full spec and status fields
- **[CLI: clusters list](../cli/reference#scalepilot-clusters-list)** — Live cluster status table
- **[ClusterScaleProfile](../reference/clusterscaleprofile)** — Cluster-wide scaling governance

---
id: federatedscaledobject
title: FederatedScaledObject CRD Reference
sidebar_label: FederatedScaledObject
---

# FederatedScaledObject CRD Reference

**API Group:** `autoscaling.scalepilot.io/v1alpha1`  
**Kind:** `FederatedScaledObject`  
**Scope:** Namespaced

`FederatedScaledObject` monitors a Prometheus metric on the primary cluster and spills workloads to one or more overflow clusters when the metric exceeds a threshold. Overflow clusters are selected by priority and capacity, health-checked every 30 seconds.

## Full Example

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: FederatedScaledObject
metadata:
  name: order-processor-federation
  namespace: production
spec:
  primaryCluster:
    name: us-east-1
    secretRef:
      name: cluster-us-east-1
      namespace: scalepilot-system
  overflowClusters:
    - name: eu-west-1
      secretRef:
        name: cluster-eu-west-1
        namespace: scalepilot-system
      namespace: production
      maxCapacity: 20
      priority: 1
    - name: ap-south-1
      secretRef:
        name: cluster-ap-south-1
        namespace: scalepilot-system
      namespace: production
      maxCapacity: 10
      priority: 2
  metric:
    prometheusAddress: http://prometheus.monitoring.svc:9090
    query: 'sum(kube_deployment_status_replicas{deployment="order-processor"})'
    thresholdValue: "50"
  workload:
    deploymentName: order-processor
    namespace: production
  cooldownSeconds: 120
  maxTotalReplicas: 80
```

## Spec Reference

### Top-Level Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `primaryCluster` | `ClusterRef` | Yes | — | The main cluster where the workload normally runs |
| `overflowClusters` | `[]ClusterRef` | Yes | — | Clusters that receive spillover workloads (at least 1 required) |
| `metric` | `SpilloverMetric` | Yes | — | The metric that triggers spillover |
| `workload` | `WorkloadTemplate` | Yes | — | The Deployment to replicate to overflow clusters |
| `cooldownSeconds` | `integer` | No | `120` | Minimum seconds between scale events (prevents thrashing; minimum: 30) |
| `maxTotalReplicas` | `integer` | No | — | Cap on total replicas across all clusters (optional) |

### `ClusterRef` (used for `primaryCluster` and `overflowClusters`)

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | `string` | Yes | — | Human-readable cluster identifier (used in status and CLI output) |
| `secretRef` | `SecretReference` | Yes | — | Reference to the Secret containing the cluster kubeconfig |
| `namespace` | `string` | No | Same as primary workload namespace | Target namespace on this cluster for overflow Deployments |
| `maxCapacity` | `int32` | No | — | Maximum replicas this cluster can accept during spillover |
| `priority` | `int32` | No | `0` | Lower values are used first during cluster selection |

### `SecretReference`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | Name of the Secret containing the kubeconfig |
| `namespace` | `string` | Yes | Namespace where the Secret lives (typically `scalepilot-system`) |

The Secret must:
- Contain a `kubeconfig` key with a valid kubeconfig file (base64-encoded)
- Be labeled `scalepilot.io/cluster: "true"`

### `SpilloverMetric`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `prometheusAddress` | `string` | Yes | Prometheus API endpoint to query |
| `query` | `string` | Yes | PromQL expression returning a scalar value |
| `thresholdValue` | `string` | Yes | Decimal threshold; spillover activates when `query result > thresholdValue` |

### `WorkloadTemplate`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `deploymentName` | `string` | Yes | Name of the primary Deployment to copy to overflow clusters |
| `namespace` | `string` | Yes | Namespace of the primary Deployment |

## Status Reference

### `status` Fields

| Field | Type | Description |
|-------|------|-------------|
| `primaryReplicas` | `int32` | Current replica count on the primary cluster |
| `totalReplicas` | `int32` | Sum of replicas across all clusters |
| `currentMetricValue` | `string` | Latest observed metric value from Prometheus |
| `spilloverActive` | `bool` | Whether any overflow clusters currently have non-zero replicas |
| `overflowClusters` | `[]OverflowClusterStatus` | Per-cluster status (see below) |
| `lastScaleTime` | `metav1.Time` | Timestamp of the last scaling action |
| `conditions` | `[]metav1.Condition` | Standard Kubernetes conditions |

### `OverflowClusterStatus`

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Matches the `ClusterRef.name` |
| `replicas` | `int32` | Current replica count on this cluster |
| `healthy` | `bool` | Whether the cluster API server is reachable |
| `lastProbeTime` | `metav1.Time` | When the cluster was last health-checked |
| `lastSpillTime` | `metav1.Time` | When workloads were last spilled to this cluster |

### `status.conditions`

| Type | Status | Reason | Description |
|------|--------|--------|-------------|
| `Ready` | `True` | `Synced` | At least one cluster is healthy and synced |
| `Ready` | `False` | `NoHealthyClusters` | All overflow clusters are unreachable |
| `Ready` | `False` | `MetricError` | Prometheus query failed |
| `SpilloverActive` | `True` | `ThresholdExceeded` | Metric exceeds threshold; replicas running on overflow clusters |
| `SpilloverActive` | `False` | `BelowThreshold` | Metric is below threshold; overflow Deployments scaled to 0 |
| `SpilloverActive` | `False` | `Cooldown` | Threshold exceeded but cooldown period is active |

## kubectl Print Columns

```bash
kubectl get federatedscaledobjects -n production
# NAME                          PRIMARY     PRIMARYREPLICAS  TOTALREPLICAS  SPILLOVER  AGE
# order-processor-federation    us-east-1   50               75             true       1h
```

| Column | JSON Path | Description |
|--------|----------|-------------|
| `PRIMARY` | `.spec.primaryCluster.name` | Primary cluster name |
| `PRIMARYREPLICAS` | `.status.primaryReplicas` | Primary cluster replica count |
| `TOTALREPLICAS` | `.status.totalReplicas` | Aggregate replica count across all clusters |
| `SPILLOVER` | `.status.spilloverActive` | Whether spillover is currently active |
| `AGE` | `.metadata.creationTimestamp` | Resource age |

## Kubeconfig Secret Format

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cluster-eu-west-1
  namespace: scalepilot-system
  labels:
    scalepilot.io/cluster: "true"   # REQUIRED label
type: Opaque
data:
  kubeconfig: <base64-encoded-kubeconfig-file>
```

Generate the Secret with:

```bash
kubectl create secret generic cluster-eu-west-1 \
  --from-file=kubeconfig=./eu-west-1.kubeconfig \
  --namespace=scalepilot-system

kubectl label secret cluster-eu-west-1 \
  scalepilot.io/cluster="true" \
  --namespace=scalepilot-system
```

## Validation Rules

- `overflowClusters` must contain at least 1 item (validated at CRD level via `+kubebuilder:validation:MinItems=1`)
- `cooldownSeconds` must be ≥ 30
- `priority` must be ≥ 0
- `maxCapacity`, when set, must be ≥ 1
- `maxTotalReplicas`, when set, must be ≥ 1

## Related

- **[Multi-Cluster Federation Feature Guide](../features/multi-cluster-federation)**
- **[CLI: scalepilot clusters list](../cli/reference#scalepilot-clusters-list)**
- **[ClusterScaleProfile CRD Reference](./clusterscaleprofile)**

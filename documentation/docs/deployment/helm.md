---
id: helm
title: Helm Deployment Reference
sidebar_label: Helm Values
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Helm Deployment Reference

ScalePilot ships a production-ready Helm chart at `charts/scalepilot/`. This page documents all configurable values and recommended production configurations.

## Quick Install

```bash
helm install scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --create-namespace
```

## Values Reference

### Image Configuration

| Value | Default | Description |
|-------|---------|-------------|
| `image.repository` | `ghcr.io/srujan-rai/scalepilot` | Container image repository |
| `image.tag` | `""` (uses Chart `appVersion`) | Image tag; leave empty to follow chart version |
| `image.pullPolicy` | `IfNotPresent` | Kubernetes image pull policy |
| `imagePullSecrets` | `[]` | List of image pull secret names |
| `nameOverride` | `""` | Override the chart name |
| `fullnameOverride` | `""` | Override the full release name |

### Replica and Scaling

| Value | Default | Description |
|-------|---------|-------------|
| `replicaCount` | `1` | Number of operator replicas. Use 1 for most deployments (leader election handles HA) |

### Service Account and RBAC

| Value | Default | Description |
|-------|---------|-------------|
| `serviceAccount.create` | `true` | Create a ServiceAccount for the operator |
| `serviceAccount.annotations` | `{}` | Annotations to add to the ServiceAccount (e.g. for IRSA/Workload Identity) |
| `serviceAccount.name` | `""` | Name of the ServiceAccount; auto-generated from release name when empty |
| `rbac.create` | `true` | Create ClusterRole and ClusterRoleBinding |

### Leader Election

| Value | Default | Description |
|-------|---------|-------------|
| `leaderElection.enabled` | `true` | Enable leader election (required for safe HA deployments) |

### Metrics

| Value | Default | Description |
|-------|---------|-------------|
| `metrics.enabled` | `true` | Expose Prometheus metrics |
| `metrics.port` | `8080` | Port for the metrics endpoint |
| `metrics.secure` | `false` | Serve metrics over TLS |

### Health Probes

| Value | Default | Description |
|-------|---------|-------------|
| `healthProbe.port` | `8081` | Port for liveness and readiness probes |

### Resource Requests and Limits

| Value | Default | Description |
|-------|---------|-------------|
| `resources.limits.cpu` | `500m` | CPU limit |
| `resources.limits.memory` | `256Mi` | Memory limit |
| `resources.requests.cpu` | `100m` | CPU request |
| `resources.requests.memory` | `128Mi` | Memory request |

### Pod Configuration

| Value | Default | Description |
|-------|---------|-------------|
| `nodeSelector` | `{}` | Node selector labels |
| `tolerations` | `[]` | Pod tolerations |
| `affinity` | `{}` | Pod affinity rules |
| `podAnnotations` | `{}` | Additional annotations for the operator pod |

### Security Context

| Value | Default | Description |
|-------|---------|-------------|
| `podSecurityContext.runAsNonRoot` | `true` | Run as non-root user |
| `podSecurityContext.seccompProfile.type` | `RuntimeDefault` | Seccomp profile |
| `securityContext.allowPrivilegeEscalation` | `false` | Disable privilege escalation |
| `securityContext.capabilities.drop` | `["ALL"]` | Drop all Linux capabilities |
| `securityContext.readOnlyRootFilesystem` | `true` | Read-only root filesystem |

### Prometheus Integration

| Value | Default | Description |
|-------|---------|-------------|
| `prometheusRule.enabled` | `false` | Create a `PrometheusRule` resource (requires prometheus-operator) |
| `prometheusRule.additionalLabels` | `{}` | Labels merged onto the `PrometheusRule` object |

## Example Values Files

<Tabs>
<TabItem value="production" label="Production">

```yaml title="values-production.yaml"
replicaCount: 1

image:
  repository: ghcr.io/srujan-rai/scalepilot
  tag: "v0.1.0"
  pullPolicy: IfNotPresent

serviceAccount:
  create: true
  # IRSA annotation for AWS (IAM Role for Service Accounts)
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789:role/scalepilot-role
  name: scalepilot

rbac:
  create: true

leaderElection:
  enabled: true

metrics:
  enabled: true
  port: 8080
  secure: false

resources:
  limits:
    cpu: "1"
    memory: 512Mi
  requests:
    cpu: 200m
    memory: 256Mi

# Schedule on nodes with more resources
nodeSelector:
  node-role.kubernetes.io/infrastructure: "true"

tolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "infrastructure"
    effect: "NoSchedule"

podAnnotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"

prometheusRule:
  enabled: true
  additionalLabels:
    prometheus: monitoring

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
  readOnlyRootFilesystem: true
```

</TabItem>
<TabItem value="gke-workload-identity" label="GKE Workload Identity">

```yaml title="values-gke.yaml"
serviceAccount:
  create: true
  annotations:
    iam.gke.io/gcp-service-account: scalepilot@YOUR_PROJECT.iam.gserviceaccount.com
  name: scalepilot

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

prometheusRule:
  enabled: true
  additionalLabels:
    release: monitoring
```

</TabItem>
<TabItem value="development" label="Development / Kind">

```yaml title="values-dev.yaml"
replicaCount: 1

image:
  repository: ko.local/scalepilot
  tag: "latest"
  pullPolicy: Never   # use local ko build

leaderElection:
  enabled: false  # faster startup in dev

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 64Mi

prometheusRule:
  enabled: false
```

</TabItem>
</Tabs>

## Upgrading

```bash
# Pull the latest chart
git pull origin main

# Upgrade with existing values
helm upgrade scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --values my-values.yaml
```

Check the diff before upgrading:

```bash
helm diff upgrade scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --values my-values.yaml
```

## Rendering the Chart for Review

```bash
# Render to stdout
helm template scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --values my-values.yaml

# Render to a file for GitOps
helm template scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --values my-values.yaml \
  > rendered/scalepilot.yaml
```

## IRSA / Workload Identity for Cloud Costs

If using `ScalingBudget` with AWS, configure IRSA so the operator pod can call Cost Explorer without storing credentials in Secrets:

```bash
# Create the IAM role and annotate the ServiceAccount
eksctl create iamserviceaccount \
  --name scalepilot \
  --namespace scalepilot-system \
  --cluster my-cluster \
  --attach-policy-arn arn:aws:iam::123456789:policy/ScalePilotCostExplorer \
  --approve
```

Then in `values.yaml`:

```yaml
serviceAccount:
  create: false   # eksctl already created it
  name: scalepilot
```

In your `ScalingBudget`, omit `credentialsSecretRef` and the operator will use the IRSA ambient credentials automatically.

## Prometheus Operator Integration

When `prometheusRule.enabled: true`, the chart creates a `PrometheusRule` with alerts for:

- `ScalePilotModelTrainingFailure` — model training has been failing for > 5 minutes
- `ScalePilotBudgetBreached` — a ScalingBudget is in breach state
- `ScalePilotOperatorDown` — the operator has been unreachable for > 2 minutes

The rule requires the `monitoring.coreos.com/v1` CRD (installed by kube-prometheus-stack or prometheus-operator). Set `prometheusRule.additionalLabels` to match your Prometheus's `ruleSelector`.

## Related Resources

- **[Installation](../getting-started/installation)** — General installation guide
- **[GitHub Actions CI/CD](./github-actions)** — Automate deployments

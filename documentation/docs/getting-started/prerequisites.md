---
id: prerequisites
title: Prerequisites
sidebar_label: Prerequisites
---

# Prerequisites

Before installing ScalePilot, verify your environment meets these requirements.

## Kubernetes Cluster

:::info Minimum Version
ScalePilot requires **Kubernetes v1.27 or later**. The operator uses `autoscaling/v2` HPA resources, server-side apply, and structured logging features introduced in v1.27.
:::

- Kubernetes **v1.27+** (v1.28+ recommended)
- `kubectl` configured with cluster admin access (required to install CRDs)
- At least **1 CPU / 256Mi memory** available for the operator pod

Verify your cluster version:

```bash
kubectl version --short
# Client Version: v1.29.0
# Server Version: v1.29.2
```

## Prometheus

**Prometheus is required** for any `ForecastPolicy` or `FederatedScaledObject`. ScalePilot queries Prometheus using the HTTP API - it does not need to run inside the cluster, but it must be reachable from the operator pod.

Supported Prometheus deployments:

| Deployment | Notes |
|-----------|-------|
| [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) | Recommended - installs Prometheus + Alertmanager + Grafana |
| [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) | Supported, any version |
| Vanilla Prometheus | Supported, v2.x+ |
| Grafana Cloud / Managed Prometheus | Supported (provide the remote read URL as `metricSource.address`) |

Install kube-prometheus-stack if you don't have Prometheus:

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace
```

Verify Prometheus is accessible:

```bash
kubectl port-forward svc/monitoring-kube-prometheus-prometheus -n monitoring 9090:9090 &
curl http://localhost:9090/api/v1/query?query=up
```

## RBAC Permissions

The ScalePilot operator needs **cluster-admin** (or a custom ClusterRole) to:

- Install and manage CRDs
- Read and patch `HorizontalPodAutoscaler` resources
- Read `Deployments`, `Secrets`, `ConfigMaps` across namespaces
- Read and write its own CRs (`ForecastPolicy`, `FederatedScaledObject`, `ScalingBudget`, `ClusterScaleProfile`)

The Helm chart creates all necessary RBAC resources automatically. For production, review `charts/scalepilot/templates/rbac.yaml` and narrow permissions if needed.

## Local Development Requirements

If you want to build or run ScalePilot from source:

| Tool | Minimum Version | Install |
|------|----------------|---------|
| Go | 1.22 | [go.dev/dl](https://go.dev/dl/) |
| make | any | `apt install make` / `brew install make` |
| kubectl | v1.27+ | [kubernetes.io/docs/tasks/tools](https://kubernetes.io/docs/tasks/tools/) |
| kubebuilder | v3 | `go install sigs.k8s.io/kubebuilder/cmd/kubebuilder@latest` |
| ko | latest | `go install github.com/google/ko@latest` |
| golangci-lint | v1.57+ | [golangci-lint.run/usage/install](https://golangci-lint.run/usage/install/) |

Verify Go version:

```bash
go version
# go version go1.22.3 linux/amd64
```

## Optional: Cloud Billing Access

Only required if you want to use `ScalingBudget` with live cloud cost data.

### AWS

- An IAM user or role with the `ce:GetCostAndUsage` permission
- Cost allocation tags enabled on your account
- The namespace must be tagged with `kubernetes-namespace: <namespace-name>`

```json title="Minimal IAM Policy"
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["ce:GetCostAndUsage"],
    "Resource": "*"
  }]
}
```

### GCP

- A GCP service account with `roles/billing.viewer`
- BigQuery billing export enabled for your project
- GKE labels set: `k8s-namespace: <namespace-name>`

### Azure

- An Azure service principal with `Cost Management Reader` role
- Resource tags set: `kubernetes-namespace: <namespace-name>`

## Multi-Cluster Requirements

Only required if you want to use `FederatedScaledObject`.

- **One kubeconfig per overflow cluster**, with permissions to create/update Deployments in the target namespace
- The kubeconfig Secrets must be in the `scalepilot-system` namespace and labeled `scalepilot.io/cluster: "true"`
- Network connectivity from the ScalePilot operator pod to each overflow cluster's API server

## Checklist

Before continuing to [Installation](./installation), verify:

- [ ] Kubernetes cluster v1.27+ with cluster admin access
- [ ] `kubectl` configured and pointing to the right cluster
- [ ] Prometheus deployed and accessible from within the cluster
- [ ] (Optional) Cloud credentials ready for `ScalingBudget`
- [ ] (Optional) Overflow cluster kubeconfigs available for federation

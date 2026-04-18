---
id: installation
title: Installation
sidebar_label: Installation
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Installation

ScalePilot can be installed three ways. **Helm is recommended for production.** Local dev is the fastest path for contributing or evaluating.

## Option 1: Helm (Recommended)

<Tabs>
<TabItem value="helm-basic" label="Basic Install">

```bash
helm install scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --create-namespace
```

</TabItem>
<TabItem value="helm-remote" label="From Git">

```bash
# Clone the repository first
git clone https://github.com/srujan-rai/scalepilot.git
cd scalepilot

helm install scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --create-namespace \
  --set image.repository=ghcr.io/srujan-rai/scalepilot \
  --set image.tag=v0.1.0
```

</TabItem>
<TabItem value="helm-custom" label="With Custom Values">

```bash
helm install scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --create-namespace \
  --values my-values.yaml
```

```yaml title="my-values.yaml"
replicaCount: 1

image:
  repository: ghcr.io/srujan-rai/scalepilot
  tag: "v0.1.0"
  pullPolicy: IfNotPresent

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

leaderElection:
  enabled: true

metrics:
  enabled: true
  port: 8080

# Enable PrometheusRule (requires prometheus-operator CRDs)
prometheusRule:
  enabled: true
  additionalLabels:
    prometheus: monitoring
```

</TabItem>
</Tabs>

Verify the installation:

```bash
kubectl get pods -n scalepilot-system
# NAME                          READY   STATUS    RESTARTS   AGE
# scalepilot-7d4b6c9f5d-xk2p8  1/1     Running   0          45s

kubectl get crd | grep scalepilot
# clusterscaleprofiles.autoscaling.scalepilot.io
# federatedscaledobjects.autoscaling.scalepilot.io
# forecastpolicies.autoscaling.scalepilot.io
# scalingbudgets.autoscaling.scalepilot.io
```

## Option 2: Kustomize / Make

Use this path if you want to deploy from the latest source without building an image.

```bash
# Clone the repository
git clone https://github.com/srujan-rai/scalepilot.git
cd scalepilot

# Install CRDs into your cluster
make install

# Deploy the operator with a pre-built image
make deploy IMG=ghcr.io/srujan-rai/scalepilot:v0.1.0
```

Or build your own image with `ko`:

```bash
# Set your image registry
export KO_DOCKER_REPO=ghcr.io/your-org/scalepilot

# Build and push (ko handles multi-arch, distroless base)
ko build ./cmd/operator/ --push

# Deploy with your image
make deploy IMG=$(ko build ./cmd/operator/ --push)
```

## Option 3: Local Development

This option runs the operator binary directly on your machine using your `~/.kube/config`. It's the fastest way to iterate during development.

```bash
git clone https://github.com/srujan-rai/scalepilot.git
cd scalepilot

# Step 1: Install the CRDs into your cluster (required)
make install

# Step 2: Run the operator locally
make run
```

The `make run` command sets `ENABLE_WEBHOOKS=false` automatically and uses your local kubeconfig. You'll see structured JSON logs in your terminal.

:::tip Port-forward Prometheus
If your Prometheus runs in-cluster, port-forward it before running locally:
```bash
kubectl port-forward svc/monitoring-kube-prometheus-prometheus -n monitoring 9090:9090
```
Then set `metricSource.address: http://localhost:9090` in your ForecastPolicy.
:::

## Installing the CLI

The `scalepilot` CLI is a separate binary from the operator.

```bash
# Build from source
go build -o bin/scalepilot ./cmd/scalepilot/main.go

# Move to PATH
sudo mv bin/scalepilot /usr/local/bin/scalepilot

# Verify
scalepilot version
```

## Verifying the Installation

### Check the operator is running

```bash
kubectl get pods -n scalepilot-system -w
```

### Check CRDs are installed

```bash
kubectl api-resources --api-group=autoscaling.scalepilot.io
# NAME                      SHORTNAMES   APIVERSION                              NAMESPACED   KIND
# clusterscaleprofiles                   autoscaling.scalepilot.io/v1alpha1      false        ClusterScaleProfile
# federatedscaledobjects                 autoscaling.scalepilot.io/v1alpha1      true         FederatedScaledObject
# forecastpolicies                       autoscaling.scalepilot.io/v1alpha1      true         ForecastPolicy
# scalingbudgets                         autoscaling.scalepilot.io/v1alpha1      true         ScalingBudget
```

### Check operator logs

```bash
kubectl logs -n scalepilot-system -l app.kubernetes.io/name=scalepilot -f
```

## Uninstalling

```bash
# Helm
helm uninstall scalepilot -n scalepilot-system

# Remove CRDs (this deletes all ForecastPolicy, ScalingBudget etc. resources)
kubectl delete crd \
  forecastpolicies.autoscaling.scalepilot.io \
  federatedscaledobjects.autoscaling.scalepilot.io \
  scalingbudgets.autoscaling.scalepilot.io \
  clusterscaleprofiles.autoscaling.scalepilot.io

# Remove namespace
kubectl delete namespace scalepilot-system
```

:::warning CRD Deletion
Deleting CRDs will permanently delete all ScalePilot custom resources in your cluster. Make sure to back up any important configurations before uninstalling.
:::

## Next Steps

- **[Quick Start](./quick-start)** — Deploy your first ForecastPolicy in 5 minutes
- **[Helm Values Reference](../deployment/helm)** — All available Helm values
- **[Predictive Scaling](../features/predictive-scaling)** — Configure ForecastPolicy

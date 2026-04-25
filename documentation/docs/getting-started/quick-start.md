---
id: quick-start
title: Quick Start
sidebar_label: Quick Start
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Quick Start - 5-Minute Tutorial

This tutorial walks you through deploying a sample workload and attaching a `ForecastPolicy` to it. By the end, ScalePilot will be pre-scaling your HPA based on Prometheus predictions.

:::info Prerequisites
- Kubernetes v1.27+ cluster with cluster admin access
- Prometheus running (see [Prerequisites](./prerequisites))
- ScalePilot installed (see [Installation](./installation))
:::

## Step 1: Create the target namespace and workload

```bash
kubectl create namespace production
```

Deploy a sample web application with an HPA:

```yaml title="web-frontend.yaml"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-frontend
  namespace: production
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web-frontend
  template:
    metadata:
      labels:
        app: web-frontend
    spec:
      containers:
        - name: app
          image: nginx:1.27-alpine
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 500m
              memory: 128Mi
          ports:
            - containerPort: 80
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web-frontend-hpa
  namespace: production
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: web-frontend
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

```bash
kubectl apply -f web-frontend.yaml
```

Verify both resources are running:

```bash
kubectl get deployment,hpa -n production
```

## Step 2: Create a ForecastPolicy

This policy trains an ARIMA model on HTTP request rate and pre-scales the HPA 5 minutes ahead of predicted spikes.

```yaml title="forecast-policy.yaml"
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ForecastPolicy
metadata:
  name: web-frontend-forecast
  namespace: production
spec:
  targetDeployment:
    name: web-frontend
  targetHPA:
    name: web-frontend-hpa

  metricSource:
    address: http://monitoring-kube-prometheus-prometheus.monitoring.svc:9090
    # Replace with a metric that actually exists in your cluster.
    # This example uses kube_deployment_status_replicas as a stand-in.
    query: 'sum(rate(http_requests_total{namespace="production"}[5m]))'
    historyDuration: "7d"
    stepInterval: "5m"

  algorithm: ARIMA
  arimaParams:
    p: 2
    d: 1
    q: 1

  leadTimeMinutes: 5
  retrainIntervalMinutes: 30
  maxReplicaCap: 15
  dryRun: true   # Start in dry-run mode to see predictions without patching
```

```bash
kubectl apply -f forecast-policy.yaml
```

:::tip Start with dryRun: true
Enable `dryRun: true` when first deploying a ForecastPolicy. ScalePilot will log predictions and intended HPA patches without applying them - giving you a chance to validate model accuracy with `scalepilot simulate`.
:::

## Step 3: Watch the ForecastPolicy status

```bash
kubectl get forecastpolicy -n production -w
# NAME                    ALGORITHM  DEPLOYMENT     PREDICTED  ACTIVE  AGE
# web-frontend-forecast   ARIMA      web-frontend   <none>     <none>  10s
```

After the first reconcile (up to 60 seconds), you should see:

```bash
kubectl get forecastpolicy web-frontend-forecast -n production -o wide
# NAME                    ALGORITHM  DEPLOYMENT     PREDICTED  ACTIVE  AGE
# web-frontend-forecast   ARIMA      web-frontend   4          2       2m30s
```

Inspect the detailed status:

```bash
kubectl describe forecastpolicy web-frontend-forecast -n production
```

Look for the `Status.Conditions` section:
- `ModelReady: True` - model was trained successfully
- `PatchApplied: True` - HPA was patched (or `False` with reason `DryRun` if dryRun is enabled)

## Step 4: Simulate the forecast

Use the CLI to run the model against live data and see the predicted values:

```bash
scalepilot simulate web-frontend-forecast \
  --namespace production \
  --horizon 1h \
  --step 5m
```

Expected output:

```
Training ARIMA(2,1,1) on 2016 data points...
Model trained (RMSE: 2.3412)

TIME      PREDICTED  LOWER_95  UPPER_95
14:35:00  45.23      38.12     52.34
14:40:00  48.91      39.45     58.37
14:45:00  52.10      40.78     63.42
14:50:00  49.77      38.12     61.42
...

Peak (point)=52.10  implied minReplicas=4 (targetMetricValuePerReplica="")
```

If the RMSE looks reasonable and the predictions track your real traffic patterns, disable dry run:

```bash
kubectl patch forecastpolicy web-frontend-forecast -n production \
  --type='merge' -p '{"spec":{"dryRun":false}}'
```

## Step 5: Check the status

```bash
# Using kubectl
kubectl get forecastpolicies -n production

# Using the CLI
scalepilot status --namespace production
```

```
NAMESPACE    NAME                    ALGORITHM  DEPLOYMENT     PREDICTED  ACTIVE  LAST TRAINED        STATUS
production   web-frontend-forecast   ARIMA      web-frontend   6          4       2026-04-18 14:30    Ready
```

ScalePilot raised the HPA `minReplicas` from 2 to 4 based on the prediction. The HPA will then scale the Deployment up to match.

## Optional: Add a ClusterScaleProfile

A `ClusterScaleProfile` is cluster-scoped and governs all scaling operations:

```yaml title="cluster-scale-profile.yaml"
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ClusterScaleProfile
metadata:
  name: default
spec:
  maxSurgePercent: 25
  defaultCooldownSeconds: 60
  enableGlobalDryRun: false

  blackoutWindows:
    - name: maintenance-window
      start: "0 2 * * 6"    # Saturday 2am
      end: "0 6 * * 6"      # Saturday 6am
      timezone: UTC
```

```bash
kubectl apply -f cluster-scale-profile.yaml
```

## What Just Happened?

```
┌─────────────────────────────────────────────────────────────┐
│ Every 30 minutes (background goroutine):                    │
│   1. Queries Prometheus for 7 days of HTTP request history  │
│   2. Trains ARIMA(2,1,1) model                              │
│   3. Serializes model coefficients to ConfigMap             │
│                                                             │
│ Every 60 seconds (reconcile loop):                          │
│   1. Loads cached ARIMA model from ConfigMap (fast, O(1))   │
│   2. Predicts metric for next 5 minutes                     │
│   3. Computes implied replica count                         │
│   4. If predicted > current HPA minReplicas → patches HPA   │
│   5. Updates ForecastPolicy.Status with predictions         │
└─────────────────────────────────────────────────────────────┘
```

## Troubleshooting

<Tabs>
<TabItem value="model-not-trained" label="Model not training">

If `ModelReady` condition is `False`, check the operator logs:

```bash
kubectl logs -n scalepilot-system -l app.kubernetes.io/name=scalepilot | grep -i error
```

Common causes:
- Prometheus URL is incorrect or unreachable
- PromQL query returns no data (check it in the Prometheus UI first)
- Insufficient historical data (need at least `p+d+q+10` data points for ARIMA)

</TabItem>
<TabItem value="hpa-not-patched" label="HPA not being patched">

Check the `PatchApplied` condition:

```bash
kubectl describe forecastpolicy web-frontend-forecast -n production | grep -A 5 PatchApplied
```

Common causes:
- `dryRun: true` is still set
- Prediction is lower than current `minReplicas` (ScalePilot only raises `minReplicas`, never lowers it)
- `ClusterScaleProfile` has an active blackout window
- `scaleUpGuard` is blocking the patch

</TabItem>
<TabItem value="prometheus-query" label="Validate PromQL query">

Test your PromQL query directly against Prometheus before configuring ForecastPolicy:

```bash
# Port-forward Prometheus
kubectl port-forward svc/monitoring-kube-prometheus-prometheus -n monitoring 9090:9090

# Test the query
curl -sG 'http://localhost:9090/api/v1/query' \
  --data-urlencode 'query=sum(rate(http_requests_total{namespace="production"}[5m]))' \
  | jq '.data.result'
```

The query must return a **scalar or single-element vector**. If it returns multiple time series, add aggregation (`sum(...)`, `avg(...)`, etc.).

</TabItem>
</Tabs>

## Next Steps

- **[Predictive Scaling In Depth](../features/predictive-scaling)** - Algorithm selection, ScaleUpGuard, confidence bounds
- **[Multi-Cluster Federation](../features/multi-cluster-federation)** - Spill workloads to overflow clusters
- **[FinOps Budgets](../features/finops-budgets)** - Add cost controls with ScalingBudget
- **[CLI Reference](../cli/reference)** - All CLI commands

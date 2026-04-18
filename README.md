# ScalePilot

**Cloud-agnostic Kubernetes autoscaling operator with predictive scaling, multi-cluster workload federation, and FinOps budget controls.**

ScalePilot extends HPA and KEDA with three capabilities that don't exist anywhere in the open-source Kubernetes ecosystem today:

1. **Predictive scaling** using ARIMA and Holt-Winters time-series forecasting
2. **Multi-cluster workload federation** with metric-driven spillover
3. **Namespace-scoped FinOps cost budgets** with automatic breach enforcement

---

## Table of Contents

- [Why ScalePilot](#why-scalepilot)
- [Architecture](#architecture)
- [Custom Resources](#custom-resources)
  - [ForecastPolicy](#1-forecastpolicy)
  - [FederatedScaledObject](#2-federatedscaledobject)
  - [ScalingBudget](#3-scalingbudget)
  - [ClusterScaleProfile](#4-clusterscaleprofile)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Quick Start](#quick-start)
- [CLI Reference](#cli-reference)
- [Configuration Guide](#configuration-guide)
  - [Forecasting Algorithms](#forecasting-algorithms)
  - [Multi-Cluster Setup](#multi-cluster-setup)
  - [Cloud Cost Integration](#cloud-cost-integration)
  - [Notifications](#notifications)
- [Project Structure](#project-structure)
- [Development](#development)
- [How It Works Internally](#how-it-works-internally)
- [Tech Stack](#tech-stack)
- [License](#license)

---

## Why ScalePilot

| Capability | HPA | KEDA | Kubecost | Admiralty | ScalePilot |
|------------|-----|------|----------|-----------|------------|
| Reactive scaling | Yes | Yes | No | No | Yes |
| Predictive scaling (forecast) | No | No | No | No | **Yes** |
| Multi-cluster spillover | No | No | No | Placement-based | **Metric-driven** |
| Cost observability | No | No | Yes | No | Yes |
| Cost enforcement (block/delay scaling) | No | No | No | No | **Yes** |
| Blackout windows | No | No | No | No | **Yes** |
| Per-team scaling governance | No | No | No | No | **Yes** |

**The core problem:** Kubernetes HPA is reactive. It sees a CPU spike *right now* and adds pods. By the time those pods are scheduled, images pulled, containers started, and health checks passed, you've already dropped requests for 2-5 minutes. KEDA expands the metric sources (Kafka, Prometheus, 60+ scalers) but the reaction model is the same.

**ScalePilot's answer:** Train a forecasting model on your Prometheus metric history. Predict the spike 5 minutes before it happens. Pre-scale your HPA minReplicas so pods are warm and ready when traffic arrives.

---

## Architecture

```
                                    ScalePilot Operator
                                   ┌──────────────────────────────────────┐
                                   │                                      │
  Prometheus  ◄────── queries ─────┤  ForecastPolicy Reconciler           │
  (metrics)                        │    ├─ Trains ARIMA/HoltWinters       │
                                   │    ├─ Caches model in ConfigMap      │
  HPA         ◄────── patches ─────┤    └─ Patches HPA minReplicas       │
  (autoscaler)                     │                                      │
                                   │  FederatedScaledObject Reconciler    │
  Overflow    ◄── server-side ─────┤    ├─ Monitors spillover metric      │
  Clusters       apply             │    ├─ Manages ClusterRegistry        │
  (kubeconfig                      │    └─ Creates overflow Deployments   │
   Secrets)                        │                                      │
                                   │  ScalingBudget Reconciler            │
  AWS/GCP/    ◄────── polls ───────┤    ├─ Polls cloud cost APIs          │
  Azure                            │    ├─ Computes utilization           │
  (billing)                        │    └─ Enforces breach actions        │
                                   │                                      │
  Slack/      ◄──── webhooks ──────┤  ClusterScaleProfile Reconciler     │
  PagerDuty                        │    ├─ Evaluates blackout windows     │
                                   │    └─ Applies team overrides         │
                                   └──────────────────────────────────────┘
```

**Key design principles:**

- **Non-blocking reconcilers** — All reconcilers use `ctrl.Result{RequeueAfter: ...}` and never block on long operations. Model training happens in background goroutines.
- **Dependency injection** — Every external dependency (Prometheus, cloud APIs, cluster clients) is injected via interfaces, making the operator fully testable.
- **No global state** — The cluster registry is `sync.RWMutex`-protected. All shared state flows through the controller-runtime manager.
- **Idempotent writes** — Cross-cluster Deployments use server-side apply with `FieldManager: "scalepilot"` so repeated reconciles never conflict.

---

## Custom Resources

### 1. ForecastPolicy

Attaches to a Deployment, reads Prometheus metric history, runs ARIMA or Holt-Winters forecast, and patches HPA `minReplicas` before predicted traffic spikes.

**How it works:**

```
┌─────────────────────────────────────────────────────────┐
│ Every retrainIntervalMinutes (default 30m):             │
│   1. Background goroutine queries Prometheus history    │
│   2. Trains ARIMA(p,d,q) or HoltWinters model          │
│   3. Serializes model params to ConfigMap               │
│                                                         │
│ Every reconcile cycle (60s):                            │
│   1. Loads cached model from ConfigMap (fast, no math)  │
│   2. Predicts metric value for next leadTimeMinutes     │
│   3. Converts prediction to replica count               │
│   4. If predicted > current HPA minReplicas → patches   │
│   5. Updates status with prediction + conditions        │
└─────────────────────────────────────────────────────────┘
```

**Example:**

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ForecastPolicy
metadata:
  name: web-frontend-forecast
  namespace: production
spec:
  # Which Deployment and HPA to manage
  targetDeployment:
    name: web-frontend
  targetHPA:
    name: web-frontend-hpa

  # Where to get training data
  metricSource:
    address: http://prometheus.monitoring:9090
    query: 'sum(rate(http_requests_total{deployment="web-frontend"}[5m]))'
    historyDuration: "7d"       # train on 7 days of data
    stepInterval: "5m"          # 5-minute resolution

  # Forecasting model
  algorithm: ARIMA              # or HoltWinters
  arimaParams:
    p: 2                        # autoregressive order
    d: 1                        # differencing order
    q: 1                        # moving-average order

  # Timing
  leadTimeMinutes: 5            # pre-scale 5 minutes ahead
  retrainIntervalMinutes: 30    # retrain model every 30 minutes

  # Safety
  maxReplicaCap: 50             # never set minReplicas above 50
  dryRun: false                 # set true to log without patching

  # Optional: block pre-scale when an SLO metric is hot (reuses metricSource.address if address omitted)
  # scaleUpGuard:
  #   query: 'sum(rate(http_requests_total{status=~"5.."}[5m]))'
  #   maxMetricValue: "50"
```

**KEDA, in-cluster Prometheus, metrics:** ForecastPolicy targets an `autoscaling/v2` HorizontalPodAutoscaler—the same resource KEDA manages when you use a `ScaledObject`. Set `metricSource.address` to an in-cluster Prometheus URL (for example `http://prometheus.monitoring.svc:9090`). The operator exposes Prometheus counters on the controller manager metrics bind address (see Helm `metrics.enabled` / `metrics.port`).

**ForecastPolicy metrics (`result` label):**

| Metric | `result` values | Meaning |
|--------|-----------------|--------|
| `scalepilot_forecastpolicy_training_total` | `success`, `failure` | Background model train wrote ConfigMap vs reported `TrainingFailed` |
| `scalepilot_forecastpolicy_hpa_minreplicas_patch_total` | `applied`, `skipped_dry_run`, `skipped_profile`, `skipped_guard` | HPA minReplicas raise applied vs skipped (policy dry-run, `ClusterScaleProfile`, or `scaleUpGuard`) |

**`scaleUpGuard` fields** (all optional except query and maxMetricValue when the object is set; validated by the CRD webhook):

| Field | Required | Description |
|-------|----------|-------------|
| `address` | No | Prometheus base URL; if empty, uses `metricSource.address` |
| `query` | Yes | Instant PromQL returning a scalar |
| `maxMetricValue` | Yes | If instant value is **strictly greater** than this number, the reconciler does not raise `minReplicas` (condition `PatchApplied` / reason `ScaleUpGuardBlocked`) |

**Helm alerting:** set `prometheusRule.enabled: true` to create a `monitoring.coreos.com/v1` `PrometheusRule` (requires prometheus-operator in the cluster). Optional `prometheusRule.additionalLabels` are merged onto that object.

**Status fields (`kubectl get forecastpolicy`):**

| Column | Description |
|--------|-------------|
| Algorithm | ARIMA or HoltWinters |
| Deployment | Target deployment name |
| Predicted | Forecasted minReplicas |
| Active | Current HPA minReplicas |
| Age | Time since creation |

**Spec reference:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `targetDeployment.name` | string | Yes | - | Deployment to forecast for |
| `targetHPA.name` | string | Yes | - | HPA whose minReplicas gets patched |
| `metricSource.address` | string | Yes | - | Prometheus server URL |
| `metricSource.query` | string | Yes | - | PromQL query returning a scalar |
| `metricSource.historyDuration` | string | Yes | - | Training data window (e.g. "7d") |
| `metricSource.stepInterval` | string | No | "5m" | Range query resolution |
| `algorithm` | enum | No | ARIMA | ARIMA or HoltWinters |
| `arimaParams` | object | No | p=2,d=1,q=1 | ARIMA model hyperparameters |
| `holtWintersParams` | object | No | - | Holt-Winters smoothing coefficients |
| `leadTimeMinutes` | int | No | 5 | Minutes to pre-scale ahead (3-10) |
| `retrainIntervalMinutes` | int | No | 30 | How often to retrain the model |
| `maxReplicaCap` | int | No | - | Ceiling for forecast-driven minReplicas |
| `dryRun` | bool | No | false | Log predictions without patching |
| `targetMetricValuePerReplica` | string | No | - | Forecast metric budget per replica; replicas ≈ ceil(peak / value) |
| `useUpperConfidenceBound` | bool | No | false | Use upper confidence bound for peak instead of point forecast |
| `scaleUpGuard` | object | No | - | Optional instant PromQL guard: skip raising minReplicas when value > `maxMetricValue` |

---

### 2. FederatedScaledObject

Defines a primary cluster and overflow clusters. Monitors a Prometheus metric (e.g. queue depth) and spills workloads to overflow clusters when the threshold is exceeded.

**How it works:**

```
┌───────────────────────────────────────────────────────────┐
│ Every reconcile cycle (30s):                              │
│   1. Reads kubeconfig Secrets, registers clusters         │
│   2. Queries spillover metric via Prometheus              │
│   3. If metric > threshold AND not in cooldown:           │
│      - Reads primary Deployment spec (pod template)       │
│      - Creates <name>-overflow Deployment on overflow     │
│        clusters via server-side apply                     │
│      - Respects per-cluster maxCapacity + priority        │
│   4. If metric < threshold AND not in cooldown:           │
│      - Scales overflow Deployments to 0                   │
│   5. Updates per-cluster health, replicas, spillover flag │
│                                                           │
│ Every 30s (background goroutine):                         │
│   - Health-checks all registered clusters (/version API)  │
└───────────────────────────────────────────────────────────┘
```

**Example:**

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
      name: cluster-us-east-1           # Secret with key "kubeconfig"
      namespace: scalepilot-system

  overflowClusters:
    - name: eu-west-1
      secretRef:
        name: cluster-eu-west-1
        namespace: scalepilot-system
      maxCapacity: 20                    # max 20 replicas on this cluster
      priority: 1                        # used first (lowest priority wins)
    - name: ap-south-1
      secretRef:
        name: cluster-ap-south-1
        namespace: scalepilot-system
      maxCapacity: 10
      priority: 2                        # used second

  metric:
    query: 'sum(kube_deployment_status_replicas{deployment="order-processor"})'
    prometheusAddress: http://prometheus.monitoring:9090
    thresholdValue: "50"                 # spill when metric exceeds 50

  workload:
    deploymentName: order-processor
    namespace: production

  cooldownSeconds: 120                   # wait 2min between scale events
  maxTotalReplicas: 80                   # cap across all clusters
```

**Kubeconfig Secret format:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cluster-eu-west-1
  namespace: scalepilot-system
  labels:
    scalepilot.io/cluster: "true"        # required label
type: Opaque
data:
  kubeconfig: <base64-encoded-kubeconfig>
```

---

### 3. ScalingBudget

Defines a namespace-scoped cost ceiling. Polls cloud billing APIs, computes utilization, and enforces breach actions when spend exceeds the ceiling.

**How it works:**

```
┌───────────────────────────────────────────────────────────┐
│ Every pollIntervalMinutes (default 5m):                   │
│   1. Queries cloud cost API for namespace spend           │
│      (filtered by k8s namespace tag/label)                │
│   2. Computes utilization = spend / ceiling * 100         │
│   3. If utilization crosses warningThresholdPercent:       │
│      → Sends Slack/PagerDuty warning notification         │
│   4. If spend >= ceiling (first breach):                  │
│      → Sends critical notification                        │
│      → Activates breach action (Downgrade/Delay/Block)    │
│   5. Updates status: spend, utilization%, breached flag   │
│                                                           │
│ Cost data is cached in-memory with 5-minute TTL to        │
│ avoid hitting cloud API rate limits.                      │
└───────────────────────────────────────────────────────────┘
```

**Breach actions explained:**

| Action | Behavior |
|--------|----------|
| `Delay` | Pauses all scale-up events in the namespace until the next billing period. Existing replicas are not affected. |
| `Downgrade` | Reduces CPU/memory requests for new pods created by scale-up events. Existing pods remain unchanged. |
| `Block` | Rejects scale-up entirely. Pairs with a validating admission webhook to prevent new replicas. |

**Example:**

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ScalingBudget
metadata:
  name: production-budget
  namespace: scalepilot-system
spec:
  namespace: production                  # which namespace to budget
  ceilingMillidollars: 150000            # $150.00/month (integer avoids float rounding)

  cloudCost:
    provider: AWS                        # AWS, GCP, or Azure
    credentialsSecretRef:
      name: aws-cost-explorer-creds      # Secret with AWS credentials
      namespace: scalepilot-system
    region: us-east-1

  breachAction: Delay                    # Downgrade | Delay | Block
  warningThresholdPercent: 80            # alert at 80% spend
  pollIntervalMinutes: 5

  notifications:
    slack:
      webhookURL: https://hooks.slack.com/services/T.../B.../xxx
      channel: "#finops-alerts"
    pagerDuty:
      routingKey: your-routing-key
      severity: warning                  # critical | error | warning | info
```

**Cloud credentials Secret format:**

```yaml
# AWS
apiVersion: v1
kind: Secret
metadata:
  name: aws-cost-explorer-creds
type: Opaque
stringData:
  aws_access_key_id: AKIA...
  aws_secret_access_key: ...

# GCP
apiVersion: v1
kind: Secret
metadata:
  name: gcp-billing-creds
type: Opaque
stringData:
  service_account_json: |
    { "type": "service_account", ... }

# Azure
apiVersion: v1
kind: Secret
metadata:
  name: azure-cost-creds
type: Opaque
stringData:
  tenant_id: ...
  client_id: ...
  client_secret: ...
  subscription_id: ...
```

---

### 4. ClusterScaleProfile

Cluster-wide (not namespaced) resource that defines scaling governance: max surge percentages, cron-based blackout windows, and per-team constraints.

**How it works:**

```
┌───────────────────────────────────────────────────────────┐
│ Every 30 seconds:                                         │
│   1. Evaluates each blackout window against current time  │
│      (respects IANA timezone per window)                  │
│   2. If current time falls between a start/end cron:      │
│      → Sets status.activeBlackout = true                  │
│      → Other reconcilers check this before scaling        │
│   3. Counts configured team overrides                     │
│   4. Updates Ready condition                              │
└───────────────────────────────────────────────────────────┘
```

**Example:**

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ClusterScaleProfile
metadata:
  name: default                          # cluster-scoped, no namespace
spec:
  maxSurgePercent: 25                    # max 25% replica increase per cycle
  defaultCooldownSeconds: 60             # 60s between scale-ups
  enableGlobalDryRun: false              # set true to disable all scaling

  blackoutWindows:
    - name: maintenance-friday
      start: "0 22 * * 5"               # Friday 10pm
      end: "0 6 * * 6"                  # Saturday 6am
      timezone: America/New_York

    - name: deploy-freeze-eom
      start: "0 0 28 * *"              # 28th of every month midnight
      end: "0 0 1 * *"                 # 1st of next month midnight
      timezone: UTC

  teamOverrides:
    - teamName: platform-team
      namespaces: [platform, infrastructure]
      maxSurgePercent: 50                # platform team gets higher surge
      maxReplicasPerDeployment: 100

    - teamName: app-team
      namespaces: [production, staging]
      maxSurgePercent: 25
      allowedAlgorithms: [ARIMA]         # restrict to ARIMA only
      maxReplicasPerDeployment: 50
```

**Blackout window cron syntax:**

Uses standard 5-field cron: `minute hour day-of-month month day-of-week`

| Expression | Meaning |
|-----------|---------|
| `0 22 * * 5` | Every Friday at 10:00 PM |
| `0 6 * * 6` | Every Saturday at 6:00 AM |
| `0 0 28 * *` | 28th of every month at midnight |
| `30 2 * * 0,6` | Weekends at 2:30 AM |

---

## Getting Started

### Prerequisites

- Kubernetes cluster (v1.27+)
- `kubectl` configured to access the cluster
- Prometheus deployed in the cluster (for ForecastPolicy)
- Go 1.22+ (for building from source)

### Installation

**Option 1: Helm (recommended)**

```bash
helm install scalepilot charts/scalepilot \
  --namespace scalepilot-system \
  --create-namespace
```

**Option 2: Kustomize**

```bash
# Install CRDs
make install

# Deploy the operator
make deploy IMG=ghcr.io/srujan-rai/scalepilot:latest
```

**Option 3: Run locally (development)**

```bash
# Install CRDs into your cluster
make install

# Run the operator on your machine (uses ~/.kube/config)
make run
```

### Quick Start

1. **Install the CRDs and run the operator:**

```bash
make install
make run
```

2. **Create sample namespaces and CRs** (uses kustomize so `kustomization.yaml` is not sent to the API as a resource):

```bash
kubectl apply -k config/samples/
```

Or apply a single sample (create namespaces first: `kubectl create ns production` if needed):

```bash
kubectl apply -f config/samples/autoscaling_v1alpha1_forecastpolicy.yaml
```

3. **Check the status:**

```bash
kubectl get forecastpolicies -o wide

# Or use the CLI
./bin/scalepilot status
```

4. **Simulate a forecast against historical data:**

```bash
./bin/scalepilot simulate web-frontend-forecast \
  --namespace production \
  --horizon 1h \
  --step 5m
```

5. **End-to-end on Minikube with Prometheus** (demo Deployment, metrics, port-forward, ForecastPolicy that trains): [hack/minikube-demo/README.md](hack/minikube-demo/README.md).

---

## CLI Reference

Build the CLI: `go build -o bin/scalepilot ./cmd/scalepilot/main.go`

### `scalepilot status`

Displays a live table of all ForecastPolicies with predictions vs actual values.

```
NAMESPACE    NAME                   ALGORITHM  DEPLOYMENT     PREDICTED  ACTIVE  LAST TRAINED      STATUS
production   web-frontend-forecast  ARIMA      web-frontend   12         8       2026-03-29 14:30  Ready
staging      api-gateway-forecast   HoltWinters api-gateway   5          5       2026-03-29 14:25  Ready
```

### `scalepilot simulate <policy-name>`

Dry-runs a forecast against live Prometheus data to validate model accuracy before enabling.

```bash
scalepilot simulate web-frontend-forecast \
  --namespace production \
  --horizon 1h \
  --step 5m
```

```
Training ARIMA(2,1,1) on 2016 data points...
Model trained (RMSE: 2.3412)

TIME      PREDICTED  LOWER_95  UPPER_95
14:35:00  45.23      38.12     52.34
14:40:00  48.91      39.45     58.37
14:45:00  52.10      40.78     63.42
...
```

### `scalepilot budget status`

Shows namespace spend vs budget ceiling for all ScalingBudgets.

```
NAMESPACE    NAME              PROVIDER  CEILING   SPEND    UTILIZATION  BREACHED  ACTION  BLOCKED
production   production-budget AWS       $150.00   $120.50  80%          No        Delay   0
staging      staging-budget    GCP       $50.00    $52.30   104%         YES       Block   3
```

### `scalepilot clusters list`

Lists all overflow clusters and their health status.

```
FSO                          CLUSTER     ROLE      HEALTHY  REPLICAS  PRIORITY  LAST PROBE
order-processor-federation   us-east-1   primary   ✓        10        0         -
order-processor-federation   eu-west-1   overflow  ✓        5         1         14:32:15
order-processor-federation   ap-south-1  overflow  ✗        0         2         14:32:15
```

### `scalepilot validate <file...>`

Lints ScalePilot CRD manifests before applying them. Catches misconfigurations like specifying `algorithm: ARIMA` without `arimaParams`.

```bash
scalepilot validate config/samples/*.yaml
```

```
OK   config/samples/autoscaling_v1alpha1_forecastpolicy.yaml
OK   config/samples/autoscaling_v1alpha1_clusterscaleprofile.yaml
FAIL config/samples/bad-policy.yaml: arimaParams required when algorithm is ARIMA
```

### `scalepilot install`

Renders and applies the Helm chart to the current cluster.

```bash
scalepilot install \
  --release-name scalepilot \
  --target-namespace scalepilot-system \
  --values my-values.yaml
```

### `scalepilot version`

```
ScalePilot CLI
  Version:    v0.1.0
  Git Commit: a1b2c3d
  Build Date: 2026-03-29T14:00:00Z
  Go Version: go1.23.1
  Controller Runtime: v0.17.3
```

---

## Configuration Guide

### Forecasting Algorithms

**ARIMA(p,d,q)** — Best for stationary or trend-based metrics (CPU, request rate).

| Parameter | What it controls | Typical values |
|-----------|-----------------|----------------|
| `p` | Autoregressive order — how many past values influence the current | 1-3 |
| `d` | Differencing order — how many times to difference for stationarity | 0-2 |
| `q` | Moving-average order — how many past forecast errors to consider | 0-2 |

Start with `p=2, d=1, q=1`. If your metric has no trend, try `d=0`. Check RMSE via `scalepilot simulate`.

**Holt-Winters (triple exponential smoothing)** — Best for metrics with seasonality (daily/weekly traffic patterns).

| Parameter | What it controls | Typical values |
|-----------|-----------------|----------------|
| `alpha` | Level smoothing — how fast the baseline adapts | 0.2-0.5 |
| `beta` | Trend smoothing — how fast the trend adapts | 0.05-0.2 |
| `gamma` | Seasonal smoothing — how fast seasonal patterns adapt | 0.1-0.3 |
| `seasonalPeriods` | Data points in one cycle (e.g. 24 for hourly data with daily cycle) | 12, 24, 168 |

### Multi-Cluster Setup

1. **Create a kubeconfig for each overflow cluster** with minimal RBAC (create/update Deployments in target namespaces).

2. **Store each kubeconfig as a Secret:**
```bash
kubectl create secret generic cluster-eu-west-1 \
  --from-file=kubeconfig=./eu-west-1.kubeconfig \
  --namespace=scalepilot-system

kubectl label secret cluster-eu-west-1 \
  scalepilot.io/cluster=true \
  --namespace=scalepilot-system
```

3. **Create a FederatedScaledObject** referencing those Secrets (see example above).

The operator health-checks each cluster every 30 seconds by calling the `/version` API endpoint. Unhealthy clusters are skipped during spillover.

### Cloud Cost Integration

ScalePilot queries cloud billing APIs using cost allocation tags. Your Kubernetes namespace must be tagged in your cloud provider:

| Provider | Tag/Label | IAM Permission Required |
|----------|-----------|------------------------|
| AWS | Cost allocation tag `kubernetes-namespace` | `ce:GetCostAndUsage` |
| GCP | GKE label `k8s-namespace` (via BigQuery billing export) | `roles/billing.viewer` |
| Azure | Resource tag `kubernetes-namespace` | Cost Management Reader |

### Notifications

Notifications are sent on two events:
- **Warning**: When spend crosses `warningThresholdPercent` (default 80%)
- **Breach**: When spend exceeds the ceiling (first occurrence only, not every poll)

Both Slack and PagerDuty can be configured simultaneously. If a notification fails, it is logged but does not block the reconciler.

---

## Project Structure

```
scalepilot/
├── cmd/
│   ├── operator/main.go               # Operator entry point — wires DI, starts manager
│   └── scalepilot/                    # Cobra CLI binary
│       ├── main.go
│       └── cmd/
│           ├── root.go                # Root command + viper config
│           ├── status.go              # scalepilot status
│           ├── simulate.go            # scalepilot simulate
│           ├── budget.go              # scalepilot budget status
│           ├── clusters.go            # scalepilot clusters list
│           ├── validate.go            # scalepilot validate
│           ├── install.go             # scalepilot install
│           └── version.go             # scalepilot version
├── api/v1alpha1/                      # CRD type definitions (kubebuilder markers)
│   ├── forecastpolicy_types.go
│   ├── federatedscaledobject_types.go
│   ├── scalingbudget_types.go
│   ├── clusterscaleprofile_types.go
│   ├── groupversion_info.go
│   ├── doc.go
│   └── zz_generated.deepcopy.go       # auto-generated
├── internal/controller/               # One reconciler per CRD
│   ├── forecastpolicy_controller.go
│   ├── federatedscaledobject_controller.go
│   ├── scalingbudget_controller.go
│   └── clusterscaleprofile_controller.go
├── pkg/
│   ├── forecast/                      # Forecasting engines
│   │   ├── forecaster.go              # Forecaster interface + types
│   │   ├── arima.go                   # ARIMA(p,d,q) with Yule-Walker estimation
│   │   ├── holtwinters.go             # Triple exponential smoothing
│   │   ├── arima_test.go
│   │   └── holtwinters_test.go
│   ├── prometheus/                    # Prometheus metric client
│   │   ├── client.go                  # MetricQuerier interface + implementation
│   │   └── helpers.go                 # API client construction, result parsing
│   ├── multicluster/                  # Multi-cluster client registry
│   │   ├── registry.go                # Registry interface + sync.RWMutex impl
│   │   ├── healthcheck.go             # API server health checker
│   │   └── registry_test.go
│   ├── cloudcost/                     # Cloud billing adapters
│   │   ├── cost.go                    # CostQuerier interface + CachedQuerier
│   │   ├── aws.go                     # AWS Cost Explorer adapter
│   │   ├── gcp.go                     # GCP Billing adapter
│   │   ├── azure.go                   # Azure Cost Management adapter
│   │   └── cost_test.go
│   └── webhook/                       # Alert senders
│       ├── sender.go                  # Sender interface, Slack + PagerDuty impls
│       └── sender_test.go
├── config/
│   ├── crd/bases/                     # Generated CRD YAML manifests
│   ├── rbac/                          # Generated RBAC roles
│   └── samples/                       # Example CR manifests for all 4 CRDs
├── charts/scalepilot/                 # Helm chart
│   ├── Chart.yaml
│   ├── values.yaml
│   └── templates/
├── .github/workflows/ci.yml          # GitHub Actions: lint, test, build, ko
├── .golangci.yml                      # Linter config (24 linters)
├── .ko.yaml                           # ko image builder config
├── Makefile                           # Build, test, generate, deploy targets
└── go.mod                             # Dependencies
```

---

## Development

```bash
# Build both binaries
go build -o bin/manager ./cmd/operator/main.go
go build -o bin/scalepilot ./cmd/scalepilot/main.go

# Run unit tests with coverage
go test ./pkg/... -cover -timeout 60s

# Regenerate CRD manifests and deepcopy after changing types
make manifests generate

# Run linter
make lint

# Build container image with ko (no Dockerfile needed)
KO_DOCKER_REPO=ghcr.io/your-org ko build ./cmd/operator/

# Run the operator locally against your cluster
make install   # install CRDs
make run       # run operator
```

---

## How It Works Internally

### Forecast Model Lifecycle

1. **Training** runs in a background goroutine, never inside the reconcile loop. The reconciler detects that `lastTrainedAt` is stale (older than `retrainIntervalMinutes`) and fires `go trainModelAsync(...)`.

2. The goroutine queries Prometheus for `historyDuration` of data, feeds it to the ARIMA or Holt-Winters engine, and writes the resulting `ModelParams` (coefficients, RMSE, timestamp) as JSON into a ConfigMap named `scalepilot-model-<policy-name>`.

3. On the next reconcile cycle, the reconciler loads the ConfigMap, deserializes the params, and calls `forecaster.LoadParams()` — this is a fast O(1) operation, no matrix math.

4. The reconciler then calls `forecaster.Predict()` with a horizon of `leadTimeMinutes` and a 1-minute step to find the peak predicted value.

### Cluster Registry (sync.RWMutex pattern)

The `ClusterRegistry` is a `map[string]*ClusterEntry` protected by a `sync.RWMutex`. This is a common Go pattern for concurrent read-heavy, write-light maps:

- **Reads** (`Get`, `List`, `HealthyOverflow`) take a read lock — multiple goroutines can read simultaneously.
- **Writes** (`Register`, `Unregister`, health check updates) take a write lock — exclusive access, blocks all readers.
- `Get()` returns a **shallow copy** of the entry to prevent data races on mutable fields like `Healthy`.

### Cost Caching

Cloud billing APIs are rate-limited and slow (100-500ms per call). The `CachedQuerier` wraps any `CostQuerier` with an in-memory cache. Each namespace gets its own cache entry with a configurable TTL (default 5 minutes). The cache is `sync.RWMutex`-protected for concurrent access from multiple ScalingBudget reconcilers.

---

## Tech Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Language | Go 1.22+ | Operator and CLI |
| Scaffolding | kubebuilder v3 | CRD generation, RBAC markers |
| Controller framework | controller-runtime v0.17 | Reconciler loop, manager, client |
| Forecasting math | gonum v0.15 | Matrix operations, statistics |
| Metric ingestion | prometheus/client_golang | PromQL range and instant queries |
| CLI | cobra + viper | Subcommands, config file support |
| Image building | ko | Distroless container images, no Dockerfile |
| Testing | ginkgo v2 + gomega + go test | BDD and table-driven tests |
| Linting | golangci-lint (24 linters) | Code quality enforcement |
| CI | GitHub Actions | Lint, test, build, manifest verification, ko build |

---

## License

Apache License 2.0

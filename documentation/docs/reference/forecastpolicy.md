---
id: forecastpolicy
title: ForecastPolicy CRD Reference
sidebar_label: ForecastPolicy
---

# ForecastPolicy CRD Reference

**API Group:** `autoscaling.scalepilot.io/v1alpha1`  
**Kind:** `ForecastPolicy`  
**Scope:** Namespaced

`ForecastPolicy` attaches to a Deployment + HPA pair, reads Prometheus metric history, trains an ARIMA or Holt-Winters time-series model, and patches `HPA.minReplicas` ahead of predicted traffic spikes.

## Full Example

```yaml
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
    address: http://prometheus.monitoring.svc:9090
    query: 'sum(rate(http_requests_total{deployment="web-frontend"}[5m]))'
    historyDuration: "7d"
    stepInterval: "5m"
  algorithm: ARIMA
  arimaParams:
    p: 2
    d: 1
    q: 1
  leadTimeMinutes: 5
  retrainIntervalMinutes: 30
  maxReplicaCap: 50
  dryRun: false
  targetMetricValuePerReplica: "10.0"
  useUpperConfidenceBound: false
  scaleUpGuard:
    query: 'sum(rate(http_requests_total{status=~"5.."}[5m]))'
    maxMetricValue: "50"
```

## Spec Reference

### Top-Level Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `targetDeployment` | `TargetDeploymentRef` | Yes | — | The Deployment to forecast for |
| `targetHPA` | `TargetHPARef` | Yes | — | The HPA whose `minReplicas` gets patched |
| `metricSource` | `PrometheusMetricSource` | Yes | — | Prometheus query configuration |
| `algorithm` | `ARIMA` or `HoltWinters` | No | `ARIMA` | Forecasting algorithm to use |
| `arimaParams` | `ARIMAParams` | No | `p=2,d=1,q=1` | ARIMA hyperparameters (required when `algorithm: ARIMA`) |
| `holtWintersParams` | `HoltWintersParams` | No | — | Holt-Winters parameters (required when `algorithm: HoltWinters`) |
| `leadTimeMinutes` | `integer` | No | `5` | Minutes ahead of the spike to pre-scale (range: 3–10) |
| `retrainIntervalMinutes` | `integer` | No | `30` | How often (in minutes) to retrain the forecasting model |
| `maxReplicaCap` | `integer` | No | — | Maximum value ScalePilot will set for `minReplicas` |
| `dryRun` | `boolean` | No | `false` | Log predictions without applying HPA patches |
| `targetMetricValuePerReplica` | `string` | No | — | Decimal: metric load each replica handles; `replicas = ceil(peak / value)` |
| `useUpperConfidenceBound` | `boolean` | No | `false` | Use the 95% upper confidence bound instead of point forecast |
| `scaleUpGuard` | `ScaleUpGuard` | No | — | Optional Prometheus gate that can block HPA patches |

### `targetDeployment`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | Name of the Deployment in the same namespace |

### `targetHPA`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | Name of the `autoscaling/v2` HorizontalPodAutoscaler in the same namespace |

### `metricSource`

| Field | Type | Required | Validation | Description |
|-------|------|----------|-----------|-------------|
| `address` | `string` | Yes | MinLength=1 | Prometheus server URL (e.g. `http://prometheus:9090`) |
| `query` | `string` | Yes | MinLength=1 | PromQL expression returning a scalar or single-element vector |
| `historyDuration` | `string` | Yes | Pattern: `^\d+(s\|m\|h\|d)$` | How far back to fetch training data (e.g. `"7d"`, `"24h"`) |
| `stepInterval` | `string` | No | Pattern: `^\d+(s\|m\|h\|d)$` | Range query resolution step (default: `"5m"`) |

### `arimaParams`

Required when `algorithm: ARIMA`. All fields have CRD-level validation.

| Field | Type | Required | Range | Description |
|-------|------|----------|-------|-------------|
| `p` | `integer` | Yes | 0–10 | Autoregressive order — how many lagged values to use |
| `d` | `integer` | Yes | 0–3 | Differencing order — how many times to difference the series |
| `q` | `integer` | Yes | 0–10 | Moving-average order — how many lagged residuals to use |

### `holtWintersParams`

Required when `algorithm: HoltWinters`.

| Field | Type | Required | Validation | Description |
|-------|------|----------|-----------|-------------|
| `alpha` | `string` | Yes | Decimal (0,1] | Level smoothing coefficient. Higher = faster adaptation |
| `beta` | `string` | Yes | Decimal (0,1] | Trend smoothing coefficient. Higher = faster trend adaptation |
| `gamma` | `string` | Yes | Decimal (0,1] | Seasonal smoothing coefficient. Higher = faster seasonal update |
| `seasonalPeriods` | `integer` | Yes | ≥2 | Data points per seasonal cycle (e.g. 24 for daily cycle with hourly data) |

### `scaleUpGuard`

Optional Prometheus instant-query gate. Scale-up is blocked when the query result is **strictly greater than** `maxMetricValue`.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `address` | `string` | No | Prometheus URL; defaults to `metricSource.address` when empty |
| `query` | `string` | Yes | Instant PromQL query (evaluated as instant, not range) |
| `maxMetricValue` | `string` | Yes | Decimal threshold; scale-up blocked when instant value > this |

## Status Reference

### `status` Fields

| Field | Type | Description |
|-------|------|-------------|
| `lastTrainedAt` | `metav1.Time` | Timestamp of the most recent successful model training |
| `currentPrediction` | `string` | Latest raw forecasted metric value (decimal string) |
| `predictedMinReplicas` | `int32` | Computed target `minReplicas` from the forecast |
| `activeMinReplicas` | `int32` | Current `minReplicas` value on the target HPA |
| `modelConfigMap` | `string` | Name of the ConfigMap storing serialized model parameters |
| `conditions` | `[]metav1.Condition` | Kubernetes standard conditions (see below) |

### `status.conditions`

| Type | Status | Reason | Description |
|------|--------|--------|-------------|
| `ModelReady` | `True` | `Trained` | Model was successfully trained |
| `ModelReady` | `False` | `TrainingFailed` | Model training failed (check logs for details) |
| `ModelReady` | `False` | `InsufficientData` | Prometheus returned too few data points |
| `PatchApplied` | `True` | `Applied` | HPA `minReplicas` was successfully patched |
| `PatchApplied` | `False` | `DryRun` | Patch was skipped because `dryRun: true` |
| `PatchApplied` | `False` | `NoIncrease` | Prediction ≤ current HPA `minReplicas`; no patch needed |
| `PatchApplied` | `False` | `ScaleUpGuardBlocked` | ScaleUpGuard instant query exceeded `maxMetricValue` |
| `PatchApplied` | `False` | `ProfileBlocked` | ClusterScaleProfile has an active blackout window |
| `Error` | `True` | `PrometheusError` | Prometheus query returned an error |

## kubectl Print Columns

```bash
kubectl get forecastpolicies -n production
# NAME                    ALGORITHM    DEPLOYMENT     PREDICTED  ACTIVE  AGE
# web-frontend-forecast   ARIMA        web-frontend   6          4       2h
```

| Column | JSON Path | Description |
|--------|----------|-------------|
| `ALGORITHM` | `.spec.algorithm` | ARIMA or HoltWinters |
| `DEPLOYMENT` | `.spec.targetDeployment.name` | Target deployment name |
| `PREDICTED` | `.status.predictedMinReplicas` | Forecasted minReplicas |
| `ACTIVE` | `.status.activeMinReplicas` | Current HPA minReplicas |
| `AGE` | `.metadata.creationTimestamp` | Resource age |

## Prometheus Metrics Exposed

| Metric Name | Labels | Description |
|-------------|--------|-------------|
| `scalepilot_forecastpolicy_training_total` | `result: success\|failure` | Count of model training attempts |
| `scalepilot_forecastpolicy_hpa_minreplicas_patch_total` | `result: applied\|skipped_dry_run\|skipped_profile\|skipped_guard` | Count of HPA patch outcomes |

## Validation Rules

The following cross-field validations are enforced by the admission webhook (not just CRD OpenAPI):

- `algorithm: ARIMA` requires `arimaParams` to be set
- `algorithm: HoltWinters` requires `holtWintersParams` to be set
- `leadTimeMinutes` must be between 3 and 10 (inclusive)
- `targetMetricValuePerReplica`, when set, must parse as a positive float64

## Related

- **[Predictive Scaling Feature Guide](../features/predictive-scaling)**
- **[ARIMA Algorithm](../algorithms/arima)**
- **[Holt-Winters Algorithm](../algorithms/holt-winters)**
- **[CLI: scalepilot simulate](../cli/reference#scalepilot-simulate)**

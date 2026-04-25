---
id: predictive-scaling
title: Predictive Scaling
sidebar_label: Predictive Scaling
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Predictive Scaling with ForecastPolicy

`ForecastPolicy` is the core ScalePilot resource. It attaches to a Deployment + HPA pair, reads Prometheus metric history, trains a time-series forecasting model, and patches `HPA.minReplicas` ahead of predicted spikes.

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│ Every retrainIntervalMinutes (default: 30m):                │
│   1. Background goroutine queries Prometheus history        │
│   2. Trains ARIMA(p,d,q) or HoltWinters model              │
│   3. Serializes model params (coefficients + RMSE) to       │
│      ConfigMap named "scalepilot-model-<policy-name>"       │
│                                                             │
│ Every reconcile cycle (60s):                                │
│   1. Loads cached model from ConfigMap (fast, no math)      │
│   2. Predicts metric value for next leadTimeMinutes         │
│   3. Takes peak over the lead-time window                   │
│   4. Converts peak to replica count                         │
│   5. If predicted > current HPA minReplicas → patches HPA   │
│   6. Updates Status: currentPrediction, predictedMinReplicas│
│      activeMinReplicas, conditions                          │
└─────────────────────────────────────────────────────────────┘
```

## Choosing an Algorithm

| Metric Type | Recommended Algorithm | Reason |
|------------|----------------------|--------|
| HTTP request rate with daily/weekly seasonality | **Holt-Winters** | Captures repeating seasonal patterns |
| CPU utilization with gradual trends | **ARIMA** | Handles non-stationary trend data |
| Queue depth (Kafka lag, SQS depth) | **ARIMA** | Works well for non-seasonal, trending metrics |
| Stationary metrics with no clear seasonality | **ARIMA(p=1,d=0,q=0)** | Simple AR model |
| Clear 24h or 168h (weekly) traffic cycles | **Holt-Winters** | Designed for seasonal data |

:::tip Rule of Thumb
If you plot 7 days of your metric and see a repeating daily or weekly pattern, use **Holt-Winters** with `seasonalPeriods` matching your data granularity (24 for hourly data with a daily cycle, 168 for hourly data with a weekly cycle).

If the metric is noisier or trend-based without clear seasonality, start with **ARIMA(2,1,1)** and tune using `scalepilot simulate`.
:::

## Complete Example

<Tabs>
<TabItem value="arima" label="ARIMA Example">

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ForecastPolicy
metadata:
  name: web-frontend-forecast
  namespace: production
spec:
  # Target workload
  targetDeployment:
    name: web-frontend
  targetHPA:
    name: web-frontend-hpa

  # Prometheus data source
  metricSource:
    address: http://prometheus.monitoring.svc:9090
    query: 'sum(rate(http_requests_total{deployment="web-frontend"}[5m]))'
    historyDuration: "7d"     # train on 7 days of history
    stepInterval: "5m"        # 5-minute resolution (288 points/day)

  # ARIMA model configuration
  algorithm: ARIMA
  arimaParams:
    p: 2    # autoregressive order: use 2 past values
    d: 1    # differencing order: first-difference to remove trend
    q: 1    # moving-average order: account for 1 lag of residuals

  # Timing
  leadTimeMinutes: 5          # pre-scale 5 minutes ahead of the spike
  retrainIntervalMinutes: 30  # retrain model every 30 minutes

  # Safety guards
  maxReplicaCap: 50           # never set minReplicas above 50
  dryRun: false               # apply HPA patches
  useUpperConfidenceBound: false  # use point forecast (not upper 95% CI)

  # Optional: how much metric load each replica handles
  # replicas = ceil(peakForecast / targetMetricValuePerReplica)
  targetMetricValuePerReplica: "10.0"

  # Optional: SLO gate - skip pre-scaling if error rate is too high
  scaleUpGuard:
    query: 'sum(rate(http_requests_total{status=~"5.."}[5m]))'
    maxMetricValue: "50"   # block if 5xx rate > 50 req/s
```

</TabItem>
<TabItem value="holt-winters" label="Holt-Winters Example">

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ForecastPolicy
metadata:
  name: api-gateway-forecast
  namespace: production
spec:
  targetDeployment:
    name: api-gateway
  targetHPA:
    name: api-gateway-hpa

  metricSource:
    address: http://prometheus.monitoring.svc:9090
    query: 'sum(rate(http_requests_total{service="api-gateway"}[5m]))'
    historyDuration: "14d"    # 2 weeks for reliable seasonal initialization
    stepInterval: "1h"        # hourly data, 24 points per day

  algorithm: HoltWinters
  holtWintersParams:
    alpha: "0.3"              # level smoothing: moderate adaptation
    beta: "0.1"               # trend smoothing: slow trend adaptation
    gamma: "0.2"              # seasonal smoothing: moderate seasonal update
    seasonalPeriods: 24       # 24 hourly data points = 1 day cycle

  leadTimeMinutes: 8
  retrainIntervalMinutes: 60  # retrain hourly (model is stable)
  maxReplicaCap: 30
  dryRun: false
  useUpperConfidenceBound: true  # use upper 95% CI for conservative scaling
```

</TabItem>
<TabItem value="conservative" label="Conservative Production">

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ForecastPolicy
metadata:
  name: payment-service-forecast
  namespace: production
spec:
  targetDeployment:
    name: payment-service
  targetHPA:
    name: payment-service-hpa

  metricSource:
    address: http://prometheus.monitoring.svc:9090
    query: 'sum(rate(payment_requests_total[5m]))'
    historyDuration: "7d"
    stepInterval: "5m"

  algorithm: ARIMA
  arimaParams:
    p: 3
    d: 1
    q: 2

  leadTimeMinutes: 10         # max lead time: 10 minutes ahead
  retrainIntervalMinutes: 15  # retrain every 15 minutes for fresher model
  maxReplicaCap: 40
  dryRun: false
  useUpperConfidenceBound: true  # conservative: always use upper bound

  # Each replica handles 5 payment requests/s
  targetMetricValuePerReplica: "5.0"

  # Only pre-scale if error rate is healthy
  scaleUpGuard:
    query: 'sum(rate(payment_errors_total[5m])) / sum(rate(payment_requests_total[5m]))'
    maxMetricValue: "0.01"    # block pre-scaling if error rate > 1%
```

</TabItem>
</Tabs>

## Replica Calculation

ScalePilot converts a metric forecast to a replica count in two ways:

### Without `targetMetricValuePerReplica`

```
predictedReplicas = ceil(peakForecast)
```

Only valid if your PromQL query already returns a "replica-equivalent" value (e.g., a query that directly returns desired replica count).

### With `targetMetricValuePerReplica`

```
predictedReplicas = ceil(peakForecast / targetMetricValuePerReplica)
```

This mirrors HPA's `averageValue` logic. If your query returns total request rate (e.g., 100 req/s) and each replica handles 10 req/s, set `targetMetricValuePerReplica: "10.0"` to get `ceil(100 / 10) = 10` replicas.

### Upper Confidence Bound

When `useUpperConfidenceBound: true`, ScalePilot uses the 95% upper confidence interval instead of the point forecast for the peak calculation. This is a more conservative approach - it pre-scales more aggressively but provides a buffer against forecast error.

## ScaleUpGuard

`ScaleUpGuard` is a Prometheus instant-query gate that runs before each HPA patch. If the instant query result is **strictly greater** than `maxMetricValue`, the patch is skipped.

```yaml
scaleUpGuard:
  address: http://prometheus.monitoring.svc:9090  # optional, defaults to metricSource.address
  query: 'sum(rate(http_requests_total{status=~"5.."}[5m]))'
  maxMetricValue: "50"
```

Use cases:
- Skip pre-scaling when SLO error budget is burning too fast
- Skip pre-scaling during a known incident
- Prevent cascade scaling when the system is already under stress

When a guard blocks a patch, the `PatchApplied` condition is set to `False` with reason `ScaleUpGuardBlocked`, and the metric `scalepilot_forecastpolicy_hpa_minreplicas_patch_total{result="skipped_guard"}` increments.

## Status Fields

```bash
kubectl describe forecastpolicy web-frontend-forecast -n production
```

```yaml
Status:
  Last Trained At:        2026-04-18T14:30:00Z
  Current Prediction:     "52.10"
  Predicted Min Replicas: 6
  Active Min Replicas:    6
  Model Config Map:       scalepilot-model-web-frontend-forecast
  Conditions:
    - Type: ModelReady
      Status: "True"
      Reason: Trained
      Message: ARIMA(2,1,1) trained on 2016 points, RMSE=2.34
    - Type: PatchApplied
      Status: "True"
      Reason: Applied
      Message: HPA minReplicas raised from 4 to 6
```

## Operator Metrics

ScalePilot exposes Prometheus metrics on port 8080 (configurable via Helm `metrics.port`):

| Metric | Labels | Description |
|--------|--------|-------------|
| `scalepilot_forecastpolicy_training_total` | `result: success\|failure` | Model training outcomes |
| `scalepilot_forecastpolicy_hpa_minreplicas_patch_total` | `result: applied\|skipped_dry_run\|skipped_profile\|skipped_guard` | HPA patch outcomes |

## Tuning Tips

### Too many false positives (pre-scaling when not needed)?

- Increase `leadTimeMinutes` - the forecast window is shorter and more accurate closer to the event
- Try `useUpperConfidenceBound: false` to use the point estimate rather than the upper bound
- Add a `scaleUpGuard` to gate on actual observed load

### Model RMSE is too high?

- Use `scalepilot simulate` to run offline comparisons with different `p`, `d`, `q` values
- Try extending `historyDuration` to give the model more data
- If the metric has strong seasonality, switch to HoltWinters

### HPA never gets patched even when load is high?

- Verify `dryRun: false`
- Check `ClusterScaleProfile` is not in a blackout window (`kubectl get clusterscaleprofile default`)
- Ensure `maxReplicaCap` is not too low
- Check if current HPA `minReplicas` already matches or exceeds the prediction

## Related Resources

- **[ARIMA Algorithm](../algorithms/arima)** - How the ARIMA engine works internally
- **[Holt-Winters Algorithm](../algorithms/holt-winters)** - How triple exponential smoothing works
- **[ForecastPolicy CRD Reference](../reference/forecastpolicy)** - Full spec and status fields
- **[CLI: simulate](../cli/reference#scalepilot-simulate)** - Test your model offline

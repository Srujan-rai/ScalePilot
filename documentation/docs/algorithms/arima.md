---
id: arima
title: ARIMA Algorithm
sidebar_label: ARIMA
---

# ARIMA Forecasting Algorithm

ScalePilot implements an **ARIMA(p,d,q)** model — *AutoRegressive Integrated Moving Average* — for time-series forecasting. This page explains what ARIMA is, how ScalePilot's implementation works, how to choose parameters, and when to prefer ARIMA over Holt-Winters.

## What is ARIMA?

ARIMA is a class of statistical models for analyzing and forecasting time-series data. It combines three components:

| Component | Letter | Meaning |
|-----------|--------|---------|
| **AutoRegressive (AR)** | `p` | Uses past *values* to predict the current value |
| **Integrated (I)** | `d` | Differencing to remove trend and make the series stationary |
| **Moving Average (MA)** | `q` | Uses past *forecast errors* to correct the prediction |

The model is written as ARIMA(p,d,q) where p, d, and q are non-negative integers.

## How ScalePilot's ARIMA Works

ScalePilot implements ARIMA from scratch (no external time-series library dependency) using classical estimation methods:

### 1. Differencing (I component)

The series is differenced `d` times to remove trend and achieve stationarity:

```
d=0: use raw series          [y₁, y₂, y₃, y₄]
d=1: first differences       [y₂-y₁, y₃-y₂, y₄-y₃]
d=2: second differences      [(y₃-y₂)-(y₂-y₁), ...]
```

The last `d` values of the original series are saved to undo differencing during prediction (undifferencing/integration step).

### 2. AR Coefficient Estimation (Yule-Walker)

After differencing, the AR coefficients are estimated using the **Yule-Walker equations** solved via the **Levinson-Durbin recursion**. This is an O(n·p) algorithm.

The Yule-Walker system: given autocorrelation values `r[0..p]`, solve:

```
r[0]   r[1]   ... r[p-1]     φ[1]     r[1]
r[1]   r[0]   ... r[p-2]  ×  φ[2]  =  r[2]
...                         ...       ...
r[p-1] r[p-2] ... r[0]       φ[p]     r[p]
```

The Levinson-Durbin recursion solves this system without matrix inversion, making it numerically stable and efficient.

### 3. Residual Computation and MA Estimation

Residuals (one-step-ahead prediction errors) are computed from the AR fit. MA coefficients are estimated from the **autocorrelation of the residuals** — an approximation that works well in practice for moderate `q` values.

### 4. Prediction

For each forecast step `i`:

```
ŷ[i] = mean + Σ(j=1..p) φ[j] * (y[i-j] - mean)
              + Σ(j=1..q) θ[j] * ε[i-j]
```

Where:
- `φ[j]` = AR coefficients
- `θ[j]` = MA coefficients  
- `ε[i-j]` = past residuals (set to 0 for future steps)
- `mean` = mean of the differenced series

### 5. Confidence Intervals

The 95% confidence interval grows with the forecast horizon, using the root mean squared error (RMSE) of training residuals:

```
Standard Error at step i = RMSE × √i
Upper 95% CI = ŷ[i] + 1.96 × SE[i]
Lower 95% CI = ŷ[i] - 1.96 × SE[i]
```

### 6. Undifferencing

Predictions are integrated back to the original scale by reversing the differencing steps, using the saved last values from the training data.

### 7. Model Caching

Trained model parameters (AR/MA coefficients, mean, RMSE, last differenced values) are serialized to JSON and stored in a Kubernetes ConfigMap:

```
ConfigMap name: scalepilot-model-<policy-name>
```

On each reconcile cycle, the controller loads the ConfigMap and calls `LoadParams()` — an O(1) operation that avoids re-training.

## Choosing ARIMA Parameters

### Understanding p, d, q

| Parameter | Intuition | Low Value | High Value |
|-----------|-----------|-----------|-----------|
| `p` | How many past values predict the next | Only the most recent period matters | Many past values matter (long autocorrelation) |
| `d` | How many times to difference | Metric is already stationary | Metric has strong trend/drift |
| `q` | How many past errors to include | Errors are uncorrelated | Errors have short-term autocorrelation |

### Starting Points

| Metric Behavior | Suggested ARIMA(p,d,q) |
|----------------|------------------------|
| CPU with slight upward trend | `(2, 1, 1)` |
| HTTP request rate (stationary) | `(2, 0, 2)` |
| Queue depth with drift | `(1, 1, 0)` |
| Very noisy metric | `(1, 0, 0)` — simple AR |
| Step-change metric | `(1, 2, 1)` |

### Using `scalepilot simulate` to Compare

```bash
# Test different parameter combinations
scalepilot simulate my-forecast --horizon 1h --step 5m
# Training ARIMA(2,1,1) on 2016 data points...
# Model trained (RMSE: 2.3412)

# Change arimaParams in the ForecastPolicy and re-simulate
# Lower RMSE = better fit
```

### Parameter Constraints in ScalePilot

The CRD enforces these validation ranges:

| Parameter | Minimum | Maximum |
|-----------|---------|---------|
| `p` | 0 | 10 |
| `d` | 0 | 3 |
| `q` | 0 | 10 |

Minimum training data requirement: `p + d + q + 10` data points.

## When to Use ARIMA

**Choose ARIMA when:**

- Your metric has **no clear seasonal pattern** (it doesn't repeat every 24 hours or 7 days)
- The metric has a **trend** (consistently rising or falling) — use `d=1`
- The metric is **stationary** after differencing — use `d=0`
- Your data window is shorter (a few days) and Holt-Winters can't initialize its seasonal component reliably
- You're forecasting **queue depths**, **active connections**, or **non-periodic CPU** usage

**Choose Holt-Winters instead when:**

- You have **clear daily or weekly traffic cycles** (web traffic, retail, batch jobs)
- You have at least 2 full seasonal cycles worth of data (e.g. 2 weeks for a weekly cycle)
- The seasonal amplitude is roughly constant over time

## ARIMA vs Holt-Winters Summary

| Property | ARIMA | Holt-Winters |
|----------|-------|-------------|
| Handles trend | ✓ (via differencing) | ✓ (via β trend coefficient) |
| Handles seasonality | ✗ (SARIMA would, not implemented) | ✓ (designed for it) |
| Minimum data needed | `p+d+q+10` points | `2 × seasonalPeriods` points |
| Best for | Stationary/trending metrics | Periodic traffic patterns |
| Parameters | 3 integers (p, d, q) | 4 floats + season length |
| Interpretability | Moderate | High |

## Practical Example

This ForecastPolicy is for a CPU-heavy backend service with gradual load growth (trend) but no fixed daily pattern:

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ForecastPolicy
metadata:
  name: worker-service-forecast
  namespace: processing
spec:
  targetDeployment:
    name: worker-service
  targetHPA:
    name: worker-service-hpa
  metricSource:
    address: http://prometheus.monitoring.svc:9090
    query: 'avg(rate(container_cpu_usage_seconds_total{pod=~"worker-.*"}[5m]))'
    historyDuration: "3d"       # 3 days of 5-min data = 864 points (ample for ARIMA)
    stepInterval: "5m"
  algorithm: ARIMA
  arimaParams:
    p: 2    # use last 2 time steps (10 minutes)
    d: 1    # difference once to remove CPU growth trend
    q: 1    # account for 1 lag of residuals
  leadTimeMinutes: 5
  retrainIntervalMinutes: 30
  maxReplicaCap: 20
  targetMetricValuePerReplica: "0.5"   # each replica handles 0.5 CPU
```

## Related Resources

- **[Holt-Winters Algorithm](./holt-winters)** — The alternative seasonal forecaster
- **[ForecastPolicy CRD Reference](../reference/forecastpolicy)** — Full spec
- **[Predictive Scaling Feature Guide](../features/predictive-scaling)** — When and how to use ForecastPolicy
- **Source code:** `pkg/forecast/arima.go` — Yule-Walker estimation, Levinson-Durbin recursion, undifferencing

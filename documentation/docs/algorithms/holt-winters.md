---
id: holt-winters
title: Holt-Winters Algorithm
sidebar_label: Holt-Winters
---

# Holt-Winters Algorithm

ScalePilot implements **additive Holt-Winters triple exponential smoothing** for forecasting metrics that exhibit seasonal patterns - daily traffic cycles, weekly batch job rhythms, or any repeating periodic behavior.

## What is Holt-Winters?

Holt-Winters (also called *triple exponential smoothing*) decomposes a time series into three components:

| Component | Symbol | Meaning |
|-----------|--------|---------|
| **Level** | `L` | The smoothed base value (local mean) |
| **Trend** | `T` | The direction and speed of change |
| **Seasonal** | `S` | The repeating periodic deviation from level+trend |

The **additive** variant (implemented in ScalePilot) assumes the seasonal amplitude stays constant regardless of the level:

```
Forecast(t + h) = Level + h × Trend + Seasonal[t mod m]
```

This is appropriate when your traffic peaks are roughly the same height whether overall load is high or low. If seasonal amplitude grows proportionally with the level, a multiplicative model would be needed (not currently implemented).

## How ScalePilot's Holt-Winters Works

### Initialization

Given a series `y[0..n-1]` with seasonal period `m` (data points per season):

```
Level₀  = mean(y[0..m-1])                      ← mean of first season
Trend₀  = (1/m) × Σ (y[m+i] - y[i]) / m       ← average slope between first two seasons
Seasonal[i] = y[i] - Level₀  for i = 0..m-1   ← seasonal deviations
```

At least `2m` data points are required to initialize the model (2 full seasonal cycles).

### Recursive Update Equations

For each observation `y[t]` from `t = m` onward:

```
Level_t   = α × (y[t] - Seasonal[t mod m]) + (1 - α) × (Level_{t-1} + Trend_{t-1})
Trend_t   = β × (Level_t - Level_{t-1})    + (1 - β) × Trend_{t-1}
Seasonal_t = γ × (y[t] - Level_t)          + (1 - γ) × Seasonal[t mod m]
```

Where:
- `α` ∈ (0,1] - level smoothing: how fast the baseline adapts to new data
- `β` ∈ (0,1] - trend smoothing: how fast the trend direction updates
- `γ` ∈ (0,1] - seasonal smoothing: how fast seasonal patterns update

### Forecasting

To forecast `h` steps ahead from the end of training:

```
ŷ[t + h] = Level_final + h × Trend_final + Seasonal[(n + h) mod m]
```

Where `n` is the length of the training series.

### Confidence Intervals

Same as ARIMA - the 95% CI widens with the forecast horizon:

```
SE[h] = RMSE × √h
Upper CI = ŷ[t+h] + 1.96 × SE[h]
Lower CI = ŷ[t+h] - 1.96 × SE[h]
```

### Model Caching

Trained parameters (`level`, `trend`, `seasonal[0..m-1]`, `rmse`) are serialized to JSON and stored in a Kubernetes ConfigMap. Subsequent reconcile cycles load the cached model with `LoadParams()` without retraining.

## Choosing Smoothing Coefficients

### What Each Coefficient Controls

| Coefficient | Low (near 0) | High (near 1) |
|-------------|-------------|--------------|
| **α (alpha)** - level | Slow adaptation, very stable baseline | Fast adaptation, responsive but noisy |
| **β (beta)** - trend | Ignores short-term trend changes | Aggressively tracks new trend directions |
| **γ (gamma)** - seasonal | Seasonal pattern rarely updates | Seasonal pattern updates quickly each cycle |

### Recommended Starting Values

| Traffic Pattern | α | β | γ | Notes |
|----------------|---|---|---|-------|
| Stable daily web traffic | 0.3 | 0.1 | 0.2 | Moderate adaptation, stable seasonality |
| Rapidly growing service | 0.5 | 0.2 | 0.1 | Fast level adaptation, slow seasonal update |
| Very regular batch jobs | 0.2 | 0.05 | 0.3 | Low level/trend adaptation, high seasonal fidelity |
| Noisy production traffic | 0.2 | 0.1 | 0.15 | Conservative across the board |

### Choosing `seasonalPeriods`

`seasonalPeriods` is the number of data points in one full seasonal cycle:

| Seasonal Cycle | `stepInterval` | `seasonalPeriods` |
|---------------|---------------|-------------------|
| Daily (24h) | 1h | 24 |
| Daily (24h) | 5m | 288 |
| Weekly (7d) | 1h | 168 |
| Weekly (7d) | 5m | 2016 |
| 12-hour shift | 1h | 12 |

:::warning Minimum Data Requirement
You need at least **2 × seasonalPeriods** data points to initialize the model. For a `seasonalPeriods=168` (weekly, hourly) model, that's 336 hours = 2 weeks of data. Set `historyDuration: "14d"` or longer.
:::

## When to Use Holt-Winters

**Choose Holt-Winters when:**

- Your metric shows **clear repeating patterns** - morning peak, evening dip, weekend drop
- You have at least **2 full seasonal cycles** of historical data
- Traffic peaks are **roughly the same height** across different absolute load levels (additive seasonality)
- You're forecasting **web request rates**, **API throughput**, **user session counts**

**Choose ARIMA instead when:**

- No clear seasonal pattern exists
- Shorter history (a few days)
- The metric is non-periodic (queue depth, database connections)

## Practical Examples

### Daily Traffic Cycle (Hourly Data)

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
    historyDuration: "14d"    # 2 full weeks for reliable initialization
    stepInterval: "1h"        # hourly data
  algorithm: HoltWinters
  holtWintersParams:
    alpha: "0.3"              # moderate level smoothing
    beta: "0.1"               # slow trend tracking
    gamma: "0.2"              # seasonal patterns update moderately
    seasonalPeriods: 24       # 24 hours = 1 daily cycle
  leadTimeMinutes: 8
  retrainIntervalMinutes: 60
  maxReplicaCap: 30
  useUpperConfidenceBound: true
```

### Weekly Traffic Cycle (Hourly Data)

```yaml
spec:
  metricSource:
    historyDuration: "21d"     # 3 full weeks of history
    stepInterval: "1h"
  algorithm: HoltWinters
  holtWintersParams:
    alpha: "0.25"
    beta: "0.05"
    gamma: "0.15"
    seasonalPeriods: 168       # 7 days × 24 hours = 168-hour weekly cycle
  retrainIntervalMinutes: 60
```

### High-Resolution Forecast (5-Minute Data, Daily Cycle)

```yaml
spec:
  metricSource:
    historyDuration: "7d"      # 7 days of 5-min data = 2016 points per day → barely 2 seasons
    stepInterval: "5m"
  algorithm: HoltWinters
  holtWintersParams:
    alpha: "0.3"
    beta: "0.1"
    gamma: "0.2"
    seasonalPeriods: 288       # 24h × 12 (5-min intervals/hr) = 288 points per day
```

:::warning High seasonalPeriods and Memory
A `seasonalPeriods` of 288 means 288 seasonal coefficients are stored in the ConfigMap. This is fine - each coefficient is a float64 (8 bytes), so 288 coefficients ≈ 2.3 KB. For `seasonalPeriods: 2016` (weekly at 5m), it's ~16 KB, well within ConfigMap limits.
:::

## Holt-Winters vs ARIMA Quick Reference

| | Holt-Winters | ARIMA |
|--|-------------|-------|
| Best for | Seasonal/periodic metrics | Trend-based/non-seasonal metrics |
| Parameters | `α, β, γ, seasonalPeriods` | `p, d, q` |
| Minimum data | `2 × seasonalPeriods` | `p + d + q + 10` |
| Seasonality | Built-in | Not supported (would need SARIMA) |
| Trend | Built-in | Via differencing (`d`) |
| Training speed | O(n) | O(n × p) + O(p²) Yule-Walker |
| Interpretability | High (three intuitive components) | Moderate |

## Related Resources

- **[ARIMA Algorithm](./arima)** - The alternative non-seasonal forecaster
- **[ForecastPolicy CRD Reference](../reference/forecastpolicy)** - Full spec
- **[Predictive Scaling Feature Guide](../features/predictive-scaling)** - When and how to use ForecastPolicy
- **Source code:** `pkg/forecast/holtwinters.go` - Triple exponential smoothing implementation

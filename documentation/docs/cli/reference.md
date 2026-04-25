---
id: reference
title: CLI Reference
sidebar_label: CLI Reference
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# CLI Reference

The `scalepilot` CLI provides commands for status inspection, dry-run simulation, budget monitoring, cluster management, and installation.

## Installation

```bash
# Build from source
git clone https://github.com/srujan-rai/scalepilot.git
cd scalepilot
go build -o bin/scalepilot ./cmd/scalepilot/main.go

# Move to PATH
sudo mv bin/scalepilot /usr/local/bin/scalepilot
```

## Global Flags

These flags are available on all commands:

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `$HOME/.scalepilot.yaml` | Path to config file |
| `--kubeconfig` | `$HOME/.kube/config` | Path to kubeconfig file |
| `--namespace` | `default` | Kubernetes namespace to operate in |

### Config File

All flags can be stored in `$HOME/.scalepilot.yaml`:

```yaml title="~/.scalepilot.yaml"
kubeconfig: /home/user/.kube/production.yaml
namespace: production
```

---

## `scalepilot status`

Displays a live table of all `ForecastPolicy` resources with their predictions vs. actual HPA values.

```bash
scalepilot status [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--namespace` | `default` | Namespace to list ForecastPolicies from (or use global flag) |

**Example:**

```bash
scalepilot status --namespace production
```

```
NAMESPACE    NAME                    ALGORITHM    DEPLOYMENT       PREDICTED  ACTIVE  LAST TRAINED         STATUS
production   web-frontend-forecast   ARIMA        web-frontend     12         8       2026-04-18 14:30     Ready
staging      api-gateway-forecast    HoltWinters  api-gateway      5          5       2026-04-18 14:25     Ready
production   payment-svc-forecast    ARIMA        payment-service  8          6       2026-04-18 14:28     ModelReady
```

Column descriptions:

| Column | Description |
|--------|-------------|
| `NAMESPACE` | Kubernetes namespace |
| `NAME` | ForecastPolicy name |
| `ALGORITHM` | ARIMA or HoltWinters |
| `DEPLOYMENT` | Target Deployment name |
| `PREDICTED` | Forecasted minReplicas value |
| `ACTIVE` | Current HPA minReplicas |
| `LAST TRAINED` | When the model was last retrained |
| `STATUS` | Summary of the latest condition |

---

## `scalepilot simulate`

Dry-runs a forecast against live Prometheus data to validate model accuracy before enabling a ForecastPolicy. Fetches historical data, trains the model, and prints predicted values with confidence intervals - without touching any HPA.

```bash
scalepilot simulate <forecast-policy-name> [flags]
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `forecast-policy-name` | Yes | Name of an existing ForecastPolicy resource |

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--horizon` | `1h` | Forecast window (e.g. `30m`, `1h`, `2h`) |
| `--step` | `5m` | Time step between forecast points (e.g. `1m`, `5m`, `15m`) |
| `--namespace` | `default` | Namespace containing the ForecastPolicy |

**Example:**

```bash
scalepilot simulate web-frontend-forecast \
  --namespace production \
  --horizon 1h \
  --step 5m
```

**Output:**

```
Training ARIMA(2,1,1) on 2016 data points...
Model trained (RMSE: 2.3412)

TIME      PREDICTED  LOWER_95  UPPER_95
14:35:00  45.23      38.12     52.34
14:40:00  48.91      39.45     58.37
14:45:00  52.10      40.78     63.42
14:50:00  49.77      38.12     61.42
14:55:00  47.33      35.90     58.76
15:00:00  46.12      33.45     58.79
15:05:00  44.89      31.12     58.66
...

Peak (point)=52.10  implied minReplicas=6 (targetMetricValuePerReplica="10.0")
```

**Interpretation:**
- `PREDICTED` - point forecast value
- `LOWER_95`, `UPPER_95` - 95% confidence interval bounds
- `Peak` - maximum predicted value over the forecast horizon
- `implied minReplicas` - replica count ScalePilot would set on the HPA

:::tip Tuning the Model
Run `scalepilot simulate` with different `arimaParams` values and compare RMSE. A lower RMSE indicates better fit to the historical data. Start with `p=2, d=1, q=1` and adjust based on your metric's autocorrelation structure.
:::

---

## `scalepilot budget status`

Shows the namespace spend vs. budget ceiling for all `ScalingBudget` resources.

```bash
scalepilot budget status [flags]
```

**Subcommands of `scalepilot budget`:**

| Subcommand | Description |
|-----------|-------------|
| `status` | Display budget utilization table |

**Example:**

```bash
scalepilot budget status
```

**Output:**

```
NAMESPACE    NAME                PROVIDER  CEILING   SPEND     UTILIZATION  BREACHED  ACTION  BLOCKED
production   production-budget   AWS       $150.00   $120.50   80%          No        Delay   0
staging      staging-budget      GCP       $50.00    $52.30    104%         YES       Block   3
development  dev-budget          Azure     $25.00    $18.75    75%          No        Delay   0
```

Column descriptions:

| Column | Description |
|--------|-------------|
| `NAMESPACE` | Target namespace being budgeted |
| `NAME` | ScalingBudget resource name |
| `PROVIDER` | Cloud billing provider |
| `CEILING` | Monthly cost ceiling in dollars |
| `SPEND` | Current month-to-date spend |
| `UTILIZATION` | Spend as % of ceiling |
| `BREACHED` | Whether the ceiling is currently exceeded |
| `ACTION` | Configured breach action |
| `BLOCKED` | Count of scale-up events blocked by breach enforcement |

---

## `scalepilot clusters list`

Lists all overflow clusters registered by `FederatedScaledObject` resources, along with their health status and current replica counts.

```bash
scalepilot clusters list [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--namespace` | `default` | Namespace to list FederatedScaledObjects from |

**Example:**

```bash
scalepilot clusters list --namespace production
```

**Output:**

```
FSO                          CLUSTER       ROLE      HEALTHY  REPLICAS  PRIORITY  LAST PROBE
order-processor-federation   us-east-1     primary   ✓        50        0         -
order-processor-federation   eu-west-1     overflow  ✓        20        1         14:32:15
order-processor-federation   ap-south-1    overflow  ✗        0         2         14:32:15
image-processor-federation   us-east-1     primary   ✓        10        0         -
image-processor-federation   us-west-2     overflow  ✓        0         1         14:32:10
```

Column descriptions:

| Column | Description |
|--------|-------------|
| `FSO` | FederatedScaledObject name |
| `CLUSTER` | Cluster name (from `ClusterRef.name`) |
| `ROLE` | `primary` or `overflow` |
| `HEALTHY` | Whether the cluster API server is reachable |
| `REPLICAS` | Current replica count on this cluster |
| `PRIORITY` | Spillover priority (lower = used first) |
| `LAST PROBE` | Time of the last health check |

---

## `scalepilot validate`

Lints ScalePilot CRD manifests before applying them. Catches misconfigurations that the API server would reject, such as specifying `algorithm: ARIMA` without `arimaParams`.

```bash
scalepilot validate <file...> [flags]
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `file...` | Yes | One or more YAML file paths or glob patterns |

**Example:**

```bash
scalepilot validate config/samples/*.yaml
```

**Output:**

```
OK   config/samples/autoscaling_v1alpha1_forecastpolicy.yaml
OK   config/samples/autoscaling_v1alpha1_clusterscaleprofile.yaml
OK   config/samples/autoscaling_v1alpha1_federatedscaledobject.yaml
FAIL config/samples/bad-policy.yaml: arimaParams required when algorithm is ARIMA
FAIL config/samples/bad-budget.yaml: ceilingMillidollars must be >= 1
```

Exit code is `0` if all files pass, non-zero if any fail. Suitable for use in CI pipelines:

```bash
scalepilot validate config/manifests/*.yaml || exit 1
```

**Validations performed:**

| Check | Description |
|-------|-------------|
| Algorithm/params consistency | ARIMA requires `arimaParams`; HoltWinters requires `holtWintersParams` |
| leadTimeMinutes range | Must be 3–10 |
| historyDuration format | Must match `\d+(s\|m\|h\|d)` |
| ceilingMillidollars | Must be ≥ 1 |
| warningThresholdPercent | Must be 1–100 |
| provider | Must be `AWS`, `GCP`, or `Azure` |
| seasonalPeriods | Must be ≥ 2 |
| Cron syntax | Validates blackout window start/end cron expressions |

---

## `scalepilot install`

Renders and applies the ScalePilot Helm chart to the current cluster.

```bash
scalepilot install [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--release-name` | `scalepilot` | Helm release name |
| `--target-namespace` | `scalepilot-system` | Namespace to install into |
| `--values` | - | Path to a Helm values YAML file |
| `--create-namespace` | `true` | Create the namespace if it doesn't exist |
| `--dry-run` | `false` | Render the Helm chart without applying |

**Example:**

```bash
scalepilot install \
  --release-name scalepilot \
  --target-namespace scalepilot-system \
  --values my-values.yaml
```

---

## `scalepilot version`

Prints version information for the CLI binary.

```bash
scalepilot version
```

**Output:**

```
ScalePilot CLI
  Version:            v0.1.0
  Git Commit:         a1b2c3d4e5f6
  Build Date:         2026-04-18T14:00:00Z
  Go Version:         go1.22.3
  Platform:           linux/amd64
  Controller Runtime: v0.17.3
```

---

## Shell Completion

Generate shell completion scripts:

<Tabs>
<TabItem value="bash" label="Bash">

```bash
scalepilot completion bash > /etc/bash_completion.d/scalepilot
source /etc/bash_completion.d/scalepilot
```

</TabItem>
<TabItem value="zsh" label="Zsh">

```zsh
scalepilot completion zsh > "${fpath[1]}/_scalepilot"
# Restart your shell or run: exec zsh
```

</TabItem>
<TabItem value="fish" label="Fish">

```fish
scalepilot completion fish > ~/.config/fish/completions/scalepilot.fish
```

</TabItem>
</Tabs>

## Environment Variables

| Variable | Description |
|----------|-------------|
| `KUBECONFIG` | Path to kubeconfig (overridden by `--kubeconfig` flag) |
| `SCALEPILOT_NAMESPACE` | Default namespace (overridden by `--namespace` flag) |

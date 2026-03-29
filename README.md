# ScalePilot

Cloud-agnostic Kubernetes autoscaling operator with predictive scaling, multi-cluster workload federation, and FinOps budget controls — extending KEDA and HPA with forecast-driven decisions.

## Features

- **Predictive Scaling** — ARIMA and Holt-Winters forecasting models trained on Prometheus metric history, patching HPA `minReplicas` 3-10 minutes before predicted traffic spikes.
- **Multi-Cluster Federation** — Automatic workload spillover to overflow clusters when queue depth exceeds a threshold, with priority-based cluster selection and health monitoring.
- **FinOps Cost Budgets** — Namespace-scoped cost ceilings backed by AWS Cost Explorer, GCP Billing, or Azure Cost Management, with Downgrade/Delay/Block breach actions and Slack/PagerDuty alerts.
- **Cluster Scale Profiles** — Cluster-wide defaults including max surge percentages, cron-based blackout windows, and per-team RBAC overrides.

## Custom Resources

| CRD | Scope | Description |
|-----|-------|-------------|
| `ForecastPolicy` | Namespaced | Attaches to a Deployment, forecasts demand, patches HPA |
| `FederatedScaledObject` | Namespaced | Multi-cluster spillover based on metric thresholds |
| `ScalingBudget` | Namespaced | Namespace cost ceiling with breach actions |
| `ClusterScaleProfile` | Cluster | Cluster-wide scaling defaults and team overrides |

## Quick Start

```bash
# Install CRDs
make install

# Run the operator locally
make run

# Apply a sample ForecastPolicy
kubectl apply -f config/samples/autoscaling_v1alpha1_forecastpolicy.yaml
```

## CLI

```bash
scalepilot status          # Live table of predictions vs actual
scalepilot simulate        # Dry-run forecast against past data
scalepilot budget status   # Namespace spend vs ceiling
scalepilot clusters list   # Overflow clusters + health status
scalepilot validate        # Lint CRD manifests before apply
scalepilot install         # Render + apply Helm chart
scalepilot version         # Operator version + git SHA
```

## Building

```bash
# Build the operator
go build -o bin/manager ./cmd/operator/main.go

# Build the CLI
go build -o bin/scalepilot ./cmd/scalepilot/main.go

# Build container image with ko (no Dockerfile needed)
ko build ./cmd/operator/

# Run tests
make test
```

## Project Structure

```
scalepilot/
├── cmd/
│   ├── operator/main.go          # Operator entry point
│   └── scalepilot/               # Cobra CLI
│       ├── main.go
│       └── cmd/                   # CLI subcommands
├── api/v1alpha1/                  # CRD type definitions
├── internal/controller/           # One reconciler per CRD
├── pkg/
│   ├── forecast/                  # ARIMA + Holt-Winters engines
│   ├── prometheus/                # Metric query client
│   ├── multicluster/              # Cluster registry
│   ├── cloudcost/                 # AWS/GCP/Azure cost adapters
│   └── webhook/                   # Slack/PagerDuty senders
├── config/
│   ├── crd/                       # Generated CRD manifests
│   ├── rbac/                      # Generated RBAC
│   └── samples/                   # Example CR manifests
└── .github/workflows/ci.yml      # GitHub Actions CI
```

## Tech Stack

- Go 1.22+ with kubebuilder v3 scaffolding
- controller-runtime v0.17 for reconciler framework
- gonum for forecasting math primitives
- Prometheus client for metric ingestion
- cobra + viper for CLI
- ko for container image building
- ginkgo v2 + gomega for testing

## License

Apache License 2.0

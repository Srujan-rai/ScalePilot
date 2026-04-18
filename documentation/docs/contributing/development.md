---
id: development
title: Development Guide
sidebar_label: Development
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Development Guide

This guide covers setting up a local development environment for ScalePilot, running tests, understanding the code structure, and submitting contributions.

## Prerequisites

| Tool | Minimum Version | Install |
|------|----------------|---------|
| Go | 1.22 | [go.dev/dl](https://go.dev/dl/) |
| make | any | `apt install make` / `brew install make` |
| kubectl | v1.27+ | [kubernetes.io/docs/tasks/tools](https://kubernetes.io/docs/tasks/tools/) |
| kubebuilder | v3 | `go install sigs.k8s.io/kubebuilder/cmd/kubebuilder@latest` |
| golangci-lint | v1.57+ | [golangci-lint.run/usage/install](https://golangci-lint.run/usage/install/) |
| ko | latest | `go install github.com/google/ko@latest` |
| kind or minikube | any | For local cluster testing |

## Repository Structure

```
scalepilot/
├── cmd/
│   ├── operator/main.go               # Operator entry point — wires DI, starts manager
│   └── scalepilot/                    # Cobra CLI binary
│       ├── main.go
│       └── cmd/
│           ├── root.go                # Root command + viper config loading
│           ├── status.go              # scalepilot status
│           ├── simulate.go            # scalepilot simulate
│           ├── budget.go              # scalepilot budget status
│           ├── clusters.go            # scalepilot clusters list
│           ├── validate.go            # scalepilot validate
│           ├── install.go             # scalepilot install
│           └── version.go             # scalepilot version
├── api/v1alpha1/                      # CRD type definitions (kubebuilder markers)
│   ├── forecastpolicy_types.go        # ForecastPolicy + all sub-types
│   ├── federatedscaledobject_types.go # FederatedScaledObject + sub-types
│   ├── scalingbudget_types.go         # ScalingBudget + cloud cost types
│   ├── clusterscaleprofile_types.go   # ClusterScaleProfile + blackout/team types
│   ├── webhooks.go                    # Cross-field validation webhooks
│   ├── groupversion_info.go           # API group registration
│   └── zz_generated.deepcopy.go      # Auto-generated (do not edit)
├── internal/controller/               # One reconciler per CRD
│   ├── forecastpolicy_controller.go   # ForecastPolicy reconciler
│   ├── federatedscaledobject_controller.go
│   ├── scalingbudget_controller.go
│   └── clusterscaleprofile_controller.go
├── pkg/
│   ├── forecast/                      # Forecasting engines
│   │   ├── forecaster.go              # Forecaster interface, DataPoint, ForecastResult
│   │   ├── arima.go                   # ARIMA(p,d,q): Yule-Walker, Levinson-Durbin
│   │   ├── holtwinters.go             # Triple exponential smoothing (additive)
│   │   ├── replicas.go                # PeakOverHorizon, ReplicasFromForecastPeak
│   │   ├── arima_test.go
│   │   ├── holtwinters_test.go
│   │   └── replicas_test.go
│   ├── prometheus/                    # Prometheus metric client
│   │   ├── client.go                  # MetricQuerier interface + HTTP implementation
│   │   ├── helpers.go                 # API client construction, result parsing
│   │   └── client_test.go
│   ├── multicluster/                  # Multi-cluster client registry
│   │   ├── registry.go                # ClusterRegistry (sync.RWMutex + map)
│   │   ├── healthcheck.go             # API server /version health checker
│   │   └── registry_test.go
│   ├── cloudcost/                     # Cloud billing adapters
│   │   ├── cost.go                    # CostQuerier interface + CachedQuerier (TTL)
│   │   ├── aws.go                     # AWS Cost Explorer adapter
│   │   ├── gcp.go                     # GCP Billing adapter
│   │   ├── azure.go                   # Azure Cost Management adapter
│   │   └── cost_test.go
│   ├── webhook/                       # Alert notification senders
│   │   ├── sender.go                  # Sender interface, Slack + PagerDuty impls
│   │   └── sender_test.go
│   └── metrics/                       # Custom Prometheus counters
│       └── forecast.go                # scalepilot_forecastpolicy_* metrics
├── config/
│   ├── crd/bases/                     # Generated CRD YAML (from make manifests)
│   ├── rbac/                          # Generated RBAC roles
│   └── samples/                       # Example CR manifests (all 4 CRDs)
├── charts/scalepilot/                 # Helm chart
├── hack/                              # Development scripts
├── test/
│   ├── e2e/                           # End-to-end tests (ginkgo)
│   └── utils/                         # Test utilities
├── .github/workflows/ci.yml          # GitHub Actions
├── .golangci.yml                      # golangci-lint configuration (24 linters)
├── .ko.yaml                           # ko image builder config
├── Makefile                           # All build, test, generate, deploy targets
└── go.mod
```

## Setting Up Local Development

### 1. Clone and Install Dependencies

```bash
git clone https://github.com/srujan-rai/scalepilot.git
cd scalepilot

# Download Go modules
go mod download

# Verify everything compiles
go build ./...
```

### 2. Create a Local Cluster

<Tabs>
<TabItem value="kind" label="kind">

```bash
kind create cluster --name scalepilot-dev

# Install Prometheus (required for ForecastPolicy)
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm install monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace
```

</TabItem>
<TabItem value="minikube" label="minikube">

```bash
minikube start --cpus 4 --memory 8192

# Install Prometheus
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm install monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace
```

</TabItem>
</Tabs>

### 3. Install CRDs

```bash
make install
```

This runs `kubectl apply -f config/crd/bases/` after regenerating manifests.

### 4. Run the Operator Locally

```bash
make run
```

This starts the operator binary connected to your local kubeconfig. You'll see structured JSON logs in your terminal. The operator watches all namespaces by default.

Port-forward Prometheus for local development:

```bash
kubectl port-forward svc/monitoring-kube-prometheus-prometheus \
  -n monitoring 9090:9090 &
```

### 5. Apply Sample Resources

```bash
kubectl create namespace production
kubectl apply -k config/samples/
```

Or apply individual samples:

```bash
kubectl apply -f config/samples/autoscaling_v1alpha1_forecastpolicy.yaml
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make install` | Generate CRD manifests and install into cluster |
| `make run` | Run the operator locally (uses `~/.kube/config`) |
| `make build` | Build both `bin/manager` and `bin/scalepilot` |
| `make test` | Run all unit tests with race detector |
| `make test-cover` | Run tests with HTML coverage report |
| `make lint` | Run golangci-lint |
| `make manifests` | Regenerate CRD YAML from type definitions |
| `make generate` | Run controller-gen to regenerate deepcopy functions |
| `make deploy IMG=...` | Deploy the operator to the cluster using Kustomize |
| `make undeploy` | Remove the operator from the cluster |
| `make ko-build` | Build and push the operator image with ko |

## Running Tests

```bash
# All unit tests
go test ./... -race -timeout 60s

# Just the forecast package
go test ./pkg/forecast/... -v

# With coverage report
go test ./... -coverprofile=cover.out -timeout 60s
go tool cover -html=cover.out -o coverage.html
open coverage.html

# Run a single test
go test ./pkg/forecast/... -run TestARIMA -v
```

### Test Structure

Tests in ScalePilot follow two patterns:

**Table-driven tests** (unit tests in `pkg/`):

```go
func TestARIMATrain(t *testing.T) {
    cases := []struct {
        name    string
        config  ARIMAConfig
        data    []DataPoint
        wantErr bool
    }{
        {name: "valid 2,1,1", config: ARIMAConfig{P: 2, D: 1, Q: 1}, ...},
        {name: "insufficient data", data: shortData, wantErr: true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            ...
        })
    }
}
```

**Ginkgo BDD tests** (controller tests in `internal/controller/`):

```go
Describe("ForecastPolicy reconciler", func() {
    Context("when the model ConfigMap exists", func() {
        It("should patch HPA minReplicas", func() {
            ...
        })
    })
})
```

### Linting

ScalePilot uses golangci-lint with 24 linters configured in `.golangci.yml`:

```bash
make lint

# Or run directly
golangci-lint run --timeout 5m
```

Key linters enabled:
- `govet` — correctness
- `errcheck` — unhandled errors
- `staticcheck` — static analysis
- `gosec` — security
- `exhaustive` — exhaustive enum switches
- `gocognit` — cognitive complexity
- `misspell` — spelling
- `unconvert` — unnecessary type conversions

## Adding a New Feature

### Adding a New CRD

1. **Define the types** in `api/v1alpha1/<name>_types.go` following the kubebuilder marker conventions
2. **Register** the type in `groupversion_info.go`
3. **Run** `make manifests generate` to regenerate CRD YAML and deepcopy functions
4. **Create the reconciler** in `internal/controller/<name>_controller.go`
5. **Register the reconciler** in `cmd/operator/main.go`
6. **Add tests** alongside the controller file
7. **Add a sample** in `config/samples/`

### Adding a New Cloud Billing Adapter

Implement the `CostQuerier` interface in `pkg/cloudcost/`:

```go
type CostQuerier interface {
    // GetNamespaceCost returns the current month's spend for the given namespace.
    GetNamespaceCost(ctx context.Context, namespace string) (int64, error) // millidollars
}
```

Then register the adapter in `internal/controller/scalingbudget_controller.go`.

### Adding a New Forecasting Algorithm

Implement the `Forecaster` interface in `pkg/forecast/`:

```go
type Forecaster interface {
    Name() string
    Train(ctx context.Context, data []DataPoint) (*ModelParams, error)
    Predict(ctx context.Context, horizon, step time.Duration) (*ForecastResult, error)
    LoadParams(params *ModelParams) error
}
```

Add the new algorithm as a value to `ForecastAlgorithm` in `api/v1alpha1/forecastpolicy_types.go` and wire it in the ForecastPolicy reconciler.

## Code Style Guidelines

- **No global variables** — all state is passed via struct fields or function arguments
- **Explicit error handling** — every error must be handled or explicitly ignored with a comment
- **Context propagation** — all I/O operations accept a `context.Context` as the first argument
- **Interface-first** — external dependencies are defined as interfaces in the consuming package
- **Short variable names in closures, descriptive names for exported symbols**
- **Structured logging** — use `ctrl.Log.WithValues("key", val).Info(...)` not `fmt.Println`
- **Reconciler design** — return `ctrl.Result{RequeueAfter: ...}` for periodic work; never sleep in a reconciler

## Submitting a Pull Request

1. **Fork** the repository and create a branch from `main`
2. **Make your changes** and add tests
3. **Run** `make lint test` — both must pass
4. **Run** `make manifests generate` if you changed any types in `api/`
5. **Commit** with a clear message: `feat(forecast): add SARIMA(p,d,q,P,D,Q,s) support`
6. **Open a PR** against `main` with a description of the change and any relevant issue numbers

### PR Checklist

- [ ] `make lint` passes with no new warnings
- [ ] `go test ./...` passes with race detector
- [ ] New code has test coverage ≥ 80% for the changed package
- [ ] API changes have updated CRD manifests (`make manifests`)
- [ ] Documentation updated if behavior changes

## Related Resources

- **[GitHub repository](https://github.com/srujan-rai/scalepilot)**
- **[Issues](https://github.com/srujan-rai/scalepilot/issues)**
- **[controller-runtime documentation](https://pkg.go.dev/sigs.k8s.io/controller-runtime)**
- **[kubebuilder book](https://book.kubebuilder.io/)**

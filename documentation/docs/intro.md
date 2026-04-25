---
id: intro
title: What is ScalePilot?
sidebar_label: Introduction
slug: /intro
---

# What is ScalePilot?

**ScalePilot** is a cloud-agnostic Kubernetes autoscaling operator that extends HPA and KEDA with three capabilities that don't exist anywhere in the open-source ecosystem today:

1. **Predictive Scaling** - Train ARIMA or Holt-Winters models on Prometheus history and patch `HPA.minReplicas` *before* traffic spikes arrive.
2. **Multi-Cluster Workload Federation** - Monitor a metric and spill workloads to overflow clusters when a threshold is exceeded, ordered by priority.
3. **FinOps ScalingBudget** - Define a namespace-scoped monthly cost ceiling backed by live AWS/GCP/Azure billing data, with automatic breach enforcement.

## The Problem with Reactive Scaling

Kubernetes HPA is reactive. It observes a CPU or custom metric spike *right now* and adds pods. By the time those pods are:

- scheduled by the kube-scheduler,
- image-pulled and container-started,
- passing readiness/liveness probes,

…you have already dropped requests for **2–5 minutes**. KEDA extends the metric sources to Kafka, Redis, Prometheus, and 60+ others - but the reaction model is identical.

**ScalePilot's answer:** train a forecasting model on your Prometheus metric history. Predict the spike 3–10 minutes before it happens. Pre-scale your HPA `minReplicas` so pods are warm and ready when traffic arrives.

## Architecture Overview

```
                                ScalePilot Operator
                               ┌──────────────────────────────────────┐
                               │                                      │
Prometheus  ◄────── queries ───┤  ForecastPolicy Reconciler           │
(metrics)                      │    ├─ Trains ARIMA/HoltWinters       │
                               │    ├─ Caches model in ConfigMap      │
HPA         ◄────── patches ───┤    └─ Patches HPA minReplicas       │
(autoscaler)                   │                                      │
                               │  FederatedScaledObject Reconciler    │
Overflow    ◄── server-side ───┤    ├─ Monitors spillover metric      │
Clusters       apply           │    ├─ Manages ClusterRegistry        │
                               │    └─ Creates overflow Deployments   │
                               │                                      │
AWS/GCP/    ◄────── polls ─────┤  ScalingBudget Reconciler            │
Azure                          │    ├─ Polls cloud cost APIs          │
(billing)                      │    └─ Enforces breach actions        │
                               │                                      │
Slack/      ◄──── webhooks ────┤  ClusterScaleProfile Reconciler     │
PagerDuty                      │    ├─ Evaluates blackout windows     │
                               │    └─ Applies team overrides         │
                               └──────────────────────────────────────┘
```

## The Four CRDs

| CRD | Scope | Purpose |
|-----|-------|---------|
| `ForecastPolicy` | Namespaced | Attach to a Deployment + HPA; configures the forecasting model and metric source |
| `FederatedScaledObject` | Namespaced | Define primary + overflow clusters; triggers workload spillover on metric threshold |
| `ScalingBudget` | Namespaced | Set a monthly cost ceiling per namespace with cloud billing integration |
| `ClusterScaleProfile` | Cluster-scoped | Global scaling governance: blackout windows, max surge %, per-team RBAC overrides |

## How ScalePilot Compares

| Capability | HPA | KEDA | Kubecost | Admiralty | **ScalePilot** |
|-----------|-----|------|----------|-----------|----------------|
| Reactive scaling | ✓ | ✓ | - | - | ✓ |
| Predictive scaling (forecast) | - | - | - | - | **✓** |
| Multi-cluster spillover | - | - | - | Placement-based | **Metric-driven** |
| Cost observability | - | - | ✓ | - | ✓ |
| Cost enforcement (block/delay) | - | - | - | - | **✓** |
| Blackout windows | - | - | - | - | **✓** |
| Per-team scaling governance | - | - | - | - | **✓** |

## Key Design Principles

- **Non-blocking reconcilers** - Model training runs in background goroutines. Reconcile loops never block on long I/O.
- **Dependency injection** - Every external dependency (Prometheus, cloud APIs, cluster clients) is injected via interfaces, making the operator fully testable.
- **No global state** - The cluster registry is `sync.RWMutex`-protected. All shared state flows through controller-runtime.
- **Idempotent writes** - Cross-cluster Deployments use server-side apply with `FieldManager: "scalepilot"` so repeated reconciles never conflict.
- **Cost precision** - Budget ceilings are expressed in *millidollars* (integer thousandths of a dollar) to eliminate floating-point rounding errors.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.22+ |
| Scaffolding | kubebuilder v3 |
| Controller framework | controller-runtime v0.17 |
| Forecasting math | gonum v0.15 |
| Metric ingestion | prometheus/client_golang |
| CLI | cobra + viper |
| Image building | ko (distroless, no Dockerfile) |
| Testing | ginkgo v2 + gomega |
| Linting | golangci-lint (24 linters) |
| CI | GitHub Actions |

## Next Steps

- **[Prerequisites](./getting-started/prerequisites)** - What you need before installing
- **[Installation](./getting-started/installation)** - Helm, Kustomize, or local dev setup
- **[Quick Start](./getting-started/quick-start)** - Deploy your first ForecastPolicy in 5 minutes
- **[Predictive Scaling](./features/predictive-scaling)** - Deep dive into ForecastPolicy

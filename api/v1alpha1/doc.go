// Package v1alpha1 contains the API types for the autoscaling.scalepilot.io
// group. It defines four custom resource types:
//
//   - ForecastPolicy: attaches to a Deployment, runs ARIMA or Holt-Winters
//     forecasting against Prometheus metric history, and patches HPA minReplicas
//     ahead of predicted traffic spikes.
//
//   - FederatedScaledObject: defines primary and overflow clusters for
//     multi-cluster workload federation, spilling workloads when a queue depth
//     metric exceeds a threshold.
//
//   - ScalingBudget: enforces namespace-scoped FinOps cost ceilings using
//     cloud billing API integration (AWS Cost Explorer, GCP Billing, Azure
//     Cost Management) with configurable breach actions and webhook alerts.
//
//   - ClusterScaleProfile: provides cluster-wide defaults including max surge
//     percentages, cron-based blackout windows, and per-team RBAC overrides.
package v1alpha1

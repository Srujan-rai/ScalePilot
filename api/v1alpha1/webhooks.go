package v1alpha1

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ---- ForecastPolicy Webhook ----

// ForecastPolicyValidator validates ForecastPolicy resources.
type ForecastPolicyValidator struct{}

func (v *ForecastPolicyValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&ForecastPolicy{}).
		WithValidator(v).
		Complete()
}

func (v *ForecastPolicyValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	fp, ok := obj.(*ForecastPolicy)
	if !ok {
		return nil, fmt.Errorf("expected ForecastPolicy, got %T", obj)
	}
	return nil, validateForecastPolicy(fp).ToAggregate()
}

func (v *ForecastPolicyValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	fp, ok := newObj.(*ForecastPolicy)
	if !ok {
		return nil, fmt.Errorf("expected ForecastPolicy, got %T", newObj)
	}
	return nil, validateForecastPolicy(fp).ToAggregate()
}

func (v *ForecastPolicyValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateForecastPolicy(fp *ForecastPolicy) field.ErrorList {
	var errs field.ErrorList
	specPath := field.NewPath("spec")

	if fp.Spec.Algorithm == ForecastAlgorithmARIMA && fp.Spec.ARIMAParams == nil {
		errs = append(errs, field.Required(
			specPath.Child("arimaParams"),
			"arimaParams is required when algorithm is ARIMA",
		))
	}

	if fp.Spec.Algorithm == ForecastAlgorithmHoltWinters {
		if fp.Spec.HoltWintersParams == nil {
			errs = append(errs, field.Required(
				specPath.Child("holtWintersParams"),
				"holtWintersParams is required when algorithm is HoltWinters",
			))
		} else {
			hwPath := specPath.Child("holtWintersParams")
			for _, p := range []struct {
				name string
				val  string
			}{
				{"alpha", fp.Spec.HoltWintersParams.Alpha},
				{"beta", fp.Spec.HoltWintersParams.Beta},
				{"gamma", fp.Spec.HoltWintersParams.Gamma},
			} {
				v, err := strconv.ParseFloat(p.val, 64)
				if err != nil || v <= 0 || v > 1 {
					errs = append(errs, field.Invalid(
						hwPath.Child(p.name), p.val,
						"must be a decimal number in (0, 1]",
					))
				}
			}
		}
	}

	if fp.Spec.MetricSource.Address != "" && !strings.HasPrefix(fp.Spec.MetricSource.Address, "http") {
		errs = append(errs, field.Invalid(
			specPath.Child("metricSource", "address"),
			fp.Spec.MetricSource.Address,
			"must start with http:// or https://",
		))
	}

	if s := strings.TrimSpace(fp.Spec.TargetMetricValuePerReplica); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			errs = append(errs, field.Invalid(
				specPath.Child("targetMetricValuePerReplica"),
				fp.Spec.TargetMetricValuePerReplica,
				"must be a positive finite decimal number",
			))
		}
	}

	return errs
}

// ---- FederatedScaledObject Webhook ----

// FederatedScaledObjectValidator validates FederatedScaledObject resources.
type FederatedScaledObjectValidator struct{}

func (v *FederatedScaledObjectValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&FederatedScaledObject{}).
		WithValidator(v).
		Complete()
}

func (v *FederatedScaledObjectValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	fso, ok := obj.(*FederatedScaledObject)
	if !ok {
		return nil, fmt.Errorf("expected FederatedScaledObject, got %T", obj)
	}
	return nil, validateFederatedScaledObject(fso).ToAggregate()
}

func (v *FederatedScaledObjectValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	fso, ok := newObj.(*FederatedScaledObject)
	if !ok {
		return nil, fmt.Errorf("expected FederatedScaledObject, got %T", newObj)
	}
	return nil, validateFederatedScaledObject(fso).ToAggregate()
}

func (v *FederatedScaledObjectValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateFederatedScaledObject(fso *FederatedScaledObject) field.ErrorList {
	var errs field.ErrorList
	specPath := field.NewPath("spec")

	_, err := strconv.ParseFloat(fso.Spec.Metric.ThresholdValue, 64)
	if err != nil {
		errs = append(errs, field.Invalid(
			specPath.Child("metric", "thresholdValue"),
			fso.Spec.Metric.ThresholdValue,
			"must be a valid numeric value",
		))
	}

	clusterNames := make(map[string]struct{})
	clusterNames[fso.Spec.PrimaryCluster.Name] = struct{}{}
	for i, oc := range fso.Spec.OverflowClusters {
		if _, exists := clusterNames[oc.Name]; exists {
			errs = append(errs, field.Duplicate(
				specPath.Child("overflowClusters").Index(i).Child("name"),
				oc.Name,
			))
		}
		clusterNames[oc.Name] = struct{}{}
	}

	if !strings.HasPrefix(fso.Spec.Metric.PrometheusAddress, "http") {
		errs = append(errs, field.Invalid(
			specPath.Child("metric", "prometheusAddress"),
			fso.Spec.Metric.PrometheusAddress,
			"must start with http:// or https://",
		))
	}

	return errs
}

// ---- ScalingBudget Webhook ----

// ScalingBudgetValidator validates ScalingBudget resources.
type ScalingBudgetValidator struct{}

func (v *ScalingBudgetValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&ScalingBudget{}).
		WithValidator(v).
		Complete()
}

func (v *ScalingBudgetValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	sb, ok := obj.(*ScalingBudget)
	if !ok {
		return nil, fmt.Errorf("expected ScalingBudget, got %T", obj)
	}
	return nil, validateScalingBudget(sb).ToAggregate()
}

func (v *ScalingBudgetValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	sb, ok := newObj.(*ScalingBudget)
	if !ok {
		return nil, fmt.Errorf("expected ScalingBudget, got %T", newObj)
	}
	return nil, validateScalingBudget(sb).ToAggregate()
}

func (v *ScalingBudgetValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateScalingBudget(sb *ScalingBudget) field.ErrorList {
	var errs field.ErrorList
	specPath := field.NewPath("spec")

	if sb.Spec.Notifications != nil {
		notifPath := specPath.Child("notifications")
		if sb.Spec.Notifications.Slack != nil {
			url := sb.Spec.Notifications.Slack.WebhookURL
			if !strings.HasPrefix(url, "https://hooks.slack.com/") {
				errs = append(errs, field.Invalid(
					notifPath.Child("slack", "webhookURL"), url,
					"must be a valid Slack webhook URL (https://hooks.slack.com/...)",
				))
			}
		}
		if sb.Spec.Notifications.PagerDuty != nil {
			if len(sb.Spec.Notifications.PagerDuty.RoutingKey) < 32 {
				errs = append(errs, field.Invalid(
					notifPath.Child("pagerDuty", "routingKey"),
					"<redacted>",
					"PagerDuty routing key must be at least 32 characters",
				))
			}
		}
	}

	if sb.Spec.WarningThresholdPercent >= 100 {
		errs = append(errs, field.Invalid(
			specPath.Child("warningThresholdPercent"),
			sb.Spec.WarningThresholdPercent,
			"warning threshold must be less than 100% (the ceiling itself)",
		))
	}

	return errs
}

// ---- ClusterScaleProfile Webhook ----

// ClusterScaleProfileValidator validates ClusterScaleProfile resources.
type ClusterScaleProfileValidator struct{}

func (v *ClusterScaleProfileValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&ClusterScaleProfile{}).
		WithValidator(v).
		Complete()
}

func (v *ClusterScaleProfileValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	csp, ok := obj.(*ClusterScaleProfile)
	if !ok {
		return nil, fmt.Errorf("expected ClusterScaleProfile, got %T", obj)
	}
	return nil, validateClusterScaleProfile(csp).ToAggregate()
}

func (v *ClusterScaleProfileValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	csp, ok := newObj.(*ClusterScaleProfile)
	if !ok {
		return nil, fmt.Errorf("expected ClusterScaleProfile, got %T", newObj)
	}
	return nil, validateClusterScaleProfile(csp).ToAggregate()
}

func (v *ClusterScaleProfileValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateClusterScaleProfile(csp *ClusterScaleProfile) field.ErrorList {
	var errs field.ErrorList
	specPath := field.NewPath("spec")

	for i, bw := range csp.Spec.BlackoutWindows {
		bwPath := specPath.Child("blackoutWindows").Index(i)

		if len(strings.Fields(bw.Start)) != 5 {
			errs = append(errs, field.Invalid(
				bwPath.Child("start"), bw.Start,
				"must be a 5-field cron expression (minute hour day-of-month month day-of-week)",
			))
		}
		if len(strings.Fields(bw.End)) != 5 {
			errs = append(errs, field.Invalid(
				bwPath.Child("end"), bw.End,
				"must be a 5-field cron expression (minute hour day-of-month month day-of-week)",
			))
		}
	}

	teamNames := make(map[string]struct{})
	for i, to := range csp.Spec.TeamOverrides {
		toPath := specPath.Child("teamOverrides").Index(i)
		if _, exists := teamNames[to.TeamName]; exists {
			errs = append(errs, field.Duplicate(toPath.Child("teamName"), to.TeamName))
		}
		teamNames[to.TeamName] = struct{}{}

		if to.MaxSurgePercent != nil && *to.MaxSurgePercent > csp.Spec.MaxSurgePercent {
			errs = append(errs, field.Invalid(
				toPath.Child("maxSurgePercent"),
				*to.MaxSurgePercent,
				fmt.Sprintf("team maxSurgePercent cannot exceed cluster default (%d)", csp.Spec.MaxSurgePercent),
			))
		}
	}

	return errs
}

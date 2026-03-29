/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1 "k8s.io/api/autoscaling/v1"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/forecast"
)

// MetricQuerier is the interface the ForecastPolicy reconciler uses to
// fetch time-series data. Defined here (consumer side) per Go convention.
type MetricQuerier interface {
	RangeQuery(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]forecast.DataPoint, error)
	InstantQuery(ctx context.Context, query string) (float64, error)
}

// ForecasterFactory creates a Forecaster from a ForecastPolicy spec.
type ForecasterFactory func(spec autoscalingv1alpha1.ForecastPolicySpec) (forecast.Forecaster, error)

// ForecastPolicyReconciler reconciles a ForecastPolicy object.
// On each cycle it:
//  1. Checks if the model needs retraining (background goroutine writes ConfigMap)
//  2. Loads cached model from ConfigMap
//  3. Runs prediction for the lead-time horizon
//  4. Patches HPA minReplicas if a spike is predicted
type ForecastPolicyReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	Clock                Clock
	ForecasterFactory    ForecasterFactory
	MetricQuerierFactory func(address string) (MetricQuerier, error)
}

//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=forecastpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=forecastpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=forecastpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

// Reconcile loads the cached forecast model, runs prediction, and patches
// the target HPA's minReplicas ahead of predicted traffic spikes.
func (r *ForecastPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var policy autoscalingv1alpha1.ForecastPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	clk := r.Clock
	if clk == nil {
		clk = realClock{}
	}
	now := clk.Now()

	// Check if retraining is needed and trigger it asynchronously.
	needsRetrain := policy.Status.LastTrainedAt == nil ||
		now.Sub(policy.Status.LastTrainedAt.Time) > time.Duration(policy.Spec.RetrainIntervalMinutes)*time.Minute

	if needsRetrain {
		go r.trainModelAsync(context.Background(), policy)
		logger.Info("triggered background model retraining")
	}

	// Load the cached model from ConfigMap.
	cmName := modelConfigMapName(policy.Name)
	var cm corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: policy.Namespace}, &cm); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("model ConfigMap not found, waiting for training to complete", "configmap", cmName)
			return r.updateStatusError(ctx, &policy, "ModelNotReady", "waiting for initial model training"), nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching model ConfigMap %s: %w", cmName, err)
	}

	// Parse model params from ConfigMap.
	modelJSON, ok := cm.Data["model"]
	if !ok {
		return r.updateStatusError(ctx, &policy, "ModelCorrupted", "ConfigMap missing 'model' key"), nil
	}

	var params forecast.ModelParams
	if err := json.Unmarshal([]byte(modelJSON), &params); err != nil {
		return r.updateStatusError(ctx, &policy, "ModelCorrupted", fmt.Sprintf("failed to parse model: %v", err)), nil
	}

	// Create a forecaster and load the cached params.
	factory := r.ForecasterFactory
	if factory == nil {
		factory = defaultForecasterFactory
	}

	forecaster, err := factory(policy.Spec)
	if err != nil {
		return r.updateStatusError(ctx, &policy, "InvalidConfig", err.Error()), nil
	}

	if err := forecaster.LoadParams(&params); err != nil {
		return r.updateStatusError(ctx, &policy, "ModelLoadFailed", err.Error()), nil
	}

	// Run prediction for the lead-time window.
	horizon := time.Duration(policy.Spec.LeadTimeMinutes) * time.Minute
	step := 1 * time.Minute
	result, err := forecaster.Predict(ctx, horizon, step)
	if err != nil {
		return r.updateStatusError(ctx, &policy, "PredictionFailed", err.Error()), nil
	}

	// Determine the peak predicted value within the lead-time horizon.
	peakValue := 0.0
	for _, dp := range result.PredictedValues {
		if dp.Value > peakValue {
			peakValue = dp.Value
		}
	}

	// Convert the peak metric value to a replica count.
	// We use a simple heuristic: predicted_replicas = ceil(predicted_value / current_per_replica_value).
	// For a more accurate mapping, the user should configure their HPA metric thresholds to match.
	predictedReplicas := int32(math.Ceil(peakValue))
	if predictedReplicas < 1 {
		predictedReplicas = 1
	}

	// Apply the max replica cap if configured.
	if policy.Spec.MaxReplicaCap != nil && predictedReplicas > *policy.Spec.MaxReplicaCap {
		logger.Info("capping predicted replicas", "predicted", predictedReplicas, "cap", *policy.Spec.MaxReplicaCap)
		predictedReplicas = *policy.Spec.MaxReplicaCap
	}

	// Fetch the target HPA.
	var hpa autoscalingv1.HorizontalPodAutoscaler
	hpaKey := types.NamespacedName{Name: policy.Spec.TargetHPA.Name, Namespace: policy.Namespace}
	if err := r.Get(ctx, hpaKey, &hpa); err != nil {
		return r.updateStatusError(ctx, &policy, "HPANotFound", fmt.Sprintf("HPA %s not found: %v", hpaKey, err)), nil
	}

	currentMin := int32(1)
	if hpa.Spec.MinReplicas != nil {
		currentMin = *hpa.Spec.MinReplicas
	}

	// Patch HPA minReplicas if the prediction suggests a higher value.
	if predictedReplicas > currentMin && !policy.Spec.DryRun {
		hpa.Spec.MinReplicas = &predictedReplicas
		if err := r.Update(ctx, &hpa); err != nil {
			return ctrl.Result{}, fmt.Errorf("patching HPA %s minReplicas: %w", hpaKey, err)
		}
		logger.Info("patched HPA minReplicas",
			"hpa", hpaKey,
			"from", currentMin,
			"to", predictedReplicas,
			"leadTimeMinutes", policy.Spec.LeadTimeMinutes)
	} else if policy.Spec.DryRun {
		logger.Info("[DRY RUN] would patch HPA minReplicas",
			"hpa", hpaKey,
			"from", currentMin,
			"to", predictedReplicas)
	}

	// Update status.
	nowMeta := metav1.NewTime(now)
	trainedAt := metav1.NewTime(params.TrainedAt)
	policy.Status.LastTrainedAt = &trainedAt
	policy.Status.CurrentPrediction = fmt.Sprintf("%.2f", peakValue)
	policy.Status.PredictedMinReplicas = &predictedReplicas
	policy.Status.ActiveMinReplicas = hpa.Spec.MinReplicas
	policy.Status.ModelConfigMap = cmName

	readyCondition := metav1.Condition{
		Type:               string(autoscalingv1alpha1.ForecastConditionModelReady),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: nowMeta,
		Reason:             "ModelLoaded",
		Message:            fmt.Sprintf("Model %s loaded, RMSE=%.4f", forecaster.Name(), params.RMSE),
	}
	setCondition(&policy.Status.Conditions, readyCondition)

	patchCondition := metav1.Condition{
		Type:               string(autoscalingv1alpha1.ForecastConditionPatchApplied),
		ObservedGeneration: policy.Generation,
		LastTransitionTime: nowMeta,
	}
	if predictedReplicas > currentMin {
		patchCondition.Status = metav1.ConditionTrue
		patchCondition.Reason = "Patched"
		patchCondition.Message = fmt.Sprintf("HPA minReplicas set to %d (was %d)", predictedReplicas, currentMin)
	} else {
		patchCondition.Status = metav1.ConditionFalse
		patchCondition.Reason = "NoChangeNeeded"
		patchCondition.Message = fmt.Sprintf("Current minReplicas %d is sufficient", currentMin)
	}
	setCondition(&policy.Status.Conditions, patchCondition)

	// Clear any error condition.
	clearErrorCondition(&policy.Status.Conditions, policy.Generation, nowMeta)

	if err := r.Status().Update(ctx, &policy); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating ForecastPolicy status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// trainModelAsync fetches metric history from Prometheus, trains the forecast
// model, and writes the serialized parameters to a ConfigMap. This runs in a
// background goroutine so the reconcile loop never blocks on training.
func (r *ForecastPolicyReconciler) trainModelAsync(ctx context.Context, policy autoscalingv1alpha1.ForecastPolicy) {
	logger := ctrl.Log.WithName("trainer").WithValues("policy", policy.Name, "namespace", policy.Namespace)

	factory := r.ForecasterFactory
	if factory == nil {
		factory = defaultForecasterFactory
	}

	forecaster, err := factory(policy.Spec)
	if err != nil {
		logger.Error(err, "failed to create forecaster")
		return
	}

	mqFactory := r.MetricQuerierFactory
	if mqFactory == nil {
		logger.Error(fmt.Errorf("no MetricQuerierFactory configured"), "cannot fetch training data")
		return
	}

	querier, err := mqFactory(policy.Spec.MetricSource.Address)
	if err != nil {
		logger.Error(err, "failed to create metric querier")
		return
	}

	historyDur := parseDurationShorthand(policy.Spec.MetricSource.HistoryDuration)
	stepInterval := 5 * time.Minute
	if policy.Spec.MetricSource.StepInterval != "" {
		stepInterval = parseDurationShorthand(policy.Spec.MetricSource.StepInterval)
	}

	now := time.Now()
	data, err := querier.RangeQuery(ctx, policy.Spec.MetricSource.Query,
		now.Add(-historyDur), now, stepInterval)
	if err != nil {
		logger.Error(err, "failed to query prometheus for training data")
		return
	}

	params, err := forecaster.Train(ctx, data)
	if err != nil {
		logger.Error(err, "failed to train model")
		return
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		logger.Error(err, "failed to serialize model params")
		return
	}

	// Write to ConfigMap.
	cmName := modelConfigMapName(policy.Name)
	var cm corev1.ConfigMap
	cmKey := types.NamespacedName{Name: cmName, Namespace: policy.Namespace}

	if err := r.Get(ctx, cmKey, &cm); err != nil {
		if errors.IsNotFound(err) {
			cm = corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: policy.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "scalepilot",
						"scalepilot.io/forecast-model": policy.Name,
					},
				},
				Data: map[string]string{
					"model": string(paramsJSON),
				},
			}
			if err := r.Create(ctx, &cm); err != nil {
				logger.Error(err, "failed to create model ConfigMap")
				return
			}
			logger.Info("created model ConfigMap", "configmap", cmName, "rmse", params.RMSE)
			return
		}
		logger.Error(err, "failed to fetch model ConfigMap")
		return
	}

	cm.Data["model"] = string(paramsJSON)
	if err := r.Update(ctx, &cm); err != nil {
		logger.Error(err, "failed to update model ConfigMap")
		return
	}
	logger.Info("updated model ConfigMap", "configmap", cmName, "rmse", params.RMSE)
}

func (r *ForecastPolicyReconciler) updateStatusError(ctx context.Context, policy *autoscalingv1alpha1.ForecastPolicy, reason, message string) ctrl.Result {
	logger := log.FromContext(ctx)

	nowMeta := metav1.NewTime(time.Now())
	errCondition := metav1.Condition{
		Type:               string(autoscalingv1alpha1.ForecastConditionError),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: nowMeta,
		Reason:             reason,
		Message:            message,
	}
	setCondition(&policy.Status.Conditions, errCondition)

	if err := r.Status().Update(ctx, policy); err != nil {
		logger.Error(err, "failed to update error status")
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ForecastPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.ForecastPolicy{}).
		Complete(r)
}

func modelConfigMapName(policyName string) string {
	return fmt.Sprintf("scalepilot-model-%s", policyName)
}

// defaultForecasterFactory creates a Forecaster from the policy spec using
// the built-in ARIMA and HoltWinters implementations.
func defaultForecasterFactory(spec autoscalingv1alpha1.ForecastPolicySpec) (forecast.Forecaster, error) {
	switch spec.Algorithm {
	case autoscalingv1alpha1.ForecastAlgorithmARIMA:
		p, d, q := 2, 1, 1
		if spec.ARIMAParams != nil {
			p = spec.ARIMAParams.P
			d = spec.ARIMAParams.D
			q = spec.ARIMAParams.Q
		}
		return forecast.NewARIMA(forecast.ARIMAConfig{P: p, D: d, Q: q}), nil

	case autoscalingv1alpha1.ForecastAlgorithmHoltWinters:
		cfg := forecast.HoltWintersConfig{
			Alpha:           0.3,
			Beta:            0.1,
			Gamma:           0.2,
			SeasonalPeriods: 24,
		}
		if spec.HoltWintersParams != nil {
			if v, err := strconv.ParseFloat(spec.HoltWintersParams.Alpha, 64); err == nil {
				cfg.Alpha = v
			}
			if v, err := strconv.ParseFloat(spec.HoltWintersParams.Beta, 64); err == nil {
				cfg.Beta = v
			}
			if v, err := strconv.ParseFloat(spec.HoltWintersParams.Gamma, 64); err == nil {
				cfg.Gamma = v
			}
			cfg.SeasonalPeriods = spec.HoltWintersParams.SeasonalPeriods
		}
		return forecast.NewHoltWinters(cfg), nil

	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", spec.Algorithm)
	}
}

// parseDurationShorthand converts shorthand durations like "7d", "24h", "30m" to time.Duration.
func parseDurationShorthand(s string) time.Duration {
	if len(s) == 0 {
		return time.Hour
	}

	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return time.Hour
	}

	switch unit {
	case 's':
		return time.Duration(num) * time.Second
	case 'm':
		return time.Duration(num) * time.Minute
	case 'h':
		return time.Duration(num) * time.Hour
	case 'd':
		return time.Duration(num) * 24 * time.Hour
	default:
		return time.Hour
	}
}

// clearErrorCondition sets the Error condition to False.
func clearErrorCondition(conditions *[]metav1.Condition, generation int64, now metav1.Time) {
	cleared := metav1.Condition{
		Type:               string(autoscalingv1alpha1.ForecastConditionError),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		LastTransitionTime: now,
		Reason:             "Cleared",
		Message:            "No errors",
	}
	setCondition(conditions, cleared)
}

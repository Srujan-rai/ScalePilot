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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ForecastAlgorithm selects the time-series forecasting model.
// +kubebuilder:validation:Enum=ARIMA;HoltWinters
type ForecastAlgorithm string

const (
	ForecastAlgorithmARIMA       ForecastAlgorithm = "ARIMA"
	ForecastAlgorithmHoltWinters ForecastAlgorithm = "HoltWinters"
)

// ARIMAParams configures the ARIMA(p,d,q) model.
type ARIMAParams struct {
	// P is the autoregressive order.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	P int `json:"p"`

	// D is the differencing order.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	D int `json:"d"`

	// Q is the moving-average order.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	Q int `json:"q"`
}

// HoltWintersParams configures triple exponential smoothing.
type HoltWintersParams struct {
	// Alpha is the level smoothing coefficient in range (0,1].
	// Expressed as a decimal string (e.g. "0.3").
	// +kubebuilder:validation:Pattern=`^0?\.\d+$|^1(\.0+)?$`
	Alpha string `json:"alpha"`

	// Beta is the trend smoothing coefficient in range (0,1].
	// Expressed as a decimal string (e.g. "0.1").
	// +kubebuilder:validation:Pattern=`^0?\.\d+$|^1(\.0+)?$`
	Beta string `json:"beta"`

	// Gamma is the seasonal smoothing coefficient in range (0,1].
	// Expressed as a decimal string (e.g. "0.2").
	// +kubebuilder:validation:Pattern=`^0?\.\d+$|^1(\.0+)?$`
	Gamma string `json:"gamma"`

	// SeasonalPeriods is the number of data points in one seasonal cycle.
	// +kubebuilder:validation:Minimum=2
	SeasonalPeriods int `json:"seasonalPeriods"`
}

// PrometheusMetricSource defines the Prometheus query and connection info.
type PrometheusMetricSource struct {
	// Address is the Prometheus server URL (e.g. http://prometheus:9090).
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address"`

	// Query is the PromQL query that returns a scalar or single-element vector.
	// +kubebuilder:validation:MinLength=1
	Query string `json:"query"`

	// HistoryDuration is how far back to fetch training data (e.g. "7d", "24h").
	// +kubebuilder:validation:Pattern=`^\d+(s|m|h|d)$`
	HistoryDuration string `json:"historyDuration"`

	// StepInterval is the resolution step for range queries (e.g. "5m").
	// +kubebuilder:validation:Pattern=`^\d+(s|m|h|d)$`
	// +optional
	StepInterval string `json:"stepInterval,omitempty"`
}

// TargetHPARef references the HPA whose minReplicas will be patched.
type TargetHPARef struct {
	// Name of the HorizontalPodAutoscaler.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// TargetDeploymentRef references the Deployment being forecast-scaled.
type TargetDeploymentRef struct {
	// Name of the Deployment.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ScaleUpGuard runs a Prometheus instant query before raising HPA minReplicas.
// If the query returns a value strictly greater than MaxMetricValue, the scale-up is skipped.
// Use for SLO-style gates (e.g. do not pre-scale while 5xx rate is high).
type ScaleUpGuard struct {
	// Address defaults to spec.metricSource.address when empty.
	// +optional
	Address string `json:"address,omitempty"`

	// Query is PromQL executed as an instant query.
	// +kubebuilder:validation:MinLength=1
	Query string `json:"query"`

	// MaxMetricValue is a decimal threshold; scale-up is blocked when the instant query is strictly greater than this value.
	// +kubebuilder:validation:MinLength=1
	MaxMetricValue string `json:"maxMetricValue"`
}

// ForecastPolicySpec defines the desired state of ForecastPolicy.
type ForecastPolicySpec struct {
	// TargetDeployment references the Deployment to forecast for.
	TargetDeployment TargetDeploymentRef `json:"targetDeployment"`

	// TargetHPA references the HPA whose minReplicas will be patched
	// ahead of predicted spikes.
	TargetHPA TargetHPARef `json:"targetHPA"`

	// MetricSource configures the Prometheus metric used for training.
	MetricSource PrometheusMetricSource `json:"metricSource"`

	// Algorithm selects the forecasting model.
	// +kubebuilder:default=ARIMA
	Algorithm ForecastAlgorithm `json:"algorithm"`

	// ARIMAParams configures the ARIMA model. Required when algorithm is ARIMA.
	// +optional
	ARIMAParams *ARIMAParams `json:"arimaParams,omitempty"`

	// HoltWintersParams configures the Holt-Winters model. Required when algorithm is HoltWinters.
	// +optional
	HoltWintersParams *HoltWintersParams `json:"holtWintersParams,omitempty"`

	// LeadTimeMinutes is how many minutes before a predicted spike
	// the HPA minReplicas should be raised. Range: 3-10.
	// +kubebuilder:validation:Minimum=3
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=5
	LeadTimeMinutes int `json:"leadTimeMinutes"`

	// RetrainIntervalMinutes is how often the model is retrained
	// in a background goroutine.
	// +kubebuilder:validation:Minimum=5
	// +kubebuilder:default=30
	RetrainIntervalMinutes int `json:"retrainIntervalMinutes"`

	// MaxReplicaCap is the ceiling applied to forecast-driven minReplicas.
	// Prevents runaway scaling from model errors.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxReplicaCap *int32 `json:"maxReplicaCap,omitempty"`

	// DryRun, when true, logs predicted values and intended HPA patches
	// without applying them. Useful for simulation.
	// +kubebuilder:default=false
	// +optional
	DryRun bool `json:"dryRun,omitempty"`

	// TargetMetricValuePerReplica is how much of the forecast metric each replica
	// is expected to handle (same units as MetricSource.Query). When set to a
	// positive decimal string, desired replicas = ceil(peakForecast / value),
	// matching HPA averageValue-style thinking. When empty, legacy behavior applies:
	// ceil(peakForecast) — only valid if the query already returns replica-equivalent units.
	// +optional
	TargetMetricValuePerReplica string `json:"targetMetricValuePerReplica,omitempty"`

	// UseUpperConfidenceBound, when true, uses the 95% upper forecast bound instead
	// of the point forecast when taking the peak over the lead-time window (more conservative).
	// +kubebuilder:default=false
	// +optional
	UseUpperConfidenceBound bool `json:"useUpperConfidenceBound,omitempty"`

	// ScaleUpGuard optional Prometheus gate before increasing minReplicas.
	// +optional
	ScaleUpGuard *ScaleUpGuard `json:"scaleUpGuard,omitempty"`
}

// ForecastConditionType represents the state of a forecast policy.
type ForecastConditionType string

const (
	// ForecastConditionModelReady indicates the model has been trained.
	ForecastConditionModelReady ForecastConditionType = "ModelReady"
	// ForecastConditionPatchApplied indicates the HPA was patched.
	ForecastConditionPatchApplied ForecastConditionType = "PatchApplied"
	// ForecastConditionError indicates a processing error.
	ForecastConditionError ForecastConditionType = "Error"
)

// ForecastPolicyStatus defines the observed state of ForecastPolicy.
type ForecastPolicyStatus struct {
	// LastTrainedAt is when the model was last retrained.
	// +optional
	LastTrainedAt *metav1.Time `json:"lastTrainedAt,omitempty"`

	// CurrentPrediction is the latest forecasted metric value.
	// +optional
	CurrentPrediction string `json:"currentPrediction,omitempty"`

	// PredictedMinReplicas is the minReplicas value the forecast suggests.
	// +optional
	PredictedMinReplicas *int32 `json:"predictedMinReplicas,omitempty"`

	// ActiveMinReplicas is the minReplicas currently set on the HPA.
	// +optional
	ActiveMinReplicas *int32 `json:"activeMinReplicas,omitempty"`

	// ModelConfigMap stores the name of the ConfigMap caching the trained model.
	// +optional
	ModelConfigMap string `json:"modelConfigMap,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Algorithm",type=string,JSONPath=`.spec.algorithm`
// +kubebuilder:printcolumn:name="Deployment",type=string,JSONPath=`.spec.targetDeployment.name`
// +kubebuilder:printcolumn:name="Predicted",type=string,JSONPath=`.status.predictedMinReplicas`
// +kubebuilder:printcolumn:name="Active",type=string,JSONPath=`.status.activeMinReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ForecastPolicy attaches to a Deployment, reads Prometheus metric history,
// runs ARIMA or Holt-Winters forecast, and patches HPA minReplicas
// before predicted spikes.
type ForecastPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ForecastPolicySpec   `json:"spec,omitempty"`
	Status ForecastPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ForecastPolicyList contains a list of ForecastPolicy.
type ForecastPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ForecastPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ForecastPolicy{}, &ForecastPolicyList{})
}

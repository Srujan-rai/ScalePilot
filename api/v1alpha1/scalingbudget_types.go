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

// CloudProvider identifies the cost-data source.
// +kubebuilder:validation:Enum=AWS;GCP;Azure
type CloudProvider string

const (
	CloudProviderAWS   CloudProvider = "AWS"
	CloudProviderGCP   CloudProvider = "GCP"
	CloudProviderAzure CloudProvider = "Azure"
)

// BreachAction defines what happens when a budget ceiling is exceeded.
// +kubebuilder:validation:Enum=Downgrade;Delay;Block
type BreachAction string

const (
	// BreachActionDowngrade reduces resource requests for new pods.
	BreachActionDowngrade BreachAction = "Downgrade"
	// BreachActionDelay pauses scale-up events until the budget resets.
	BreachActionDelay BreachAction = "Delay"
	// BreachActionBlock rejects scale-up entirely with an admission webhook.
	BreachActionBlock BreachAction = "Block"
)

// CloudCostConfig holds the credentials and settings for a cloud cost API.
type CloudCostConfig struct {
	// Provider selects the cloud billing API.
	Provider CloudProvider `json:"provider"`

	// CredentialsSecretRef points to a Secret containing cloud API credentials.
	// AWS: keys aws_access_key_id, aws_secret_access_key
	// GCP: key service_account_json
	// Azure: keys tenant_id, client_id, client_secret, subscription_id
	CredentialsSecretRef SecretReference `json:"credentialsSecretRef"`

	// Region filters cost data to a specific cloud region.
	// +optional
	Region string `json:"region,omitempty"`

	// AccountID is the cloud account/project ID to query costs for.
	// +optional
	AccountID string `json:"accountId,omitempty"`
}

// NotificationConfig defines a webhook endpoint for breach alerts.
type NotificationConfig struct {
	// Slack configures Slack webhook notifications.
	// +optional
	Slack *SlackNotification `json:"slack,omitempty"`

	// PagerDuty configures PagerDuty event notifications.
	// +optional
	PagerDuty *PagerDutyNotification `json:"pagerDuty,omitempty"`
}

// SlackNotification holds Slack webhook configuration.
type SlackNotification struct {
	// WebhookURL is the Slack incoming webhook URL.
	// +kubebuilder:validation:MinLength=1
	WebhookURL string `json:"webhookURL"`

	// Channel overrides the default channel for the webhook.
	// +optional
	Channel string `json:"channel,omitempty"`
}

// PagerDutyNotification holds PagerDuty event configuration.
type PagerDutyNotification struct {
	// RoutingKey is the PagerDuty Events API v2 routing key.
	// +kubebuilder:validation:MinLength=1
	RoutingKey string `json:"routingKey"`

	// Severity is the PagerDuty alert severity.
	// +kubebuilder:validation:Enum=critical;error;warning;info
	// +kubebuilder:default=warning
	Severity string `json:"severity"`
}

// ScalingBudgetSpec defines the desired state of ScalingBudget.
type ScalingBudgetSpec struct {
	// Namespace scopes the budget to a single Kubernetes namespace.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// CeilingMillidollars is the monthly cost ceiling in thousandths of a dollar.
	// Using integer millidollars avoids floating-point rounding (e.g. $150.00 = 150000).
	// +kubebuilder:validation:Minimum=1
	CeilingMillidollars int64 `json:"ceilingMillidollars"`

	// CloudCost configures the cloud billing API integration.
	CloudCost CloudCostConfig `json:"cloudCost"`

	// BreachAction specifies what happens when spend exceeds the ceiling.
	// +kubebuilder:default=Delay
	BreachAction BreachAction `json:"breachAction"`

	// WarningThresholdPercent triggers a warning notification at this
	// percentage of the ceiling (e.g. 80 means alert at 80% spend).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=80
	WarningThresholdPercent int `json:"warningThresholdPercent"`

	// Notifications configures where breach/warning alerts are sent.
	// +optional
	Notifications *NotificationConfig `json:"notifications,omitempty"`

	// PollIntervalMinutes is how often to re-check cloud cost APIs.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=5
	PollIntervalMinutes int `json:"pollIntervalMinutes"`
}

// ScalingBudgetStatus defines the observed state of ScalingBudget.
type ScalingBudgetStatus struct {
	// CurrentSpendMillidollars is the current month's spend in millidollars.
	CurrentSpendMillidollars int64 `json:"currentSpendMillidollars,omitempty"`

	// UtilizationPercent is CurrentSpend / Ceiling * 100.
	UtilizationPercent int `json:"utilizationPercent,omitempty"`

	// Breached is true when current spend exceeds the ceiling.
	Breached bool `json:"breached,omitempty"`

	// LastCheckedAt is the timestamp of the last cost API poll.
	// +optional
	LastCheckedAt *metav1.Time `json:"lastCheckedAt,omitempty"`

	// BlockedScaleEvents counts how many scale-up requests were
	// blocked or delayed due to budget breach.
	BlockedScaleEvents int32 `json:"blockedScaleEvents,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.namespace`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.cloudCost.provider`
// +kubebuilder:printcolumn:name="Utilization",type=integer,JSONPath=`.status.utilizationPercent`,description="Cost utilization %"
// +kubebuilder:printcolumn:name="Breached",type=boolean,JSONPath=`.status.breached`
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.breachAction`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ScalingBudget defines a namespace-scoped cost ceiling that intercepts
// scale decisions and enforces FinOps budget controls with cloud billing
// API integration.
type ScalingBudget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScalingBudgetSpec   `json:"spec,omitempty"`
	Status ScalingBudgetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScalingBudgetList contains a list of ScalingBudget.
type ScalingBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScalingBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScalingBudget{}, &ScalingBudgetList{})
}

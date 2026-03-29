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

// BlackoutWindow defines a time window during which scaling is suppressed.
type BlackoutWindow struct {
	// Name is a human-readable label for this window (e.g. "maintenance-friday").
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Start is a cron expression for when the blackout begins.
	// Uses standard 5-field cron syntax (minute hour day-of-month month day-of-week).
	// +kubebuilder:validation:MinLength=9
	Start string `json:"start"`

	// End is a cron expression for when the blackout ends.
	// +kubebuilder:validation:MinLength=9
	End string `json:"end"`

	// Timezone is the IANA timezone for the cron schedule (e.g. "America/New_York").
	// +kubebuilder:default=UTC
	// +optional
	Timezone string `json:"timezone,omitempty"`
}

// TeamOverride provides per-team RBAC and scaling constraints.
type TeamOverride struct {
	// TeamName identifies the team (maps to a Kubernetes Group or ServiceAccount prefix).
	// +kubebuilder:validation:MinLength=1
	TeamName string `json:"teamName"`

	// Namespaces restricts this team's scaling operations to these namespaces.
	// +kubebuilder:validation:MinItems=1
	Namespaces []string `json:"namespaces"`

	// MaxSurgePercent overrides the cluster default for this team.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	MaxSurgePercent *int32 `json:"maxSurgePercent,omitempty"`

	// AllowedAlgorithms restricts which forecast algorithms this team may use.
	// Empty means all algorithms are allowed.
	// +optional
	AllowedAlgorithms []ForecastAlgorithm `json:"allowedAlgorithms,omitempty"`

	// MaxReplicasPerDeployment caps the total replicas any single deployment
	// owned by this team can scale to.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxReplicasPerDeployment *int32 `json:"maxReplicasPerDeployment,omitempty"`
}

// ClusterScaleProfileSpec defines the desired state of ClusterScaleProfile.
type ClusterScaleProfileSpec struct {
	// MaxSurgePercent is the default maximum percentage increase in replicas
	// allowed in a single reconcile cycle, cluster-wide.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=25
	MaxSurgePercent int32 `json:"maxSurgePercent"`

	// BlackoutWindows defines time windows during which all scaling
	// operations are suppressed (e.g. during maintenance or deploy freezes).
	// +optional
	BlackoutWindows []BlackoutWindow `json:"blackoutWindows,omitempty"`

	// TeamOverrides provides per-team RBAC-aware scaling constraints
	// that override the cluster defaults.
	// +optional
	TeamOverrides []TeamOverride `json:"teamOverrides,omitempty"`

	// DefaultCooldownSeconds is the minimum seconds between consecutive
	// scale-up events, cluster-wide.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=60
	DefaultCooldownSeconds int32 `json:"defaultCooldownSeconds"`

	// EnableGlobalDryRun disables all scaling operations cluster-wide
	// and logs intended actions instead.
	// +kubebuilder:default=false
	// +optional
	EnableGlobalDryRun bool `json:"enableGlobalDryRun,omitempty"`
}

// ClusterScaleProfileStatus defines the observed state of ClusterScaleProfile.
type ClusterScaleProfileStatus struct {
	// ActiveBlackout is true when the current time falls within a blackout window.
	ActiveBlackout bool `json:"activeBlackout,omitempty"`

	// ActiveBlackoutName is the name of the currently active blackout window.
	// +optional
	ActiveBlackoutName string `json:"activeBlackoutName,omitempty"`

	// TeamsConfigured is the number of teams with overrides.
	TeamsConfigured int32 `json:"teamsConfigured,omitempty"`

	// LastReconcileTime is the timestamp of the last reconciliation.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="MaxSurge",type=integer,JSONPath=`.spec.maxSurgePercent`,description="Max surge %"
// +kubebuilder:printcolumn:name="Blackout",type=boolean,JSONPath=`.status.activeBlackout`
// +kubebuilder:printcolumn:name="Teams",type=integer,JSONPath=`.status.teamsConfigured`
// +kubebuilder:printcolumn:name="DryRun",type=boolean,JSONPath=`.spec.enableGlobalDryRun`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterScaleProfile provides cluster-wide defaults for scaling operations
// including maximum surge percentages, blackout windows (cron-based),
// and per-team RBAC overrides.
type ClusterScaleProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterScaleProfileSpec   `json:"spec,omitempty"`
	Status ClusterScaleProfileStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterScaleProfileList contains a list of ClusterScaleProfile.
type ClusterScaleProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterScaleProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterScaleProfile{}, &ClusterScaleProfileList{})
}

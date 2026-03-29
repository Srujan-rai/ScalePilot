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

// ClusterRef identifies a cluster by referencing the Secret that holds its kubeconfig.
type ClusterRef struct {
	// Name is a human-readable identifier for this cluster.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// SecretRef references a Secret containing a kubeconfig file at key "kubeconfig".
	// The Secret must be labeled scalepilot.io/cluster=true.
	SecretRef SecretReference `json:"secretRef"`

	// Namespace is the target namespace for spilled workloads on this cluster.
	// Defaults to the same namespace as the primary workload.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// MaxCapacity is the maximum replicas this overflow cluster can accept.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxCapacity *int32 `json:"maxCapacity,omitempty"`

	// Priority determines the order in which overflow clusters are used.
	// Lower values are used first.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	// +optional
	Priority int32 `json:"priority,omitempty"`
}

// SecretReference points to a Secret in a specific namespace.
type SecretReference struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the Secret.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// SpilloverMetric defines the metric that triggers cross-cluster spillover.
type SpilloverMetric struct {
	// Query is a PromQL query returning a scalar (e.g. queue depth).
	// +kubebuilder:validation:MinLength=1
	Query string `json:"query"`

	// PrometheusAddress is the endpoint of the Prometheus server.
	// +kubebuilder:validation:MinLength=1
	PrometheusAddress string `json:"prometheusAddress"`

	// ThresholdValue is the metric value above which spillover activates.
	// +kubebuilder:validation:MinLength=1
	ThresholdValue string `json:"thresholdValue"`
}

// WorkloadTemplate defines the workload to be spilled to overflow clusters.
type WorkloadTemplate struct {
	// DeploymentName is the name of the Deployment on the primary cluster.
	// +kubebuilder:validation:MinLength=1
	DeploymentName string `json:"deploymentName"`

	// Namespace is the namespace of the Deployment on the primary cluster.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// FederatedScaledObjectSpec defines the desired state of FederatedScaledObject.
type FederatedScaledObjectSpec struct {
	// PrimaryCluster identifies the main cluster where the workload runs.
	PrimaryCluster ClusterRef `json:"primaryCluster"`

	// OverflowClusters lists the clusters that receive spillover workloads,
	// ordered by priority (lowest priority value is used first).
	// +kubebuilder:validation:MinItems=1
	OverflowClusters []ClusterRef `json:"overflowClusters"`

	// Metric defines the signal that triggers spillover.
	Metric SpilloverMetric `json:"metric"`

	// Workload references the Deployment to be replicated.
	Workload WorkloadTemplate `json:"workload"`

	// CooldownSeconds prevents thrashing by requiring this many seconds
	// between scale-down events on overflow clusters.
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:default=120
	CooldownSeconds int32 `json:"cooldownSeconds"`

	// MaxTotalReplicas caps the total replicas across all clusters.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxTotalReplicas *int32 `json:"maxTotalReplicas,omitempty"`
}

// OverflowClusterStatus tracks the state of an individual overflow cluster.
type OverflowClusterStatus struct {
	// Name matches the ClusterRef name.
	Name string `json:"name"`

	// Replicas is the current replica count on this cluster.
	Replicas int32 `json:"replicas"`

	// Healthy indicates whether the cluster's API server is reachable.
	Healthy bool `json:"healthy"`

	// LastProbeTime is the last time the cluster was health-checked.
	// +optional
	LastProbeTime *metav1.Time `json:"lastProbeTime,omitempty"`

	// LastSpillTime is the last time workloads were spilled to this cluster.
	// +optional
	LastSpillTime *metav1.Time `json:"lastSpillTime,omitempty"`
}

// FederatedScaledObjectStatus defines the observed state of FederatedScaledObject.
type FederatedScaledObjectStatus struct {
	// PrimaryReplicas is the current replica count on the primary cluster.
	PrimaryReplicas int32 `json:"primaryReplicas,omitempty"`

	// TotalReplicas is the sum across all clusters.
	TotalReplicas int32 `json:"totalReplicas,omitempty"`

	// CurrentMetricValue is the latest observed metric value.
	// +optional
	CurrentMetricValue string `json:"currentMetricValue,omitempty"`

	// SpilloverActive indicates whether any overflow clusters have replicas.
	SpilloverActive bool `json:"spilloverActive,omitempty"`

	// OverflowClusters reports per-cluster overflow state.
	// +optional
	OverflowClusters []OverflowClusterStatus `json:"overflowClusters,omitempty"`

	// LastScaleTime is the timestamp of the last scaling action.
	// +optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Primary",type=string,JSONPath=`.spec.primaryCluster.name`
// +kubebuilder:printcolumn:name="PrimaryReplicas",type=integer,JSONPath=`.status.primaryReplicas`
// +kubebuilder:printcolumn:name="TotalReplicas",type=integer,JSONPath=`.status.totalReplicas`
// +kubebuilder:printcolumn:name="Spillover",type=boolean,JSONPath=`.status.spilloverActive`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// FederatedScaledObject defines a multi-cluster workload federation policy
// that monitors a queue depth metric on the primary cluster and spills
// workloads to overflow clusters when the threshold is exceeded.
type FederatedScaledObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FederatedScaledObjectSpec   `json:"spec,omitempty"`
	Status FederatedScaledObjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FederatedScaledObjectList contains a list of FederatedScaledObject.
type FederatedScaledObjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FederatedScaledObject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FederatedScaledObject{}, &FederatedScaledObjectList{})
}

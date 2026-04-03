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
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/multicluster"
)

// FederatedScaledObjectReconciler reconciles a FederatedScaledObject object.
// It monitors a metric on the primary cluster and spills workloads to
// overflow clusters when the threshold is exceeded.
type FederatedScaledObjectReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	Clock                Clock
	ClusterRegistry      multicluster.Registry
	MetricQuerierFactory func(address string) (MetricQuerier, error)
}

//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=federatedscaledobjects,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=federatedscaledobjects/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=federatedscaledobjects/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch

// Reconcile monitors the spillover metric, manages overflow cluster registration,
// and scales workloads across clusters when the threshold is crossed.
func (r *FederatedScaledObjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var fso autoscalingv1alpha1.FederatedScaledObject
	if err := r.Get(ctx, req.NamespacedName, &fso); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	clk := r.Clock
	if clk == nil {
		clk = realClock{}
	}
	now := clk.Now()

	// Ensure overflow clusters are registered in the cluster registry.
	if err := r.ensureClustersRegistered(ctx, fso); err != nil {
		logger.Error(err, "failed to register overflow clusters")
	}

	// Query the spillover metric.
	var currentValue float64
	if r.MetricQuerierFactory != nil {
		querier, err := r.MetricQuerierFactory(fso.Spec.Metric.PrometheusAddress)
		if err != nil {
			logger.Error(err, "failed to create metric querier")
			return r.updateFSOStatusError(ctx, &fso, now, "MetricQuerierFailed", err.Error())
		}

		currentValue, err = querier.InstantQuery(ctx, fso.Spec.Metric.Query)
		if err != nil {
			logger.Error(err, "failed to query spillover metric")
			return r.updateFSOStatusError(ctx, &fso, now, "MetricQueryFailed", err.Error())
		}
	}

	threshold, _ := strconv.ParseFloat(fso.Spec.Metric.ThresholdValue, 64)

	// Read primary deployment replicas.
	var primaryDeploy appsv1.Deployment
	primaryKey := types.NamespacedName{
		Name:      fso.Spec.Workload.DeploymentName,
		Namespace: fso.Spec.Workload.Namespace,
	}
	primaryReplicas := int32(0)
	if err := r.Get(ctx, primaryKey, &primaryDeploy); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("fetching primary deployment: %w", err)
		}
		logger.Info("primary deployment not found", "deployment", primaryKey)
	} else {
		if primaryDeploy.Spec.Replicas != nil {
			primaryReplicas = *primaryDeploy.Spec.Replicas
		}
	}

	// Check cluster-wide scaling policy (blackouts, global dry-run).
	guard := &ScaleGuard{Reader: r.Client}
	scalePolicy := guard.Check(ctx)

	if reason := scalePolicy.ShouldSuppress(); reason != "" {
		logger.Info("spillover suppressed by ClusterScaleProfile",
			"reason", reason,
			"metric", currentValue,
			"threshold", threshold)
	}

	// Determine if spillover is needed.
	spilloverNeeded := currentValue > threshold && !scalePolicy.Blocked && !scalePolicy.GlobalDryRun

	// Check cooldown — prevent thrashing.
	inCooldown := false
	if fso.Status.LastScaleTime != nil {
		elapsed := now.Sub(fso.Status.LastScaleTime.Time)
		if elapsed < time.Duration(fso.Spec.CooldownSeconds)*time.Second {
			inCooldown = true
		}
	}

	// Collect per-cluster status.
	overflowStatuses := make([]autoscalingv1alpha1.OverflowClusterStatus, 0, len(fso.Spec.OverflowClusters))
	totalReplicas := primaryReplicas
	spilloverActive := false

	for _, oc := range fso.Spec.OverflowClusters {
		status := autoscalingv1alpha1.OverflowClusterStatus{
			Name:     oc.Name,
			Replicas: 0,
			Healthy:  false,
		}

		if r.ClusterRegistry != nil {
			entry, found := r.ClusterRegistry.Get(oc.Name)
			if found {
				status.Healthy = entry.Healthy
				probeMeta := metav1.NewTime(entry.LastProbe)
				status.LastProbeTime = &probeMeta
			}
		}

		// If spillover needed and cluster is healthy, scale up on overflow.
		if spilloverNeeded && status.Healthy && !inCooldown {
			desiredOverflow := int32(1)
			if oc.MaxCapacity != nil && desiredOverflow > *oc.MaxCapacity {
				desiredOverflow = *oc.MaxCapacity
			}

			// Respect max total replicas.
			if fso.Spec.MaxTotalReplicas != nil {
				remaining := *fso.Spec.MaxTotalReplicas - totalReplicas
				if remaining <= 0 {
					desiredOverflow = 0
				} else if desiredOverflow > remaining {
					desiredOverflow = remaining
				}
			}

			if desiredOverflow > 0 && r.ClusterRegistry != nil {
				if err := r.applyOverflowDeployment(ctx, fso, oc, desiredOverflow); err != nil {
					logger.Error(err, "failed to apply overflow deployment", "cluster", oc.Name)
				} else {
					status.Replicas = desiredOverflow
					spillMeta := metav1.NewTime(now)
					status.LastSpillTime = &spillMeta
					spilloverActive = true
				}
			}
		} else if !spilloverNeeded && status.Healthy && !inCooldown {
			// Scale down overflow when metric is below threshold.
			if err := r.scaleDownOverflow(ctx, fso, oc); err != nil {
				logger.Error(err, "failed to scale down overflow", "cluster", oc.Name)
			}
		}

		totalReplicas += status.Replicas
		overflowStatuses = append(overflowStatuses, status)
	}

	// Update status.
	nowMeta := metav1.NewTime(now)
	fso.Status.PrimaryReplicas = primaryReplicas
	fso.Status.TotalReplicas = totalReplicas
	fso.Status.CurrentMetricValue = fmt.Sprintf("%.2f", currentValue)
	fso.Status.SpilloverActive = spilloverActive
	fso.Status.OverflowClusters = overflowStatuses

	if spilloverNeeded && !inCooldown {
		fso.Status.LastScaleTime = &nowMeta
	}

	readyCondition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: fso.Generation,
		LastTransitionTime: nowMeta,
		Reason:             "Reconciled",
		Message: fmt.Sprintf("metric=%.2f threshold=%.2f spillover=%v primary=%d total=%d",
			currentValue, threshold, spilloverActive, primaryReplicas, totalReplicas),
	}
	setCondition(&fso.Status.Conditions, readyCondition)

	if err := r.Status().Update(ctx, &fso); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating FederatedScaledObject status: %w", err)
	}

	logger.Info("reconciled FederatedScaledObject",
		"metric", currentValue,
		"threshold", threshold,
		"spillover", spilloverActive,
		"totalReplicas", totalReplicas)

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// ensureClustersRegistered reads kubeconfig Secrets and registers each
// overflow cluster in the ClusterRegistry.
func (r *FederatedScaledObjectReconciler) ensureClustersRegistered(ctx context.Context, fso autoscalingv1alpha1.FederatedScaledObject) error {
	if r.ClusterRegistry == nil {
		return nil
	}

	allClusters := append([]autoscalingv1alpha1.ClusterRef{fso.Spec.PrimaryCluster}, fso.Spec.OverflowClusters...)

	for _, ref := range allClusters {
		if _, found := r.ClusterRegistry.Get(ref.Name); found {
			continue
		}

		var secret corev1.Secret
		secretKey := types.NamespacedName{
			Name:      ref.SecretRef.Name,
			Namespace: ref.SecretRef.Namespace,
		}

		if err := r.Get(ctx, secretKey, &secret); err != nil {
			return fmt.Errorf("fetching kubeconfig secret for cluster %s: %w", ref.Name, err)
		}

		kubeconfigData, ok := secret.Data["kubeconfig"]
		if !ok {
			return fmt.Errorf("secret %s missing 'kubeconfig' key", secretKey)
		}

		if err := r.ClusterRegistry.Register(ctx, ref.Name, kubeconfigData, r.Scheme); err != nil {
			return fmt.Errorf("registering cluster %s: %w", ref.Name, err)
		}
	}

	return nil
}

// applyOverflowDeployment creates or patches a Deployment on an overflow
// cluster using server-side apply with FieldManager "scalepilot".
func (r *FederatedScaledObjectReconciler) applyOverflowDeployment(
	ctx context.Context,
	fso autoscalingv1alpha1.FederatedScaledObject,
	clusterRef autoscalingv1alpha1.ClusterRef,
	replicas int32,
) error {
	if r.ClusterRegistry == nil {
		return fmt.Errorf("cluster registry not configured")
	}

	entry, found := r.ClusterRegistry.Get(clusterRef.Name)
	if !found {
		return fmt.Errorf("cluster %s not found in registry", clusterRef.Name)
	}

	targetNS := clusterRef.Namespace
	if targetNS == "" {
		targetNS = fso.Spec.Workload.Namespace
	}

	// Read the primary deployment to replicate its pod spec.
	var primaryDeploy appsv1.Deployment
	primaryKey := types.NamespacedName{
		Name:      fso.Spec.Workload.DeploymentName,
		Namespace: fso.Spec.Workload.Namespace,
	}
	if err := r.Get(ctx, primaryKey, &primaryDeploy); err != nil {
		return fmt.Errorf("reading primary deployment for replication: %w", err)
	}

	overflowDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-overflow", fso.Spec.Workload.DeploymentName),
			Namespace: targetNS,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "scalepilot",
				"scalepilot.io/fso":            fso.Name,
				"scalepilot.io/source-cluster": fso.Spec.PrimaryCluster.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: primaryDeploy.Spec.Selector,
			Template: primaryDeploy.Spec.Template,
		},
	}

	// Use server-side apply for idempotent, conflict-free writes.
	if err := entry.Client.Patch(ctx, overflowDeploy,
		client.Apply,
		client.FieldOwner("scalepilot"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("applying overflow deployment to cluster %s: %w", clusterRef.Name, err)
	}

	return nil
}

// scaleDownOverflow sets the overflow deployment replicas to 0 on the
// specified cluster.
func (r *FederatedScaledObjectReconciler) scaleDownOverflow(
	ctx context.Context,
	fso autoscalingv1alpha1.FederatedScaledObject,
	clusterRef autoscalingv1alpha1.ClusterRef,
) error {
	if r.ClusterRegistry == nil {
		return nil
	}

	entry, found := r.ClusterRegistry.Get(clusterRef.Name)
	if !found {
		return nil
	}

	targetNS := clusterRef.Namespace
	if targetNS == "" {
		targetNS = fso.Spec.Workload.Namespace
	}

	deployName := fmt.Sprintf("%s-overflow", fso.Spec.Workload.DeploymentName)
	var overflowDeploy appsv1.Deployment
	key := types.NamespacedName{Name: deployName, Namespace: targetNS}

	if err := entry.Client.Get(ctx, key, &overflowDeploy); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("fetching overflow deployment on cluster %s: %w", clusterRef.Name, err)
	}

	zero := int32(0)
	overflowDeploy.Spec.Replicas = &zero
	if err := entry.Client.Update(ctx, &overflowDeploy); err != nil {
		return fmt.Errorf("scaling down overflow on cluster %s: %w", clusterRef.Name, err)
	}

	return nil
}

func (r *FederatedScaledObjectReconciler) updateFSOStatusError(ctx context.Context, fso *autoscalingv1alpha1.FederatedScaledObject, now time.Time, reason, message string) (ctrl.Result, error) {
	nowMeta := metav1.NewTime(now)

	errCondition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: fso.Generation,
		LastTransitionTime: nowMeta,
		Reason:             reason,
		Message:            message,
	}
	setCondition(&fso.Status.Conditions, errCondition)

	if err := r.Status().Update(ctx, fso); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating FederatedScaledObject error status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FederatedScaledObjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.FederatedScaledObject{}).
		Complete(r)
}

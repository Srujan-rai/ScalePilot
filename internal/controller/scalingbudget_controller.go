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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/cloudcost"
	"github.com/srujan-rai/scalepilot/pkg/webhook"
)

// CostQuerierFactory builds a CostQuerier from a ScalingBudget's cloud config.
type CostQuerierFactory func(config autoscalingv1alpha1.CloudCostConfig) (cloudcost.CostQuerier, error)

// ScalingBudgetReconciler reconciles a ScalingBudget object.
// It polls cloud cost APIs, computes utilization, and enforces breach actions.
type ScalingBudgetReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Clock               Clock
	CostQuerierFactory  CostQuerierFactory
	NotificationSenders []webhook.Sender
}

//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=scalingbudgets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=scalingbudgets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=scalingbudgets/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile polls the cloud cost API, updates spend status, and triggers
// breach actions and notifications when spend exceeds thresholds.
func (r *ScalingBudgetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var budget autoscalingv1alpha1.ScalingBudget
	if err := r.Get(ctx, req.NamespacedName, &budget); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	clk := r.Clock
	if clk == nil {
		clk = realClock{}
	}
	now := clk.Now()

	// Build the cost querier from config.
	var costData *cloudcost.CostData
	if r.CostQuerierFactory != nil {
		querier, err := r.CostQuerierFactory(budget.Spec.CloudCost)
		if err != nil {
			logger.Error(err, "failed to create cost querier")
			return r.updateBudgetStatusError(ctx, &budget, now, "CostQuerierFailed", err.Error())
		}

		costData, err = querier.GetCurrentCost(ctx, budget.Spec.Namespace)
		if err != nil {
			logger.Error(err, "failed to fetch cost data", "namespace", budget.Spec.Namespace)
			return r.updateBudgetStatusError(ctx, &budget, now, "CostFetchFailed", err.Error())
		}
	} else {
		logger.Info("no CostQuerierFactory configured, using current status values")
		costData = &cloudcost.CostData{
			CurrentSpendMillidollars: budget.Status.CurrentSpendMillidollars,
			FetchedAt:               now,
		}
	}

	// Compute utilization.
	ceiling := budget.Spec.CeilingMillidollars
	spend := costData.CurrentSpendMillidollars
	utilization := 0
	if ceiling > 0 {
		utilization = int((spend * 100) / ceiling)
	}
	breached := spend >= ceiling

	wasBelowWarning := budget.Status.UtilizationPercent < budget.Spec.WarningThresholdPercent
	wasNotBreached := !budget.Status.Breached

	nowMeta := metav1.NewTime(now)
	budget.Status.CurrentSpendMillidollars = spend
	budget.Status.UtilizationPercent = utilization
	budget.Status.Breached = breached
	budget.Status.LastCheckedAt = &nowMeta

	// Send warning notification when crossing the threshold.
	if utilization >= budget.Spec.WarningThresholdPercent && wasBelowWarning {
		logger.Info("budget warning threshold crossed",
			"namespace", budget.Spec.Namespace,
			"utilization", utilization,
			"threshold", budget.Spec.WarningThresholdPercent)
		r.sendNotification(ctx, budget, webhook.SeverityWarning,
			"Budget Warning",
			fmt.Sprintf("Namespace %s is at %d%% of its $%.2f budget ceiling",
				budget.Spec.Namespace, utilization, float64(ceiling)/1000.0))
	}

	// Handle breach.
	if breached && wasNotBreached {
		logger.Info("budget ceiling BREACHED",
			"namespace", budget.Spec.Namespace,
			"spend", spend,
			"ceiling", ceiling,
			"action", budget.Spec.BreachAction)
		r.sendNotification(ctx, budget, webhook.SeverityCritical,
			"Budget Breached",
			fmt.Sprintf("Namespace %s exceeded its $%.2f ceiling (current: $%.2f). Action: %s",
				budget.Spec.Namespace, float64(ceiling)/1000.0,
				float64(spend)/1000.0, budget.Spec.BreachAction))
	}

	// Set conditions.
	costCondition := metav1.Condition{
		Type:               "CostFetched",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: budget.Generation,
		LastTransitionTime: nowMeta,
		Reason:             "CostDataFetched",
		Message:            fmt.Sprintf("Current spend: $%.2f / $%.2f (%d%%)", float64(spend)/1000.0, float64(ceiling)/1000.0, utilization),
	}
	setCondition(&budget.Status.Conditions, costCondition)

	breachStatus := metav1.ConditionFalse
	breachReason := "WithinBudget"
	breachMsg := fmt.Sprintf("Spend at %d%% of ceiling", utilization)
	if breached {
		breachStatus = metav1.ConditionTrue
		breachReason = "BudgetExceeded"
		breachMsg = fmt.Sprintf("Breach action: %s", budget.Spec.BreachAction)
	}
	breachCondition := metav1.Condition{
		Type:               "Breached",
		Status:             breachStatus,
		ObservedGeneration: budget.Generation,
		LastTransitionTime: nowMeta,
		Reason:             breachReason,
		Message:            breachMsg,
	}
	setCondition(&budget.Status.Conditions, breachCondition)

	if err := r.Status().Update(ctx, &budget); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating ScalingBudget status: %w", err)
	}

	requeueAfter := time.Duration(budget.Spec.PollIntervalMinutes) * time.Minute
	logger.Info("reconciled ScalingBudget",
		"namespace", budget.Spec.Namespace,
		"utilization", utilization,
		"breached", breached,
		"nextPoll", requeueAfter)

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *ScalingBudgetReconciler) updateBudgetStatusError(ctx context.Context, budget *autoscalingv1alpha1.ScalingBudget, now time.Time, reason, message string) (ctrl.Result, error) {
	nowMeta := metav1.NewTime(now)
	budget.Status.LastCheckedAt = &nowMeta

	errCondition := metav1.Condition{
		Type:               "CostFetched",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: budget.Generation,
		LastTransitionTime: nowMeta,
		Reason:             reason,
		Message:            message,
	}
	setCondition(&budget.Status.Conditions, errCondition)

	if err := r.Status().Update(ctx, budget); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating ScalingBudget error status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func (r *ScalingBudgetReconciler) sendNotification(ctx context.Context, budget autoscalingv1alpha1.ScalingBudget, severity webhook.Severity, title, message string) {
	logger := log.FromContext(ctx)

	alert := webhook.Alert{
		Title:     title,
		Message:   message,
		Severity:  severity,
		Namespace: budget.Spec.Namespace,
		Resource:  fmt.Sprintf("ScalingBudget/%s", budget.Name),
		Timestamp: time.Now(),
	}

	for _, sender := range r.NotificationSenders {
		if err := sender.Send(ctx, alert); err != nil {
			logger.Error(err, "failed to send notification", "sender", sender.Name())
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalingBudgetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.ScalingBudget{}).
		Complete(r)
}

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
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// realClock delegates to time.Now.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// ClusterScaleProfileReconciler reconciles a ClusterScaleProfile object.
// It evaluates blackout windows, counts team overrides, and updates status.
type ClusterScaleProfileReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Clock  Clock
}

//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=clusterscaleprofiles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=clusterscaleprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.scalepilot.io,resources=clusterscaleprofiles/finalizers,verbs=update

// Reconcile evaluates blackout windows against the current time and updates
// status fields. It requeues every 30 seconds to keep blackout state fresh.
func (r *ClusterScaleProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var profile autoscalingv1alpha1.ClusterScaleProfile
	if err := r.Get(ctx, req.NamespacedName, &profile); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	clk := r.Clock
	if clk == nil {
		clk = realClock{}
	}
	now := clk.Now()

	activeBlackout := false
	activeBlackoutName := ""

	for _, bw := range profile.Spec.BlackoutWindows {
		active, err := isInBlackoutWindow(now, bw)
		if err != nil {
			logger.Error(err, "failed to evaluate blackout window", "window", bw.Name)
			continue
		}
		if active {
			activeBlackout = true
			activeBlackoutName = bw.Name
			logger.Info("blackout window active — scaling suppressed",
				"window", bw.Name,
				"dryRun", profile.Spec.EnableGlobalDryRun)
			break
		}
	}

	nowMeta := metav1.NewTime(now)
	profile.Status.ActiveBlackout = activeBlackout
	profile.Status.ActiveBlackoutName = activeBlackoutName
	profile.Status.TeamsConfigured = int32(len(profile.Spec.TeamOverrides))
	profile.Status.LastReconcileTime = &nowMeta

	readyCondition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: profile.Generation,
		LastTransitionTime: nowMeta,
		Reason:             "Reconciled",
		Message:            fmt.Sprintf("Profile reconciled: %d teams, blackout=%v", len(profile.Spec.TeamOverrides), activeBlackout),
	}
	setCondition(&profile.Status.Conditions, readyCondition)

	if err := r.Status().Update(ctx, &profile); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating ClusterScaleProfile status: %w", err)
	}

	logger.Info("reconciled ClusterScaleProfile",
		"teams", len(profile.Spec.TeamOverrides),
		"blackout", activeBlackout,
		"globalDryRun", profile.Spec.EnableGlobalDryRun)

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterScaleProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.ClusterScaleProfile{}).
		Complete(r)
}

// isInBlackoutWindow checks whether the given time falls within a blackout window.
// The start and end fields use 5-field cron syntax. We check whether `now` is
// between the most recent start trigger and the most recent end trigger.
func isInBlackoutWindow(now time.Time, bw autoscalingv1alpha1.BlackoutWindow) (bool, error) {
	tz := bw.Timezone
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false, fmt.Errorf("loading timezone %q: %w", tz, err)
	}

	localNow := now.In(loc)

	startMatch, err := cronMatchesRecently(localNow, bw.Start)
	if err != nil {
		return false, fmt.Errorf("parsing start cron %q: %w", bw.Start, err)
	}

	endMatch, err := cronMatchesRecently(localNow, bw.End)
	if err != nil {
		return false, fmt.Errorf("parsing end cron %q: %w", bw.End, err)
	}

	// If start triggered more recently than end, we're in the blackout.
	if startMatch.IsZero() {
		return false, nil
	}
	if endMatch.IsZero() {
		return true, nil
	}
	return startMatch.After(endMatch), nil
}

// cronMatchesRecently finds the most recent time (within 7 days) where the
// cron expression would have triggered. This is a simplified cron evaluator
// that checks minute-by-minute going backwards from `now`.
func cronMatchesRecently(now time.Time, cronExpr string) (time.Time, error) {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("expected 5 cron fields, got %d in %q", len(fields), cronExpr)
	}

	// Walk backwards minute-by-minute up to 7 days.
	limit := 7 * 24 * 60
	candidate := now.Truncate(time.Minute)
	for i := 0; i < limit; i++ {
		if cronFieldMatches(fields[0], candidate.Minute()) &&
			cronFieldMatches(fields[1], candidate.Hour()) &&
			cronFieldMatches(fields[2], candidate.Day()) &&
			cronFieldMatches(fields[3], int(candidate.Month())) &&
			cronFieldMatches(fields[4], int(candidate.Weekday())) {
			return candidate, nil
		}
		candidate = candidate.Add(-time.Minute)
	}

	return time.Time{}, nil
}

// cronFieldMatches checks if a single cron field matches a value.
// Supports: "*", exact numbers, and comma-separated lists.
func cronFieldMatches(field string, value int) bool {
	if field == "*" {
		return true
	}

	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if n, err := strconv.Atoi(part); err == nil && n == value {
			return true
		}
	}
	return false
}

// setCondition updates or appends a condition in the slice.
func setCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	if conditions == nil {
		return
	}
	for i, existing := range *conditions {
		if existing.Type == condition.Type {
			if existing.Status != condition.Status {
				condition.LastTransitionTime = metav1.NewTime(time.Now())
			} else {
				condition.LastTransitionTime = existing.LastTransitionTime
			}
			(*conditions)[i] = condition
			return
		}
	}
	*conditions = append(*conditions, condition)
}

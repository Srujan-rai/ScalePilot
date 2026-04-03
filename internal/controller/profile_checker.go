package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

// ScaleGuard checks whether scaling operations should be suppressed based on
// active ClusterScaleProfile settings (blackout windows, global dry-run).
type ScaleGuard struct {
	client.Reader
}

// ScalePolicy captures the cluster-wide scaling constraints.
type ScalePolicy struct {
	Blocked         bool
	BlackoutName    string
	GlobalDryRun    bool
	MaxSurgePercent int32
}

// Check returns the active ScalePolicy by examining all ClusterScaleProfiles.
// If no profiles exist, returns a permissive policy.
func (g *ScaleGuard) Check(ctx context.Context) ScalePolicy {
	logger := log.FromContext(ctx)
	policy := ScalePolicy{MaxSurgePercent: 100}

	var profiles autoscalingv1alpha1.ClusterScaleProfileList
	if err := g.List(ctx, &profiles); err != nil {
		logger.Error(err, "failed to list ClusterScaleProfiles, allowing scaling")
		return policy
	}

	for _, p := range profiles.Items {
		if p.Status.ActiveBlackout {
			policy.Blocked = true
			policy.BlackoutName = p.Status.ActiveBlackoutName
		}
		if p.Spec.EnableGlobalDryRun {
			policy.GlobalDryRun = true
		}
		if p.Spec.MaxSurgePercent > 0 && p.Spec.MaxSurgePercent < policy.MaxSurgePercent {
			policy.MaxSurgePercent = p.Spec.MaxSurgePercent
		}
	}

	return policy
}

// ShouldSuppress returns a human-readable reason if scaling should be suppressed,
// or empty string if scaling is allowed.
func (p ScalePolicy) ShouldSuppress() string {
	if p.Blocked {
		return fmt.Sprintf("blackout window %q is active", p.BlackoutName)
	}
	if p.GlobalDryRun {
		return "global dry-run is enabled"
	}
	return ""
}

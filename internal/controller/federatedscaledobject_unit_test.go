package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/forecast"
	"github.com/srujan-rai/scalepilot/pkg/multicluster"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func fsoScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = autoscalingv1alpha1.AddToScheme(s)
	return s
}

func fsoClient(s *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&autoscalingv1alpha1.FederatedScaledObject{}).
		Build()
}

func newFSO(name string, threshold string, cooldown int32, overflow ...autoscalingv1alpha1.ClusterRef) *autoscalingv1alpha1.FederatedScaledObject {
	return &autoscalingv1alpha1.FederatedScaledObject{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: autoscalingv1alpha1.FederatedScaledObjectSpec{
			PrimaryCluster: autoscalingv1alpha1.ClusterRef{
				Name:      "primary",
				SecretRef: autoscalingv1alpha1.SecretReference{Name: "primary-secret", Namespace: "default"},
			},
			OverflowClusters: overflow,
			Metric: autoscalingv1alpha1.SpilloverMetric{
				Query:             "queue_depth",
				PrometheusAddress: "http://prometheus:9090",
				ThresholdValue:    threshold,
			},
			Workload: autoscalingv1alpha1.WorkloadTemplate{
				DeploymentName: "worker",
				Namespace:      "default",
			},
			CooldownSeconds: cooldown,
		},
	}
}

func overflowCluster(name string, priority int32, maxCap *int32) autoscalingv1alpha1.ClusterRef {
	return autoscalingv1alpha1.ClusterRef{
		Name:        name,
		SecretRef:   autoscalingv1alpha1.SecretReference{Name: name + "-secret", Namespace: "default"},
		Priority:    priority,
		MaxCapacity: maxCap,
	}
}

func int32p(v int32) *int32 { return &v }

func primaryDeploy(replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}},
		},
	}
}

// fakeClock returns a fixed time so cooldown calculations are deterministic.
type fakeClock struct{ t time.Time }

func (f fakeClock) Now() time.Time { return f.t }

// fsoMetricQuerier returns a fixed value for InstantQuery.
type fsoMetricQuerier struct {
	value float64
	err   error
}

func (q fsoMetricQuerier) InstantQuery(_ context.Context, _ string) (float64, error) {
	return q.value, q.err
}
func (q fsoMetricQuerier) RangeQuery(_ context.Context, _ string, _, _ time.Time, _ time.Duration) ([]forecast.DataPoint, error) {
	return nil, fmt.Errorf("not used")
}

// fsoFakeRegistry is a simple in-memory Registry for testing.
type fsoFakeRegistry struct {
	entries map[string]*multicluster.ClusterEntry
}

func newFSORegistry(entries ...*multicluster.ClusterEntry) *fsoFakeRegistry {
	r := &fsoFakeRegistry{entries: make(map[string]*multicluster.ClusterEntry)}
	for _, e := range entries {
		r.entries[e.Name] = e
	}
	return r
}

func (r *fsoFakeRegistry) Register(_ context.Context, name string, _ []byte, _ *runtime.Scheme, namespace string, maxCapacity *int32, priority int32) error {
	r.entries[name] = &multicluster.ClusterEntry{
		Name: name, Healthy: true,
		Namespace: namespace, MaxCapacity: maxCapacity, Priority: priority,
	}
	return nil
}
func (r *fsoFakeRegistry) Unregister(name string)                     { delete(r.entries, name) }
func (r *fsoFakeRegistry) RunHealthChecks(_ context.Context)          {}
func (r *fsoFakeRegistry) List() []*multicluster.ClusterEntry         { return nil }
func (r *fsoFakeRegistry) HealthyOverflow() []*multicluster.ClusterEntry { return nil }

func (r *fsoFakeRegistry) Get(name string) (*multicluster.ClusterEntry, bool) {
	e, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	cp := *e
	return &cp, true
}

// clusterEntry builds a ClusterEntry whose Client is a fake k8s client
// pre-populated with the given objects on the overflow cluster.
func clusterEntry(name string, healthy bool, priority int32, maxCap *int32, overflowObjs ...client.Object) *multicluster.ClusterEntry {
	s := fsoScheme()
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(overflowObjs...).Build()
	return &multicluster.ClusterEntry{
		Name:        name,
		Client:      c,
		Healthy:     healthy,
		Priority:    priority,
		MaxCapacity: maxCap,
	}
}

func reconcilerFor(mainClient client.Client, registry multicluster.Registry, metricVal float64, metricErr error, clk Clock) *FederatedScaledObjectReconciler {
	return &FederatedScaledObjectReconciler{
		Client:          mainClient,
		Scheme:          mainClient.Scheme(),
		Clock:           clk,
		ClusterRegistry: registry,
		MetricQuerierFactory: func(_ string) (MetricQuerier, error) {
			return fsoMetricQuerier{value: metricVal, err: metricErr}, metricErr
		},
	}
}

func doReconcile(t *testing.T, r *FederatedScaledObjectReconciler, name string) autoscalingv1alpha1.FederatedScaledObject {
	t.Helper()
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var fso autoscalingv1alpha1.FederatedScaledObject
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &fso); err != nil {
		t.Fatalf("Get FSO after reconcile: %v", err)
	}
	return fso
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestFSO_BelowThreshold_NoSpillover(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-1", "100", 60, overflowCluster("oc-1", 0, nil))
	c := fsoClient(s, fso)

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, nil))
	r := reconcilerFor(c, reg, 40.0, nil, nil) // metric=40, threshold=100

	updated := doReconcile(t, r, "fso-1")

	if updated.Status.SpilloverActive {
		t.Error("expected SpilloverActive=false when metric below threshold")
	}
	if updated.Status.CurrentMetricValue != "40.00" {
		t.Errorf("unexpected CurrentMetricValue: %s", updated.Status.CurrentMetricValue)
	}
}

func TestFSO_AboveThreshold_SpilloverDeployed(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-2", "50", 60, overflowCluster("oc-1", 0, nil))
	c := fsoClient(s, fso, primaryDeploy(30))

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, nil))
	r := reconcilerFor(c, reg, 80.0, nil, nil) // metric=80, threshold=50, primary=30 → need 50 more

	updated := doReconcile(t, r, "fso-2")

	if !updated.Status.SpilloverActive {
		t.Error("expected SpilloverActive=true when metric exceeds threshold")
	}
	if len(updated.Status.OverflowClusters) == 0 {
		t.Fatal("expected at least one overflow cluster status")
	}
	if updated.Status.OverflowClusters[0].Replicas == 0 {
		t.Error("expected overflow cluster to have replicas > 0")
	}
}

func TestFSO_UnhealthyCluster_NotUsed(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-3", "10", 60, overflowCluster("oc-unhealthy", 0, nil))
	c := fsoClient(s, fso, primaryDeploy(5))

	reg := newFSORegistry(clusterEntry("oc-unhealthy", false, 0, nil)) // unhealthy
	r := reconcilerFor(c, reg, 80.0, nil, nil)

	updated := doReconcile(t, r, "fso-3")

	if updated.Status.SpilloverActive {
		t.Error("expected no spillover when only cluster is unhealthy")
	}
	if updated.Status.OverflowClusters[0].Replicas != 0 {
		t.Error("expected 0 replicas on unhealthy cluster")
	}
}

func TestFSO_Cooldown_BlocksSpillover(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-4", "10", 120, overflowCluster("oc-1", 0, nil))

	// Simulate a recent scale — set LastScaleTime to 30 seconds ago (within 120s cooldown).
	recentScale := metav1.NewTime(time.Now().Add(-30 * time.Second))
	fso.Status.LastScaleTime = &recentScale
	c := fsoClient(s, fso, primaryDeploy(5))

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, nil))
	r := reconcilerFor(c, reg, 999.0, nil, nil) // metric way above threshold

	updated := doReconcile(t, r, "fso-4")

	if updated.Status.SpilloverActive {
		t.Error("expected spillover to be blocked by cooldown")
	}
}

func TestFSO_Cooldown_OnlyTriggersOnSuccess(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-5", "10", 120, overflowCluster("oc-1", 0, nil))
	c := fsoClient(s, fso, primaryDeploy(5))

	// Entry with nil Client — applyOverflowDeployment returns an error, so
	// spilloverActive stays false and LastScaleTime must not be set.
	nilClientEntry := &multicluster.ClusterEntry{Name: "oc-1", Healthy: true, Client: nil}
	reg := newFSORegistry(nilClientEntry)

	r := &FederatedScaledObjectReconciler{
		Client:          c,
		Scheme:          s,
		ClusterRegistry: reg,
		MetricQuerierFactory: func(_ string) (MetricQuerier, error) {
			return fsoMetricQuerier{value: 999}, nil
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "fso-5", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}

	var updated autoscalingv1alpha1.FederatedScaledObject
	_ = r.Client.Get(context.Background(), types.NamespacedName{Name: "fso-5", Namespace: "default"}, &updated)

	if updated.Status.LastScaleTime != nil {
		t.Error("expected LastScaleTime to remain nil when all scale operations failed")
	}
}

func TestFSO_MaxCapacity_CapsReplicas(t *testing.T) {
	s := fsoScheme()
	cap := int32(5)
	fso := newFSO("fso-6", "10", 60, overflowCluster("oc-1", 0, &cap))
	c := fsoClient(s, fso, primaryDeploy(8))

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, &cap))
	r := reconcilerFor(c, reg, 100.0, nil, nil) // needs 92 overflow, but capped at 5

	updated := doReconcile(t, r, "fso-6")

	if !updated.Status.SpilloverActive {
		t.Fatal("expected spillover active")
	}
	if updated.Status.OverflowClusters[0].Replicas > 5 {
		t.Errorf("expected replicas <= 5 (MaxCapacity), got %d", updated.Status.OverflowClusters[0].Replicas)
	}
}

func TestFSO_MaxTotalReplicas_CapsAcrossClusters(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-7", "10", 60,
		overflowCluster("oc-1", 1, int32p(20)),
		overflowCluster("oc-2", 2, int32p(20)),
	)
	maxTotal := int32(15)
	fso.Spec.MaxTotalReplicas = &maxTotal

	c := fsoClient(s, fso, primaryDeploy(5))

	reg := newFSORegistry(
		clusterEntry("oc-1", true, 1, int32p(20)),
		clusterEntry("oc-2", true, 2, int32p(20)),
	)
	r := reconcilerFor(c, reg, 100.0, nil, nil)

	updated := doReconcile(t, r, "fso-7")

	total := updated.Status.TotalReplicas
	if total > 15 {
		t.Errorf("expected TotalReplicas <= 15 (MaxTotalReplicas), got %d", total)
	}
}

func TestFSO_ScaleDown_WhenMetricDrops(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-8", "100", 60, overflowCluster("oc-1", 0, nil))

	// Simulate no recent scale (cooldown not in effect).
	c := fsoClient(s, fso)

	// Pre-populate overflow cluster with an existing overflow deployment to scale down.
	existingOverflow := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-overflow", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32p(10),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}},
		},
	}
	overflowClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existingOverflow).Build()
	entry := &multicluster.ClusterEntry{Name: "oc-1", Healthy: true, Client: overflowClient}
	reg := newFSORegistry(entry)

	r := reconcilerFor(c, reg, 40.0, nil, nil) // metric=40 < threshold=100 → scale down

	updated := doReconcile(t, r, "fso-8")

	if updated.Status.SpilloverActive {
		t.Error("expected SpilloverActive=false after scale-down")
	}

	// Verify the overflow deployment was scaled to 0 on the overflow cluster.
	var overflowDeploy appsv1.Deployment
	err := overflowClient.Get(context.Background(),
		types.NamespacedName{Name: "worker-overflow", Namespace: "default"}, &overflowDeploy)
	if err != nil {
		t.Fatalf("overflow deployment not found: %v", err)
	}
	if overflowDeploy.Spec.Replicas == nil || *overflowDeploy.Spec.Replicas != 0 {
		t.Errorf("expected overflow deployment replicas=0, got %v", overflowDeploy.Spec.Replicas)
	}
}

func TestFSO_InvalidThreshold_ErrorCondition(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-9", "not-a-number", 60, overflowCluster("oc-1", 0, nil))
	c := fsoClient(s, fso)

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, nil))
	r := reconcilerFor(c, reg, 50.0, nil, nil)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "fso-9", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}

	var updated autoscalingv1alpha1.FederatedScaledObject
	_ = r.Client.Get(context.Background(), types.NamespacedName{Name: "fso-9", Namespace: "default"}, &updated)

	cond := findFSOCondition(updated.Status.Conditions, "Ready")
	if cond == nil {
		t.Fatal("expected Ready condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected Ready=False on invalid threshold, got %s", cond.Status)
	}
	if cond.Reason != "InvalidThreshold" {
		t.Errorf("expected reason=InvalidThreshold, got %s", cond.Reason)
	}
}

func TestFSO_MetricQueryFailure_ErrorCondition(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-10", "50", 60, overflowCluster("oc-1", 0, nil))
	c := fsoClient(s, fso)

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, nil))
	r := &FederatedScaledObjectReconciler{
		Client:          c,
		Scheme:          s,
		ClusterRegistry: reg,
		MetricQuerierFactory: func(_ string) (MetricQuerier, error) {
			return nil, fmt.Errorf("prometheus connection refused")
		},
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "fso-10", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}

	var updated autoscalingv1alpha1.FederatedScaledObject
	_ = r.Client.Get(context.Background(), types.NamespacedName{Name: "fso-10", Namespace: "default"}, &updated)

	cond := findFSOCondition(updated.Status.Conditions, "Ready")
	if cond == nil {
		t.Fatal("expected Ready condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected Ready=False on query failure, got %s", cond.Status)
	}
	if cond.Reason != "MetricQuerierFailed" {
		t.Errorf("expected reason=MetricQuerierFailed, got %s", cond.Reason)
	}
}

func TestFSO_PriorityOrder_LowerPriorityFilledFirst(t *testing.T) {
	s := fsoScheme()
	cap := int32(5)
	fso := newFSO("fso-11", "10", 60,
		overflowCluster("oc-high-priority", 1, &cap), // priority 1 (lower = first)
		overflowCluster("oc-low-priority", 2, &cap),  // priority 2 (used second)
	)
	c := fsoClient(s, fso, primaryDeploy(8))

	reg := newFSORegistry(
		clusterEntry("oc-high-priority", true, 1, &cap),
		clusterEntry("oc-low-priority", true, 2, &cap),
	)
	// metric=18, primary=8 → need 10 overflow; cap=5 per cluster → oc-high gets 5, oc-low gets 5
	r := reconcilerFor(c, reg, 18.0, nil, nil)

	updated := doReconcile(t, r, "fso-11")

	if !updated.Status.SpilloverActive {
		t.Fatal("expected spillover active")
	}

	// Find statuses by name since iteration order is non-deterministic.
	clusterReplicas := make(map[string]int32)
	for _, ocs := range updated.Status.OverflowClusters {
		clusterReplicas[ocs.Name] = ocs.Replicas
	}

	highReplicas := clusterReplicas["oc-high-priority"]
	lowReplicas := clusterReplicas["oc-low-priority"]

	// Priority 1 cluster should be filled to capacity before priority 2 is used.
	if highReplicas < lowReplicas {
		t.Errorf("expected higher-priority cluster (priority=1) to have >= replicas than lower-priority, got high=%d low=%d", highReplicas, lowReplicas)
	}
}

func TestFSO_AtThreshold_NoSpillover(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-12", "50", 60, overflowCluster("oc-1", 0, nil))
	c := fsoClient(s, fso)

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, nil))
	r := reconcilerFor(c, reg, 50.0, nil, nil) // metric == threshold (not strictly greater)

	updated := doReconcile(t, r, "fso-12")

	if updated.Status.SpilloverActive {
		t.Error("expected no spillover when metric equals threshold (strict > required)")
	}
}

func TestFSO_LastScaleTime_SetOnSuccess(t *testing.T) {
	s := fsoScheme()
	fso := newFSO("fso-13", "10", 60, overflowCluster("oc-1", 0, nil))
	c := fsoClient(s, fso, primaryDeploy(5))

	reg := newFSORegistry(clusterEntry("oc-1", true, 0, nil))
	r := reconcilerFor(c, reg, 80.0, nil, nil)

	updated := doReconcile(t, r, "fso-13")

	if updated.Status.SpilloverActive && updated.Status.LastScaleTime == nil {
		t.Error("expected LastScaleTime to be set after successful spillover")
	}
}

// findFSOCondition looks up a Condition by type from a slice.
func findFSOCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

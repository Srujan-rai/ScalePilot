package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/cloudcost"
	"github.com/srujan-rai/scalepilot/pkg/webhook"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func budgetScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = autoscalingv1alpha1.AddToScheme(s)
	return s
}

func budgetClient(objs ...client.Object) client.Client {
	s := budgetScheme()
	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&autoscalingv1alpha1.ScalingBudget{}).
		Build()
}

func newBudget(name string, ceiling int64, warningPct int) *autoscalingv1alpha1.ScalingBudget {
	return &autoscalingv1alpha1.ScalingBudget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: autoscalingv1alpha1.ScalingBudgetSpec{
			Namespace:           "production",
			CeilingMillidollars: ceiling,
			CloudCost: autoscalingv1alpha1.CloudCostConfig{
				Provider: autoscalingv1alpha1.CloudProviderAWS,
				CredentialsSecretRef: autoscalingv1alpha1.SecretReference{
					Name: "creds", Namespace: "default",
				},
			},
			BreachAction:            autoscalingv1alpha1.BreachActionDelay,
			WarningThresholdPercent: warningPct,
			PollIntervalMinutes:     5,
		},
	}
}

func spendQuerier(spend int64) CostQuerierFactory {
	return func(_ autoscalingv1alpha1.CloudCostConfig) (cloudcost.CostQuerier, error) {
		return &mockCostQuerier{spend: spend}, nil
	}
}

func errorQuerier(msg string) CostQuerierFactory {
	return func(_ autoscalingv1alpha1.CloudCostConfig) (cloudcost.CostQuerier, error) {
		return nil, fmt.Errorf("%s", msg)
	}
}

type capturedAlert struct {
	severity webhook.Severity
	title    string
}

type capturingSender struct {
	alerts []capturedAlert
}

func (c *capturingSender) Send(_ context.Context, a webhook.Alert) error {
	c.alerts = append(c.alerts, capturedAlert{severity: a.Severity, title: a.Title})
	return nil
}
func (c *capturingSender) Name() string { return "capture" }

func reconcileBudget(t *testing.T, r *ScalingBudgetReconciler, name string) autoscalingv1alpha1.ScalingBudget {
	t.Helper()
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	var sb autoscalingv1alpha1.ScalingBudget
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &sb); err != nil {
		t.Fatalf("Get ScalingBudget: %v", err)
	}
	return sb
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestBudget_UtilizationRounding(t *testing.T) {
	// spend=50500 millidollars, ceiling=100000 millidollars → 50.5% → rounds to 51
	budget := newBudget("b-round", 100000, 80)
	c := budgetClient(budget)

	r := &ScalingBudgetReconciler{
		Client:             c,
		Scheme:             c.Scheme(),
		CostQuerierFactory: spendQuerier(50500),
	}

	updated := reconcileBudget(t, r, "b-round")

	if updated.Status.UtilizationPercent != 51 {
		t.Errorf("expected utilization=51 (rounded from 50.5), got %d", updated.Status.UtilizationPercent)
	}
}

func TestBudget_BelowWarning_NoNotification(t *testing.T) {
	budget := newBudget("b-safe", 100000, 80)
	c := budgetClient(budget)

	capture := &capturingSender{}
	r := &ScalingBudgetReconciler{
		Client:              c,
		Scheme:              c.Scheme(),
		CostQuerierFactory:  spendQuerier(50000), // 50%, below 80% warning
		NotificationSenders: []webhook.Sender{capture},
	}

	reconcileBudget(t, r, "b-safe")

	if len(capture.alerts) != 0 {
		t.Errorf("expected no notification below warning threshold, got %d alerts", len(capture.alerts))
	}
}

func TestBudget_WarningThreshold_SendsWarning(t *testing.T) {
	budget := newBudget("b-warn", 100000, 80)
	c := budgetClient(budget)

	capture := &capturingSender{}
	r := &ScalingBudgetReconciler{
		Client:              c,
		Scheme:              c.Scheme(),
		CostQuerierFactory:  spendQuerier(85000), // 85% > 80% threshold
		NotificationSenders: []webhook.Sender{capture},
	}

	reconcileBudget(t, r, "b-warn")

	if len(capture.alerts) != 1 {
		t.Fatalf("expected 1 warning alert, got %d", len(capture.alerts))
	}
	if capture.alerts[0].severity != webhook.SeverityWarning {
		t.Errorf("expected SeverityWarning, got %s", capture.alerts[0].severity)
	}
}

func TestBudget_WarningNotSentTwice(t *testing.T) {
	budget := newBudget("b-warn2", 100000, 80)
	// Pre-set status so the controller thinks warning was already crossed.
	budget.Status.UtilizationPercent = 90
	c := budgetClient(budget)

	capture := &capturingSender{}
	r := &ScalingBudgetReconciler{
		Client:              c,
		Scheme:              c.Scheme(),
		CostQuerierFactory:  spendQuerier(92000), // still above warning — already crossed
		NotificationSenders: []webhook.Sender{capture},
	}

	reconcileBudget(t, r, "b-warn2")

	if len(capture.alerts) != 0 {
		t.Errorf("expected no duplicate warning alert, got %d alerts", len(capture.alerts))
	}
}

func TestBudget_Breach_SendsCriticalNotification(t *testing.T) {
	budget := newBudget("b-breach", 50000, 80)
	c := budgetClient(budget)

	capture := &capturingSender{}
	r := &ScalingBudgetReconciler{
		Client:              c,
		Scheme:              c.Scheme(),
		CostQuerierFactory:  spendQuerier(60000), // $60 > $50 ceiling
		NotificationSenders: []webhook.Sender{capture},
	}

	updated := reconcileBudget(t, r, "b-breach")

	if !updated.Status.Breached {
		t.Error("expected Breached=true")
	}

	criticals := 0
	for _, a := range capture.alerts {
		if a.severity == webhook.SeverityCritical {
			criticals++
		}
	}
	if criticals == 0 {
		t.Error("expected at least one critical notification on breach")
	}
}

func TestBudget_BreachNotSentTwice(t *testing.T) {
	budget := newBudget("b-breach2", 50000, 80)
	// Pre-set status: already breached on a prior reconcile.
	budget.Status.Breached = true
	c := budgetClient(budget)

	capture := &capturingSender{}
	r := &ScalingBudgetReconciler{
		Client:              c,
		Scheme:              c.Scheme(),
		CostQuerierFactory:  spendQuerier(60000),
		NotificationSenders: []webhook.Sender{capture},
	}

	reconcileBudget(t, r, "b-breach2")

	for _, a := range capture.alerts {
		if a.severity == webhook.SeverityCritical {
			t.Error("expected no duplicate critical alert on repeated breach reconcile")
		}
	}
}

func TestBudget_CostFetchError_SetsErrorCondition(t *testing.T) {
	budget := newBudget("b-err", 100000, 80)
	c := budgetClient(budget)

	r := &ScalingBudgetReconciler{
		Client:             c,
		Scheme:             c.Scheme(),
		CostQuerierFactory: errorQuerier("cost API unavailable"),
	}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "b-err", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}

	var updated autoscalingv1alpha1.ScalingBudget
	_ = r.Client.Get(context.Background(), types.NamespacedName{Name: "b-err", Namespace: "default"}, &updated)

	cond := findFSOCondition(updated.Status.Conditions, "CostFetched")
	if cond == nil {
		t.Fatal("expected CostFetched condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected CostFetched=False on error, got %s", cond.Status)
	}
}

func TestBudget_ZeroCeiling_NoUtilizationPanic(t *testing.T) {
	budget := newBudget("b-zero", 0, 80)
	c := budgetClient(budget)

	r := &ScalingBudgetReconciler{
		Client:             c,
		Scheme:             c.Scheme(),
		CostQuerierFactory: spendQuerier(50000),
	}

	// Must not panic on division by zero.
	updated := reconcileBudget(t, r, "b-zero")

	if updated.Status.UtilizationPercent != 0 {
		t.Errorf("expected utilization=0 when ceiling=0, got %d", updated.Status.UtilizationPercent)
	}
}

func TestBudget_NotificationTimestamp_UsesClock(t *testing.T) {
	budget := newBudget("b-ts", 100000, 80)
	c := budgetClient(budget)

	fixedTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	var capturedTimestamp time.Time

	type timestampSender struct{ capturingSender }

	tsSender := &struct {
		webhook.Sender
		captured time.Time
	}{}
	_ = tsSender

	// Use a custom sender to capture the alert timestamp.
	capSender := &alertTimestampCapture{}
	r := &ScalingBudgetReconciler{
		Client:              c,
		Scheme:              c.Scheme(),
		Clock:               fakeClock{t: fixedTime},
		CostQuerierFactory:  spendQuerier(85000), // triggers warning
		NotificationSenders: []webhook.Sender{capSender},
	}

	reconcileBudget(t, r, "b-ts")

	capturedTimestamp = capSender.lastTimestamp
	if !capturedTimestamp.Equal(fixedTime) {
		t.Errorf("expected alert timestamp=%v (from clock), got %v", fixedTime, capturedTimestamp)
	}
}

type alertTimestampCapture struct {
	lastTimestamp time.Time
}

func (a *alertTimestampCapture) Send(_ context.Context, alert webhook.Alert) error {
	a.lastTimestamp = alert.Timestamp
	return nil
}
func (a *alertTimestampCapture) Name() string { return "ts-capture" }

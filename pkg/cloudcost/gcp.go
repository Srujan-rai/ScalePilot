package cloudcost

import (
	"context"
	"fmt"
	"time"
)

// GCPConfig holds the configuration for the GCP Billing adapter.
type GCPConfig struct {
	ServiceAccountJSON string
	ProjectID          string
}

// gcpQuerier implements CostQuerier for GCP Cloud Billing.
type gcpQuerier struct {
	config GCPConfig
}

// NewGCPQuerier creates a CostQuerier that fetches data from GCP Cloud Billing.
// Requires IAM role: roles/billing.viewer on the billing account.
func NewGCPQuerier(config GCPConfig) CostQuerier {
	return &gcpQuerier{config: config}
}

func (q *gcpQuerier) Provider() string { return "GCP" }

// GetCurrentCost queries GCP Cloud Billing for the current month's spend.
// The namespace maps to the GKE label "k8s-namespace".
func (q *gcpQuerier) GetCurrentCost(ctx context.Context, namespace string) (*CostData, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	// TODO: Implement actual GCP Cloud Billing API call using cloud.google.com/go/billing.
	// The implementation will use BigQuery billing export with:
	//   - Filter: label "k8s-namespace" = namespace
	//   - Time range: periodStart to now
	_ = ctx
	_ = namespace

	return &CostData{
		CurrentSpendMillidollars: 0,
		ForecastMillidollars:     0,
		PeriodStart:              periodStart,
		PeriodEnd:                periodEnd,
		Currency:                 "USD",
		FetchedAt:                now,
	}, fmt.Errorf("GCP Cloud Billing integration not yet implemented: requires google cloud SDK dependency")
}

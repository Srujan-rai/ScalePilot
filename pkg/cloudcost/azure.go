package cloudcost

import (
	"context"
	"fmt"
	"time"
)

// AzureConfig holds the configuration for the Azure Cost Management adapter.
type AzureConfig struct {
	TenantID       string
	ClientID       string
	ClientSecret   string
	SubscriptionID string
}

// azureQuerier implements CostQuerier for Azure Cost Management.
type azureQuerier struct {
	config AzureConfig
}

// NewAzureQuerier creates a CostQuerier that fetches data from Azure Cost Management.
// Requires RBAC role: Cost Management Reader on the subscription.
func NewAzureQuerier(config AzureConfig) CostQuerier {
	return &azureQuerier{config: config}
}

func (q *azureQuerier) Provider() string { return "Azure" }

// GetCurrentCost queries Azure Cost Management for the current month's spend.
// The namespace maps to the Azure tag "kubernetes-namespace".
func (q *azureQuerier) GetCurrentCost(ctx context.Context, namespace string) (*CostData, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	// TODO: Implement actual Azure Cost Management API call using azure-sdk-for-go.
	// The implementation will use armcostmanagement.QueryClient with:
	//   - Scope: subscription
	//   - Filter: tag "kubernetes-namespace" = namespace
	//   - Timeframe: MonthToDate
	_ = ctx
	_ = namespace

	return &CostData{
		CurrentSpendMillidollars: 0,
		ForecastMillidollars:     0,
		PeriodStart:              periodStart,
		PeriodEnd:                periodEnd,
		Currency:                 "USD",
		FetchedAt:                now,
	}, fmt.Errorf("Azure Cost Management integration not yet implemented: requires azure-sdk-for-go dependency")
}

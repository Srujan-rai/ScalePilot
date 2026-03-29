package cloudcost

import (
	"context"
	"fmt"
	"time"
)

// AWSConfig holds the configuration for the AWS Cost Explorer adapter.
type AWSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	AccountID       string
}

// awsQuerier implements CostQuerier for AWS Cost Explorer.
type awsQuerier struct {
	config AWSConfig
}

// NewAWSQuerier creates a CostQuerier that fetches data from AWS Cost Explorer.
// Requires IAM permissions: ce:GetCostAndUsage.
func NewAWSQuerier(config AWSConfig) CostQuerier {
	return &awsQuerier{config: config}
}

func (q *awsQuerier) Provider() string { return "AWS" }

// GetCurrentCost queries AWS Cost Explorer for the current month's spend
// filtered by the given namespace tag. The namespace maps to the AWS cost
// allocation tag "kubernetes-namespace".
func (q *awsQuerier) GetCurrentCost(ctx context.Context, namespace string) (*CostData, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	// TODO: Implement actual AWS Cost Explorer API call using aws-sdk-go-v2.
	// The implementation will use costexplorer.GetCostAndUsage with:
	//   - TimePeriod: periodStart to now
	//   - Granularity: MONTHLY
	//   - Filter: Tag "kubernetes-namespace" = namespace
	//   - Metrics: UnblendedCost
	_ = ctx
	_ = namespace

	return &CostData{
		CurrentSpendMillidollars: 0,
		ForecastMillidollars:     0,
		PeriodStart:              periodStart,
		PeriodEnd:                periodEnd,
		Currency:                 "USD",
		FetchedAt:                now,
	}, fmt.Errorf("AWS Cost Explorer integration not yet implemented: requires aws-sdk-go-v2 dependency")
}

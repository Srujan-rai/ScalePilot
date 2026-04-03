package cloudcost

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
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
	client *costexplorer.Client
	config AWSConfig
}

// NewAWSQuerier creates a CostQuerier that fetches data from AWS Cost Explorer.
// Requires IAM permissions: ce:GetCostAndUsage, ce:GetCostForecast.
func NewAWSQuerier(config AWSConfig) CostQuerier {
	region := config.Region
	if region == "" {
		region = "us-east-1"
	}

	client := costexplorer.New(costexplorer.Options{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, ""),
	})

	return &awsQuerier{client: client, config: config}
}

func (q *awsQuerier) Provider() string { return "AWS" }

// GetCurrentCost queries AWS Cost Explorer for the current month's spend
// filtered by the given namespace tag. The namespace maps to the AWS cost
// allocation tag "kubernetes-namespace".
func (q *awsQuerier) GetCurrentCost(ctx context.Context, namespace string) (*CostData, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	startStr := periodStart.Format("2006-01-02")
	endStr := now.Format("2006-01-02")
	if startStr == endStr {
		endStr = now.Add(24 * time.Hour).Format("2006-01-02")
	}

	filter := &cetypes.Expression{
		Tags: &cetypes.TagValues{
			Key:          aws.String("kubernetes-namespace"),
			Values:       []string{namespace},
			MatchOptions: []cetypes.MatchOption{cetypes.MatchOptionEquals},
		},
	}

	if q.config.AccountID != "" {
		filter = &cetypes.Expression{
			And: []cetypes.Expression{
				*filter,
				{
					Dimensions: &cetypes.DimensionValues{
						Key:          cetypes.DimensionLinkedAccount,
						Values:       []string{q.config.AccountID},
						MatchOptions: []cetypes.MatchOption{cetypes.MatchOptionEquals},
					},
				},
			},
		}
	}

	input := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(startStr),
			End:   aws.String(endStr),
		},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{"UnblendedCost"},
		Filter:      filter,
	}

	result, err := q.client.GetCostAndUsage(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("AWS Cost Explorer GetCostAndUsage: %w", err)
	}

	var totalCost float64
	for _, group := range result.ResultsByTime {
		if cost, ok := group.Total["UnblendedCost"]; ok && cost.Amount != nil {
			parsed, parseErr := strconv.ParseFloat(*cost.Amount, 64)
			if parseErr != nil {
				return nil, fmt.Errorf("parsing cost amount %q: %w", *cost.Amount, parseErr)
			}
			totalCost += parsed
		}
	}

	forecastCost := q.estimateForecast(totalCost, now, periodStart, periodEnd)

	return &CostData{
		CurrentSpendMillidollars: dollarsToMillidollars(totalCost),
		ForecastMillidollars:     dollarsToMillidollars(forecastCost),
		PeriodStart:              periodStart,
		PeriodEnd:                periodEnd,
		Currency:                 "USD",
		FetchedAt:                now,
	}, nil
}

// estimateForecast does a simple linear projection: if we've spent X in D days,
// the month-end forecast is X * (daysInMonth / D).
func (q *awsQuerier) estimateForecast(
	currentSpend float64, now, periodStart, periodEnd time.Time,
) float64 {
	elapsed := now.Sub(periodStart).Hours() / 24.0
	if elapsed < 1 {
		return currentSpend
	}
	totalDays := periodEnd.Sub(periodStart).Hours() / 24.0
	return currentSpend * (totalDays / elapsed)
}

func dollarsToMillidollars(dollars float64) int64 {
	return int64(math.Round(dollars * 1000))
}

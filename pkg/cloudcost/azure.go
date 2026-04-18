package cloudcost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"golang.org/x/oauth2/clientcredentials"
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
	config     AzureConfig
	httpClient *http.Client
}

// NewAzureQuerier creates a CostQuerier that fetches data from Azure Cost Management.
// Requires RBAC role: Cost Management Reader on the subscription.
func NewAzureQuerier(config AzureConfig) (CostQuerier, error) {
	if config.TenantID == "" || config.ClientID == "" || config.ClientSecret == "" {
		return nil, fmt.Errorf("azure tenant ID, client ID, and client secret are all required")
	}
	if config.SubscriptionID == "" {
		return nil, fmt.Errorf("azure subscription ID is required")
	}

	oauthConfig := &clientcredentials.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		TokenURL: fmt.Sprintf(
			"https://login.microsoftonline.com/%s/oauth2/v2.0/token",
			config.TenantID,
		),
		Scopes: []string{"https://management.azure.com/.default"},
	}

	ctx := context.Background()
	client := oauthConfig.Client(ctx)
	client.Timeout = 30 * time.Second

	return &azureQuerier{config: config, httpClient: client}, nil
}

func (q *azureQuerier) Provider() string { return "Azure" }

// GetCurrentCost queries Azure Cost Management for the current month's spend
// filtered by the Azure tag "kubernetes-namespace".
func (q *azureQuerier) GetCurrentCost(ctx context.Context, namespace string) (*CostData, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.CostManagement/query?api-version=2023-11-01",
		q.config.SubscriptionID,
	)

	requestBody := azureCostQueryRequest{
		Type:      "ActualCost",
		Timeframe: "Custom",
		TimePeriod: azureTimePeriod{
			From: periodStart.Format("2006-01-02T15:04:05+00:00"),
			To:   now.Format("2006-01-02T15:04:05+00:00"),
		},
		Dataset: azureDataset{
			Granularity: "None",
			Aggregation: map[string]azureAggregation{
				"totalCost": {
					Name:     "Cost",
					Function: "Sum",
				},
			},
			Filter: &azureFilter{
				Tags: &azureTagFilter{
					Name:     "kubernetes-namespace",
					Operator: "In",
					Values:   []string{namespace},
				},
			},
		},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling Azure Cost Management request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating Azure Cost Management request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing Azure Cost Management query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading Azure response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure Cost Management returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result azureCostQueryResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing Azure Cost Management response: %w", err)
	}

	totalCost := 0.0
	for _, row := range result.Properties.Rows {
		if len(row) > 0 {
			switch v := row[0].(type) {
			case float64:
				totalCost += v
			case json.Number:
				f, err := v.Float64()
				if err != nil {
					return nil, fmt.Errorf("parsing cost value from Azure response: %w", err)
				}
				totalCost += f
			}
		}
	}

	elapsed := now.Sub(periodStart).Hours() / 24.0
	totalDays := periodEnd.Sub(periodStart).Hours() / 24.0
	forecastCost := totalCost
	if elapsed >= 1 {
		forecastCost = totalCost * (totalDays / elapsed)
	}

	return &CostData{
		CurrentSpendMillidollars: int64(math.Round(totalCost * 1000)),
		ForecastMillidollars:     int64(math.Round(forecastCost * 1000)),
		PeriodStart:              periodStart,
		PeriodEnd:                periodEnd,
		Currency:                 "USD",
		FetchedAt:                now,
	}, nil
}

// Azure Cost Management API request/response types.

type azureCostQueryRequest struct {
	Type       string          `json:"type"`
	Timeframe  string          `json:"timeframe"`
	TimePeriod azureTimePeriod `json:"timePeriod"`
	Dataset    azureDataset    `json:"dataset"`
}

type azureTimePeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type azureDataset struct {
	Granularity string                      `json:"granularity"`
	Aggregation map[string]azureAggregation `json:"aggregation"`
	Filter      *azureFilter                `json:"filter,omitempty"`
}

type azureAggregation struct {
	Name     string `json:"name"`
	Function string `json:"function"`
}

type azureFilter struct {
	Tags *azureTagFilter `json:"tags,omitempty"`
}

type azureTagFilter struct {
	Name     string   `json:"name"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

type azureCostQueryResponse struct {
	Properties struct {
		Rows    [][]interface{} `json:"rows"`
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
	} `json:"properties"`
}

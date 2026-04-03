package cloudcost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GCPConfig holds the configuration for the GCP Billing adapter.
type GCPConfig struct {
	ServiceAccountJSON string
	ProjectID          string

	// DatasetID is the BigQuery dataset containing the billing export.
	// Defaults to "billing_export" if empty.
	DatasetID string

	// TablePattern is the BigQuery table name pattern for the billing export.
	// Defaults to "gcp_billing_export_v1_*" if empty.
	TablePattern string
}

// gcpQuerier implements CostQuerier for GCP Cloud Billing via BigQuery export.
type gcpQuerier struct {
	config     GCPConfig
	httpClient *http.Client
}

// NewGCPQuerier creates a CostQuerier that fetches data from GCP BigQuery
// billing export. Requires IAM roles: roles/bigquery.dataViewer on the billing
// dataset and roles/bigquery.jobUser on the project.
func NewGCPQuerier(config GCPConfig) (CostQuerier, error) {
	if config.ServiceAccountJSON == "" {
		return nil, fmt.Errorf("GCP service account JSON is required")
	}
	if config.ProjectID == "" {
		return nil, fmt.Errorf("GCP project ID is required")
	}
	if config.DatasetID == "" {
		config.DatasetID = "billing_export"
	}
	if config.TablePattern == "" {
		config.TablePattern = "gcp_billing_export_v1_*"
	}

	creds, err := google.CredentialsFromJSON( //nolint:staticcheck // no non-deprecated alternative for JSON-based credentials
		context.Background(),
		[]byte(config.ServiceAccountJSON),
		"https://www.googleapis.com/auth/bigquery.readonly",
	)
	if err != nil {
		return nil, fmt.Errorf("parsing GCP service account credentials: %w", err)
	}

	baseClient := oauth2.NewClient(context.Background(), creds.TokenSource)
	baseClient.Timeout = 30 * time.Second

	return &gcpQuerier{
		config:     config,
		httpClient: baseClient,
	}, nil
}

func (q *gcpQuerier) Provider() string { return "GCP" }

// GetCurrentCost queries BigQuery billing export for the current month's spend
// filtered by the GKE label "k8s-namespace".
func (q *gcpQuerier) GetCurrentCost(ctx context.Context, namespace string) (*CostData, error) {
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	// Escape single quotes for BigQuery string literals (double ' is the escape).
	safeNS := strings.ReplaceAll(namespace, "'", "''")

	query := fmt.Sprintf(`
		SELECT SUM(cost) as total_cost
		FROM `+"`%s.%s.%s`"+`
		WHERE usage_start_time >= TIMESTAMP('%s')
		  AND usage_start_time < TIMESTAMP('%s')
		  AND EXISTS(
		    SELECT 1 FROM UNNEST(labels) AS l
		    WHERE l.key = 'k8s-namespace' AND l.value = '%s'
		  )`,
		q.config.ProjectID,
		q.config.DatasetID,
		q.config.TablePattern,
		periodStart.Format("2006-01-02"),
		now.Format("2006-01-02T15:04:05"),
		safeNS,
	)

	totalCost, err := q.runBigQueryQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("GCP BigQuery billing query: %w", err)
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

type bqQueryRequest struct {
	Query        string `json:"query"`
	UseLegacySql bool   `json:"useLegacySql"`
}

type bqQueryResponse struct {
	Rows []struct {
		F []struct {
			V string `json:"v"`
		} `json:"f"`
	} `json:"rows"`
	TotalRows string `json:"totalRows"`
	Errors    []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (q *gcpQuerier) runBigQueryQuery(ctx context.Context, sqlQuery string) (float64, error) {
	url := fmt.Sprintf(
		"https://bigquery.googleapis.com/bigquery/v2/projects/%s/queries",
		q.config.ProjectID,
	)

	body, err := json.Marshal(bqQueryRequest{Query: sqlQuery, UseLegacySql: false})
	if err != nil {
		return 0, fmt.Errorf("marshaling BigQuery request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("creating BigQuery request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("executing BigQuery query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading BigQuery response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("BigQuery returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result bqQueryResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("parsing BigQuery response: %w", err)
	}

	if len(result.Errors) > 0 {
		return 0, fmt.Errorf("BigQuery query error: %s", result.Errors[0].Message)
	}

	if len(result.Rows) == 0 || len(result.Rows[0].F) == 0 || result.Rows[0].F[0].V == "" {
		return 0, nil
	}

	cost, err := strconv.ParseFloat(result.Rows[0].F[0].V, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing cost value %q: %w", result.Rows[0].F[0].V, err)
	}

	return cost, nil
}

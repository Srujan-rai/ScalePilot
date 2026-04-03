package cloudcost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"context"
)

func TestDollarsToMillidollars(t *testing.T) {
	tests := []struct {
		dollars  float64
		expected int64
	}{
		{0, 0},
		{1.0, 1000},
		{150.0, 150000},
		{0.001, 1},
		{99.999, 99999},
		{1234.5678, 1234568},
	}
	for _, tt := range tests {
		got := dollarsToMillidollars(tt.dollars)
		if got != tt.expected {
			t.Errorf("dollarsToMillidollars(%f) = %d, want %d", tt.dollars, got, tt.expected)
		}
	}
}

func TestNewAWSQuerier(t *testing.T) {
	q := NewAWSQuerier(AWSConfig{
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
		Region:          "us-west-2",
		AccountID:       "123456789",
	})
	if q.Provider() != "AWS" {
		t.Errorf("expected provider 'AWS', got %q", q.Provider())
	}
}

func TestNewAWSQuerier_DefaultRegion(t *testing.T) {
	q := NewAWSQuerier(AWSConfig{
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	})
	if q.Provider() != "AWS" {
		t.Errorf("expected provider 'AWS', got %q", q.Provider())
	}
}

func TestAWSQuerier_EstimateForecast(t *testing.T) {
	q := &awsQuerier{}
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	forecast := q.estimateForecast(100.0, now, start, end)

	expectedDays := 31.0
	elapsedDays := 14.5
	expected := 100.0 * (expectedDays / elapsedDays)

	if diff := forecast - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("estimateForecast = %f, want ~%f", forecast, expected)
	}
}

func TestAWSQuerier_EstimateForecast_FirstDay(t *testing.T) {
	q := &awsQuerier{}
	now := time.Date(2026, 3, 1, 6, 0, 0, 0, time.UTC)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	forecast := q.estimateForecast(5.0, now, start, end)
	if forecast != 5.0 {
		t.Errorf("first day forecast should equal current spend, got %f", forecast)
	}
}

func TestNewGCPQuerier_ValidationErrors(t *testing.T) {
	_, err := NewGCPQuerier(GCPConfig{})
	if err == nil {
		t.Error("expected error for empty config")
	}

	_, err = NewGCPQuerier(GCPConfig{
		ServiceAccountJSON: "{}",
	})
	if err == nil {
		t.Error("expected error for missing project ID")
	}
}

func TestNewAzureQuerier_ValidationErrors(t *testing.T) {
	_, err := NewAzureQuerier(AzureConfig{})
	if err == nil {
		t.Error("expected error for empty config")
	}

	_, err = NewAzureQuerier(AzureConfig{
		TenantID:     "t",
		ClientID:     "c",
		ClientSecret: "s",
	})
	if err == nil {
		t.Error("expected error for missing subscription ID")
	}
}

func TestGCPQuerier_RunBigQueryQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := bqQueryResponse{
			Rows: []struct {
				F []struct {
					V string `json:"v"`
				} `json:"f"`
			}{
				{F: []struct {
					V string `json:"v"`
				}{{V: "42.50"}}},
			},
			TotalRows: "1",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	q := &gcpQuerier{
		config:     GCPConfig{ProjectID: "test-project", DatasetID: "billing"},
		httpClient: server.Client(),
	}

	// Override the URL by modifying runBigQueryQuery to use the test server.
	// Since we can't easily do that, test the response parsing path directly
	// by calling the method through a custom transport.
	transport := &urlRewriteTransport{
		inner:   http.DefaultTransport,
		baseURL: server.URL,
	}
	q.httpClient = &http.Client{Transport: transport}

	cost, err := q.runBigQueryQuery(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != 42.50 {
		t.Errorf("expected cost 42.50, got %f", cost)
	}
}

func TestGCPQuerier_RunBigQueryQuery_EmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := bqQueryResponse{TotalRows: "0"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	q := &gcpQuerier{
		config:     GCPConfig{ProjectID: "test-project"},
		httpClient: &http.Client{Transport: &urlRewriteTransport{inner: http.DefaultTransport, baseURL: server.URL}},
	}

	cost, err := q.runBigQueryQuery(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != 0 {
		t.Errorf("expected cost 0 for empty result, got %f", cost)
	}
}

func TestGCPQuerier_RunBigQueryQuery_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, "Access denied")
	}))
	defer server.Close()

	q := &gcpQuerier{
		config:     GCPConfig{ProjectID: "test-project"},
		httpClient: &http.Client{Transport: &urlRewriteTransport{inner: http.DefaultTransport, baseURL: server.URL}},
	}

	_, err := q.runBigQueryQuery(context.Background(), "SELECT 1")
	if err == nil {
		t.Error("expected error for 403 response")
	}
}

func TestAzureQuerier_GetCurrentCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := azureCostQueryResponse{}
		resp.Properties.Rows = [][]interface{}{{75.25}}
		resp.Properties.Columns = []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}{{Name: "Cost", Type: "Number"}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	q := &azureQuerier{
		config: AzureConfig{SubscriptionID: "sub-123"},
		httpClient: &http.Client{
			Transport: &urlRewriteTransport{inner: http.DefaultTransport, baseURL: server.URL},
		},
	}

	data, err := q.GetCurrentCost(context.Background(), "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.CurrentSpendMillidollars != 75250 {
		t.Errorf("expected 75250 millidollars, got %d", data.CurrentSpendMillidollars)
	}
	if data.Currency != "USD" {
		t.Errorf("expected USD currency, got %q", data.Currency)
	}
}

// urlRewriteTransport rewrites all request URLs to point to a test server,
// preserving the original path.
type urlRewriteTransport struct {
	inner   http.RoundTripper
	baseURL string
}

func (t *urlRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.baseURL[len("http://"):]
	return t.inner.RoundTrip(req)
}

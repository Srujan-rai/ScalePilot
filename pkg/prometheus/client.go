package prometheus

import (
	"context"
	"fmt"
	"time"

	"github.com/srujan-rai/scalepilot/pkg/forecast"
)

// QueryResult holds the parsed metric data from a Prometheus range query.
type QueryResult struct {
	DataPoints []forecast.DataPoint
	Query      string
	Start      time.Time
	End        time.Time
	Step       time.Duration
}

// MetricQuerier fetches time-series data from a Prometheus-compatible endpoint.
// Implementations must be safe for concurrent use.
type MetricQuerier interface {
	// RangeQuery executes a PromQL range query and returns the result
	// as a slice of DataPoints suitable for feeding into a Forecaster.
	RangeQuery(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryResult, error)

	// InstantQuery executes a PromQL instant query and returns the
	// current scalar value.
	InstantQuery(ctx context.Context, query string) (float64, error)
}

// client implements MetricQuerier using the official Prometheus HTTP API.
type client struct {
	address string
}

// NewClient creates a MetricQuerier that connects to the given Prometheus address.
func NewClient(address string) (MetricQuerier, error) {
	if address == "" {
		return nil, fmt.Errorf("prometheus address must not be empty")
	}
	return &client{address: address}, nil
}

// RangeQuery executes a PromQL range query against the configured Prometheus server.
func (c *client) RangeQuery(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryResult, error) {
	promClient, err := newAPIClient(c.address)
	if err != nil {
		return nil, fmt.Errorf("creating prometheus API client: %w", err)
	}

	result, warnings, err := promClient.QueryRange(ctx, query, promRange(start, end, step))
	if err != nil {
		return nil, fmt.Errorf("executing range query %q: %w", query, err)
	}
	if len(warnings) > 0 {
		// Warnings are non-fatal but worth propagating for observability.
		_ = warnings
	}

	points, err := parseMatrix(result)
	if err != nil {
		return nil, fmt.Errorf("parsing range query result: %w", err)
	}

	return &QueryResult{
		DataPoints: points,
		Query:      query,
		Start:      start,
		End:        end,
		Step:       step,
	}, nil
}

// InstantQuery executes a PromQL instant query and returns a scalar value.
func (c *client) InstantQuery(ctx context.Context, query string) (float64, error) {
	promClient, err := newAPIClient(c.address)
	if err != nil {
		return 0, fmt.Errorf("creating prometheus API client: %w", err)
	}

	result, warnings, err := promClient.Query(ctx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("executing instant query %q: %w", query, err)
	}
	if len(warnings) > 0 {
		_ = warnings
	}

	return parseScalar(result)
}

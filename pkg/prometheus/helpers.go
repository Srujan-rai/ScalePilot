package prometheus

import (
	"fmt"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/srujan-rai/scalepilot/pkg/forecast"
)

// newAPIClient creates a Prometheus v1 API client for the given address.
func newAPIClient(address string) (promv1.API, error) {
	c, err := promapi.NewClient(promapi.Config{Address: address})
	if err != nil {
		return nil, fmt.Errorf("creating prometheus client for %s: %w", address, err)
	}
	return promv1.NewAPI(c), nil
}

// promRange converts Go time types into the Prometheus range query format.
func promRange(start, end time.Time, step time.Duration) promv1.Range {
	return promv1.Range{
		Start: start,
		End:   end,
		Step:  step,
	}
}

// parseMatrix extracts DataPoints from a Prometheus matrix result.
// For multi-series results, all series are flattened into a single slice
// sorted by timestamp.
func parseMatrix(result model.Value) ([]forecast.DataPoint, error) {
	matrix, ok := result.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("expected matrix result, got %s", result.Type())
	}

	var points []forecast.DataPoint
	for _, series := range matrix {
		for _, sample := range series.Values {
			points = append(points, forecast.DataPoint{
				Timestamp: sample.Timestamp.Time(),
				Value:     float64(sample.Value),
			})
		}
	}

	if len(points) == 0 {
		return nil, fmt.Errorf("range query returned empty result")
	}

	return points, nil
}

// parseScalar extracts a float64 from a Prometheus instant query result.
// Supports both scalar and single-element vector results.
func parseScalar(result model.Value) (float64, error) {
	switch v := result.(type) {
	case *model.Scalar:
		return float64(v.Value), nil
	case model.Vector:
		if len(v) == 0 {
			return 0, fmt.Errorf("instant query returned empty vector")
		}
		return float64(v[0].Value), nil
	default:
		return 0, fmt.Errorf("expected scalar or vector result, got %s", result.Type())
	}
}

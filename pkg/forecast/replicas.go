package forecast

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// PeakOverHorizon returns the maximum value across the point forecast or, if useUpper is true,
// across the upper confidence series. Both slices must be non-empty and aligned in length
// (as produced by Forecaster.Predict).
func PeakOverHorizon(result *ForecastResult, useUpper bool) float64 {
	if result == nil {
		return 0
	}
	series := result.PredictedValues
	if useUpper {
		series = result.ConfidenceUpper
	}
	var peak float64
	for _, dp := range series {
		if dp.Value > peak {
			peak = dp.Value
		}
	}
	return peak
}

// ReplicasFromForecastPeak maps a peak forecasted metric value to a replica count.
// If targetPerReplica is empty, uses legacy semantics: ceil(peak) (metric must already be in replica units).
// If targetPerReplica is non-empty, it must parse to a finite float > 0 and replicas = ceil(peak / target).
// The result is clamped to at least 1 and to maxCap when maxCap is non-nil.
func ReplicasFromForecastPeak(peak float64, targetPerReplica string, maxCap *int32) (int32, error) {
	targetPerReplica = strings.TrimSpace(targetPerReplica)

	var raw float64
	if targetPerReplica == "" {
		raw = peak
	} else {
		t, err := strconv.ParseFloat(targetPerReplica, 64)
		if err != nil {
			return 0, fmt.Errorf("parse targetMetricValuePerReplica: %w", err)
		}
		if t <= 0 || math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, fmt.Errorf("targetMetricValuePerReplica must be a positive finite number, got %q", targetPerReplica)
		}
		raw = peak / t
	}

	replicas := int32(math.Ceil(raw))
	if replicas < 1 {
		replicas = 1
	}
	if maxCap != nil && *maxCap >= 1 && replicas > *maxCap {
		replicas = *maxCap
	}
	return replicas, nil
}

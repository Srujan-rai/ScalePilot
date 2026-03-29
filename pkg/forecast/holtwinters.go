package forecast

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// HoltWintersConfig holds the hyperparameters for triple exponential smoothing.
type HoltWintersConfig struct {
	Alpha           float64 // level smoothing coefficient (0,1]
	Beta            float64 // trend smoothing coefficient (0,1]
	Gamma           float64 // seasonal smoothing coefficient (0,1]
	SeasonalPeriods int     // number of observations per season
}

// holtWintersEngine implements the Forecaster interface using additive
// Holt-Winters triple exponential smoothing.
type holtWintersEngine struct {
	config HoltWintersConfig

	mu        sync.RWMutex
	level     float64
	trend     float64
	seasonal  []float64
	rmse      float64
	trainedAt time.Time
	trained   bool
}

// NewHoltWinters creates a new Holt-Winters forecaster. The additive variant
// is used, which assumes seasonal variation is roughly constant across levels.
// For multiplicative seasonality (where seasonal effect scales with level),
// a separate implementation would be needed.
func NewHoltWinters(config HoltWintersConfig) Forecaster {
	return &holtWintersEngine{config: config}
}

func (h *holtWintersEngine) Name() string {
	return fmt.Sprintf("HoltWinters(α=%.2f,β=%.2f,γ=%.2f,m=%d)",
		h.config.Alpha, h.config.Beta, h.config.Gamma, h.config.SeasonalPeriods)
}

// Train fits the Holt-Winters model on historical data. It initializes level
// and trend from the first two seasons, then runs the recursive update equations
// for the entire training set.
func (h *holtWintersEngine) Train(ctx context.Context, data []DataPoint) (*ModelParams, error) {
	m := h.config.SeasonalPeriods
	if len(data) < 2*m {
		return nil, fmt.Errorf("insufficient data: need at least %d points (2 seasons), got %d",
			2*m, len(data))
	}

	values := make([]float64, len(data))
	for i, dp := range data {
		values[i] = dp.Value
	}

	// Initialize level as the mean of the first season.
	var levelInit float64
	for i := 0; i < m; i++ {
		levelInit += values[i]
	}
	levelInit /= float64(m)

	// Initialize trend from the average slope between first two seasons.
	var trendInit float64
	for i := 0; i < m; i++ {
		trendInit += (values[m+i] - values[i]) / float64(m)
	}
	trendInit /= float64(m)

	// Initialize seasonal indices from the first season.
	seasonal := make([]float64, m)
	for i := 0; i < m; i++ {
		seasonal[i] = values[i] - levelInit
	}

	alpha := h.config.Alpha
	beta := h.config.Beta
	gamma := h.config.Gamma

	level := levelInit
	trend := trendInit
	sse := 0.0
	count := 0

	// Run through the data applying the triple exponential smoothing equations.
	for t := m; t < len(values); t++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("training cancelled: %w", err)
		}

		seasonIdx := t % m
		y := values[t]

		// One-step-ahead forecast for error calculation.
		forecast := level + trend + seasonal[seasonIdx]
		err := y - forecast
		sse += err * err
		count++

		// Update equations (additive Holt-Winters).
		newLevel := alpha*(y-seasonal[seasonIdx]) + (1-alpha)*(level+trend)
		newTrend := beta*(newLevel-level) + (1-beta)*trend
		seasonal[seasonIdx] = gamma*(y-newLevel) + (1-gamma)*seasonal[seasonIdx]

		level = newLevel
		trend = newTrend
	}

	rmse := 0.0
	if count > 0 {
		rmse = math.Sqrt(sse / float64(count))
	}

	h.mu.Lock()
	h.level = level
	h.trend = trend
	h.seasonal = make([]float64, m)
	copy(h.seasonal, seasonal)
	h.rmse = rmse
	h.trainedAt = time.Now()
	h.trained = true
	h.mu.Unlock()

	// Serialize: [level, trend, rmse, seasonal_0, ..., seasonal_{m-1}]
	coefficients := make([]float64, 0, 3+m)
	coefficients = append(coefficients, level, trend, rmse)
	coefficients = append(coefficients, seasonal...)

	return &ModelParams{
		Algorithm:    "HoltWinters",
		Coefficients: coefficients,
		TrainedAt:    h.trainedAt,
		RMSE:         rmse,
	}, nil
}

// Predict generates forecasted values by extrapolating from the last
// fitted level, trend, and seasonal components.
func (h *holtWintersEngine) Predict(ctx context.Context, horizon time.Duration, step time.Duration) (*ForecastResult, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if !h.trained {
		return nil, fmt.Errorf("model not trained: call Train or LoadParams first")
	}

	nSteps := int(horizon / step)
	if nSteps <= 0 {
		return nil, fmt.Errorf("horizon (%v) must be greater than step (%v)", horizon, step)
	}

	m := h.config.SeasonalPeriods
	predicted := make([]DataPoint, nSteps)
	upper := make([]DataPoint, nSteps)
	lower := make([]DataPoint, nSteps)
	now := time.Now()

	for i := 0; i < nSteps; i++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prediction cancelled: %w", err)
		}

		k := float64(i + 1)
		seasonIdx := i % m
		val := h.level + k*h.trend + h.seasonal[seasonIdx]

		ts := now.Add(step * time.Duration(i+1))
		se := h.rmse * math.Sqrt(k)
		predicted[i] = DataPoint{Timestamp: ts, Value: val}
		upper[i] = DataPoint{Timestamp: ts, Value: val + 1.96*se}
		lower[i] = DataPoint{Timestamp: ts, Value: val - 1.96*se}
	}

	return &ForecastResult{
		PredictedValues: predicted,
		ConfidenceUpper: upper,
		ConfidenceLower: lower,
		ModelError:      h.rmse,
		TrainedAt:       h.trainedAt,
	}, nil
}

// LoadParams restores a previously trained model from cached parameters.
func (h *holtWintersEngine) LoadParams(params *ModelParams) error {
	if params.Algorithm != "HoltWinters" {
		return fmt.Errorf("expected HoltWinters parameters, got %s", params.Algorithm)
	}

	m := h.config.SeasonalPeriods
	if len(params.Coefficients) < 3+m {
		return fmt.Errorf("coefficient vector too short: expected at least %d, got %d",
			3+m, len(params.Coefficients))
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.level = params.Coefficients[0]
	h.trend = params.Coefficients[1]
	h.rmse = params.Coefficients[2]
	h.seasonal = make([]float64, m)
	copy(h.seasonal, params.Coefficients[3:3+m])
	h.trainedAt = params.TrainedAt
	h.trained = true

	return nil
}

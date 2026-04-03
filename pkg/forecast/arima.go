package forecast

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// ARIMAConfig holds the hyperparameters for an ARIMA(p,d,q) model.
type ARIMAConfig struct {
	P int // autoregressive order
	D int // differencing order
	Q int // moving-average order
}

// arimaEngine implements the Forecaster interface for ARIMA models.
// It uses Yule-Walker estimation for AR coefficients and OLS residuals
// for MA coefficient estimation.
type arimaEngine struct {
	config ARIMAConfig

	mu         sync.RWMutex
	arCoeffs   []float64
	maCoeffs   []float64
	diffData   []float64
	residuals  []float64
	lastValues []float64 // last D original values for undifferencing
	mean       float64
	rmse       float64
	trainedAt  time.Time
	trained    bool
}

// NewARIMA creates a new ARIMA forecaster with the given (p,d,q) configuration.
func NewARIMA(config ARIMAConfig) Forecaster {
	return &arimaEngine{config: config}
}

func (a *arimaEngine) Name() string {
	return fmt.Sprintf("ARIMA(%d,%d,%d)", a.config.P, a.config.D, a.config.Q)
}

// Train fits the ARIMA model on historical data using differencing,
// Yule-Walker AR estimation, and residual-based MA estimation.
func (a *arimaEngine) Train(ctx context.Context, data []DataPoint) (*ModelParams, error) {
	if len(data) < a.config.P+a.config.D+a.config.Q+10 {
		return nil, fmt.Errorf("insufficient data: need at least %d points, got %d",
			a.config.P+a.config.D+a.config.Q+10, len(data))
	}

	values := make([]float64, len(data))
	for i, dp := range data {
		values[i] = dp.Value
	}

	// Apply differencing D times to make the series stationary.
	diffed := values
	lastVals := make([]float64, 0, a.config.D)
	for i := 0; i < a.config.D; i++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("training cancelled during differencing: %w", err)
		}
		lastVals = append(lastVals, diffed[len(diffed)-1])
		diffed = difference(diffed)
	}

	mean := meanFloat64(diffed)
	centered := make([]float64, len(diffed))
	for i, v := range diffed {
		centered[i] = v - mean
	}

	// Estimate AR coefficients via Yule-Walker equations.
	arCoeffs := make([]float64, a.config.P)
	if a.config.P > 0 {
		arCoeffs = yuleWalker(centered, a.config.P)
	}

	// Compute residuals for MA estimation.
	residuals := computeResiduals(centered, arCoeffs)

	// Estimate MA coefficients from residual autocorrelations.
	maCoeffs := make([]float64, a.config.Q)
	if a.config.Q > 0 && len(residuals) > a.config.Q {
		maCoeffs = estimateMACoeffs(residuals, a.config.Q)
	}

	rmse := computeRMSE(residuals)

	a.mu.Lock()
	a.arCoeffs = arCoeffs
	a.maCoeffs = maCoeffs
	a.diffData = diffed
	a.residuals = residuals
	a.lastValues = lastVals
	a.mean = mean
	a.rmse = rmse
	a.trainedAt = time.Now()
	a.trained = true
	a.mu.Unlock()

	coefficients := make([]float64, 0, 2+len(arCoeffs)+len(maCoeffs)+len(lastVals))
	coefficients = append(coefficients, mean, float64(a.config.D))
	coefficients = append(coefficients, arCoeffs...)
	coefficients = append(coefficients, maCoeffs...)
	coefficients = append(coefficients, lastVals...)

	return &ModelParams{
		Algorithm:    "ARIMA",
		Coefficients: coefficients,
		TrainedAt:    a.trainedAt,
		RMSE:         rmse,
	}, nil
}

// Predict generates forecasted values for the given horizon.
func (a *arimaEngine) Predict(ctx context.Context, horizon time.Duration, step time.Duration) (*ForecastResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.trained {
		return nil, fmt.Errorf("model not trained: call Train or LoadParams first")
	}

	nSteps := int(horizon / step)
	if nSteps <= 0 {
		return nil, fmt.Errorf("horizon (%v) must be greater than step (%v)", horizon, step)
	}

	// Build a working buffer from the end of the differenced series for AR lookback.
	p := a.config.P
	q := a.config.Q
	history := make([]float64, len(a.diffData))
	copy(history, a.diffData)
	resHistory := make([]float64, len(a.residuals))
	copy(resHistory, a.residuals)

	predicted := make([]DataPoint, nSteps)
	upper := make([]DataPoint, nSteps)
	lower := make([]DataPoint, nSteps)
	now := time.Now()

	for i := 0; i < nSteps; i++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("prediction cancelled: %w", err)
		}

		val := a.mean

		// AR component
		for j := 0; j < p && j < len(history); j++ {
			idx := len(history) - 1 - j
			val += a.arCoeffs[j] * (history[idx] - a.mean)
		}

		// MA component
		for j := 0; j < q && j < len(resHistory); j++ {
			idx := len(resHistory) - 1 - j
			val += a.maCoeffs[j] * resHistory[idx]
		}

		ts := now.Add(step * time.Duration(i+1))
		se := a.rmse * math.Sqrt(float64(i+1))
		predicted[i] = DataPoint{Timestamp: ts, Value: val}
		upper[i] = DataPoint{Timestamp: ts, Value: val + 1.96*se}
		lower[i] = DataPoint{Timestamp: ts, Value: val - 1.96*se}

		history = append(history, val)
		resHistory = append(resHistory, 0)
	}

	// Undo differencing on predictions to get values in original scale.
	if a.config.D > 0 {
		undiffPredictions(predicted, a.lastValues, a.config.D)
		undiffPredictions(upper, a.lastValues, a.config.D)
		undiffPredictions(lower, a.lastValues, a.config.D)
	}

	return &ForecastResult{
		PredictedValues: predicted,
		ConfidenceUpper: upper,
		ConfidenceLower: lower,
		ModelError:      a.rmse,
		TrainedAt:       a.trainedAt,
	}, nil
}

// LoadParams restores a previously trained model from cached parameters.
func (a *arimaEngine) LoadParams(params *ModelParams) error {
	if params.Algorithm != "ARIMA" {
		return fmt.Errorf("expected ARIMA parameters, got %s", params.Algorithm)
	}

	minLen := 2 + a.config.P + a.config.Q + a.config.D
	if len(params.Coefficients) < minLen {
		return fmt.Errorf("coefficient vector too short: expected at least %d, got %d",
			minLen, len(params.Coefficients))
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	offset := 0
	a.mean = params.Coefficients[offset]
	offset++
	offset++ // skip D value

	a.arCoeffs = make([]float64, a.config.P)
	copy(a.arCoeffs, params.Coefficients[offset:offset+a.config.P])
	offset += a.config.P

	a.maCoeffs = make([]float64, a.config.Q)
	copy(a.maCoeffs, params.Coefficients[offset:offset+a.config.Q])
	offset += a.config.Q

	a.lastValues = make([]float64, a.config.D)
	copy(a.lastValues, params.Coefficients[offset:offset+a.config.D])

	a.rmse = params.RMSE
	a.trainedAt = params.TrainedAt
	a.trained = true

	return nil
}

// difference computes first-order differencing on a series.
func difference(data []float64) []float64 {
	if len(data) < 2 {
		return nil
	}
	result := make([]float64, len(data)-1)
	for i := 1; i < len(data); i++ {
		result[i-1] = data[i] - data[i-1]
	}
	return result
}

// yuleWalker estimates AR coefficients via the Yule-Walker method.
// It computes the autocorrelation vector and solves the Toeplitz system
// using the Levinson-Durbin recursion.
func yuleWalker(data []float64, p int) []float64 {
	n := len(data)
	if n <= p {
		return make([]float64, p)
	}

	acf := make([]float64, p+1)
	for lag := 0; lag <= p; lag++ {
		sum := 0.0
		for i := lag; i < n; i++ {
			sum += data[i] * data[i-lag]
		}
		acf[lag] = sum / float64(n)
	}

	if acf[0] == 0 {
		return make([]float64, p)
	}

	// Levinson-Durbin recursion.
	coeffs := make([]float64, p)
	coeffsPrev := make([]float64, p)
	coeffs[0] = acf[1] / acf[0]
	errVar := acf[0] * (1 - coeffs[0]*coeffs[0])

	for m := 1; m < p; m++ {
		copy(coeffsPrev, coeffs)

		lambda := acf[m+1]
		for j := 0; j < m; j++ {
			lambda -= coeffsPrev[j] * acf[m-j]
		}
		if errVar == 0 {
			break
		}
		lambda /= errVar

		coeffs[m] = lambda
		for j := 0; j < m; j++ {
			coeffs[j] = coeffsPrev[j] - lambda*coeffsPrev[m-1-j]
		}
		errVar *= 1 - lambda*lambda
	}

	return coeffs
}

// computeResiduals calculates one-step-ahead prediction errors given
// the centered data and AR coefficients.
func computeResiduals(data []float64, arCoeffs []float64) []float64 {
	n := len(data)
	p := len(arCoeffs)
	residuals := make([]float64, n)

	for t := p; t < n; t++ {
		predicted := 0.0
		for j := 0; j < p; j++ {
			predicted += arCoeffs[j] * data[t-1-j]
		}
		residuals[t] = data[t] - predicted
	}

	return residuals
}

// estimateMACoeffs estimates MA coefficients from the autocorrelation
// of the residual series.
func estimateMACoeffs(residuals []float64, q int) []float64 {
	n := len(residuals)
	if n <= q {
		return make([]float64, q)
	}

	var r0 float64
	for _, r := range residuals {
		r0 += r * r
	}
	r0 /= float64(n)

	if r0 == 0 {
		return make([]float64, q)
	}

	coeffs := make([]float64, q)
	for lag := 1; lag <= q; lag++ {
		sum := 0.0
		for i := lag; i < n; i++ {
			sum += residuals[i] * residuals[i-lag]
		}
		coeffs[lag-1] = (sum / float64(n)) / r0
	}

	return coeffs
}

// computeRMSE computes the root mean squared error of a residual series.
func computeRMSE(residuals []float64) float64 {
	if len(residuals) == 0 {
		return 0
	}
	sum := 0.0
	count := 0
	for _, r := range residuals {
		if r != 0 || count > 0 {
			sum += r * r
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(count))
}

// meanFloat64 computes the arithmetic mean of a float64 slice.
func meanFloat64(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}

// undiffPredictions integrates predictions back to the original scale.
func undiffPredictions(points []DataPoint, lastVals []float64, d int) {
	for dd := d - 1; dd >= 0; dd-- {
		base := lastVals[dd]
		for i := range points {
			points[i].Value += base
			base = points[i].Value
		}
	}
}

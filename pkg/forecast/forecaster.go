package forecast

import (
	"context"
	"time"
)

// DataPoint represents a single timestamped metric observation.
type DataPoint struct {
	Timestamp time.Time
	Value     float64
}

// ForecastResult holds the output of a forecasting operation.
type ForecastResult struct {
	// PredictedValues contains the forecasted data points.
	PredictedValues []DataPoint

	// ConfidenceUpper holds the upper bound of the prediction interval.
	ConfidenceUpper []DataPoint

	// ConfidenceLower holds the lower bound of the prediction interval.
	ConfidenceLower []DataPoint

	// ModelError is the in-sample RMSE (root mean squared error).
	ModelError float64

	// TrainedAt records when the model was last trained.
	TrainedAt time.Time
}

// ModelParams is a serializable snapshot of a trained model's parameters,
// suitable for caching in a ConfigMap.
type ModelParams struct {
	// Algorithm identifies the model type (e.g. "ARIMA", "HoltWinters").
	Algorithm string `json:"algorithm"`

	// Coefficients holds the model-specific parameter vector.
	Coefficients []float64 `json:"coefficients"`

	// TrainedAt records when the model was last trained.
	TrainedAt time.Time `json:"trainedAt"`

	// RMSE is the in-sample root mean squared error.
	RMSE float64 `json:"rmse"`
}

// Forecaster is the primary interface for training and querying forecast models.
// Implementations are expected to be safe for concurrent use.
type Forecaster interface {
	// Train fits the model on the provided historical data points.
	// It returns the serializable model parameters for caching.
	Train(ctx context.Context, data []DataPoint) (*ModelParams, error)

	// Predict generates forecasted values for the given horizon using
	// the currently loaded model. The step parameter controls the interval
	// between predicted points.
	Predict(ctx context.Context, horizon time.Duration, step time.Duration) (*ForecastResult, error)

	// LoadParams restores a previously trained model from cached parameters.
	// This allows the reconcile path to load a model without retraining.
	LoadParams(params *ModelParams) error

	// Name returns the algorithm name for logging and status reporting.
	Name() string
}

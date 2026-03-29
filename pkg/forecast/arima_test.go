package forecast

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestARIMA_TrainAndPredict(t *testing.T) {
	// Generate a simple linear trend: y = 10 + 0.5*t with noise.
	data := generateLinearData(100, 10.0, 0.5)

	arima := NewARIMA(ARIMAConfig{P: 2, D: 1, Q: 1})

	params, err := arima.Train(context.Background(), data)
	if err != nil {
		t.Fatalf("Train() error: %v", err)
	}

	if params.Algorithm != "ARIMA" {
		t.Errorf("expected algorithm ARIMA, got %s", params.Algorithm)
	}
	if params.RMSE < 0 {
		t.Errorf("RMSE should be non-negative, got %f", params.RMSE)
	}
	if len(params.Coefficients) == 0 {
		t.Error("expected non-empty coefficients")
	}

	result, err := arima.Predict(context.Background(), 10*time.Minute, time.Minute)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}

	if len(result.PredictedValues) != 10 {
		t.Errorf("expected 10 predicted values, got %d", len(result.PredictedValues))
	}
	if len(result.ConfidenceUpper) != 10 {
		t.Errorf("expected 10 upper bounds, got %d", len(result.ConfidenceUpper))
	}
	if len(result.ConfidenceLower) != 10 {
		t.Errorf("expected 10 lower bounds, got %d", len(result.ConfidenceLower))
	}

	// Upper bound should be >= predicted >= lower bound.
	for i := range result.PredictedValues {
		if result.ConfidenceUpper[i].Value < result.PredictedValues[i].Value {
			t.Errorf("upper bound %f < predicted %f at step %d",
				result.ConfidenceUpper[i].Value, result.PredictedValues[i].Value, i)
		}
		if result.ConfidenceLower[i].Value > result.PredictedValues[i].Value {
			t.Errorf("lower bound %f > predicted %f at step %d",
				result.ConfidenceLower[i].Value, result.PredictedValues[i].Value, i)
		}
	}
}

func TestARIMA_InsufficientData(t *testing.T) {
	arima := NewARIMA(ARIMAConfig{P: 2, D: 1, Q: 1})

	data := generateLinearData(5, 10.0, 0.5)
	_, err := arima.Train(context.Background(), data)
	if err == nil {
		t.Error("expected error for insufficient data, got nil")
	}
}

func TestARIMA_PredictWithoutTrain(t *testing.T) {
	arima := NewARIMA(ARIMAConfig{P: 1, D: 0, Q: 0})

	_, err := arima.Predict(context.Background(), 5*time.Minute, time.Minute)
	if err == nil {
		t.Error("expected error when predicting without training")
	}
}

func TestARIMA_LoadParams(t *testing.T) {
	data := generateLinearData(100, 10.0, 0.5)

	original := NewARIMA(ARIMAConfig{P: 2, D: 1, Q: 1})
	params, err := original.Train(context.Background(), data)
	if err != nil {
		t.Fatalf("Train() error: %v", err)
	}

	loaded := NewARIMA(ARIMAConfig{P: 2, D: 1, Q: 1})
	if err := loaded.LoadParams(params); err != nil {
		t.Fatalf("LoadParams() error: %v", err)
	}

	result, err := loaded.Predict(context.Background(), 5*time.Minute, time.Minute)
	if err != nil {
		t.Fatalf("Predict() after LoadParams error: %v", err)
	}

	if len(result.PredictedValues) != 5 {
		t.Errorf("expected 5 predictions, got %d", len(result.PredictedValues))
	}
}

func TestARIMA_LoadParams_WrongAlgorithm(t *testing.T) {
	arima := NewARIMA(ARIMAConfig{P: 1, D: 0, Q: 0})
	err := arima.LoadParams(&ModelParams{Algorithm: "HoltWinters"})
	if err == nil {
		t.Error("expected error for wrong algorithm")
	}
}

func TestARIMA_Name(t *testing.T) {
	arima := NewARIMA(ARIMAConfig{P: 2, D: 1, Q: 1})
	if arima.Name() != "ARIMA(2,1,1)" {
		t.Errorf("unexpected name: %s", arima.Name())
	}
}

func TestARIMA_CancelledContext(t *testing.T) {
	data := generateLinearData(100, 10.0, 0.5)
	arima := NewARIMA(ARIMAConfig{P: 2, D: 1, Q: 1})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := arima.Train(ctx, data)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// Helper to generate a linear trend dataset.
func generateLinearData(n int, intercept, slope float64) []DataPoint {
	start := time.Now().Add(-time.Duration(n) * time.Minute)
	data := make([]DataPoint, n)
	for i := 0; i < n; i++ {
		data[i] = DataPoint{
			Timestamp: start.Add(time.Duration(i) * time.Minute),
			Value:     intercept + slope*float64(i),
		}
	}
	return data
}

func TestDifference(t *testing.T) {
	input := []float64{1, 3, 6, 10}
	result := difference(input)

	expected := []float64{2, 3, 4}
	if len(result) != len(expected) {
		t.Fatalf("expected %d results, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if math.Abs(v-expected[i]) > 1e-10 {
			t.Errorf("difference[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func TestMeanFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    []float64
		expected float64
	}{
		{"simple", []float64{1, 2, 3, 4, 5}, 3.0},
		{"single", []float64{42}, 42.0},
		{"empty", []float64{}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := meanFloat64(tt.input)
			if math.Abs(got-tt.expected) > 1e-10 {
				t.Errorf("meanFloat64() = %f, expected %f", got, tt.expected)
			}
		})
	}
}

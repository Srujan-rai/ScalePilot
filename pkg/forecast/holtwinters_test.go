package forecast

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestHoltWinters_TrainAndPredict(t *testing.T) {
	// Generate seasonal data: y = 100 + 2*t + 20*sin(2*pi*t/24) (period=24).
	data := generateSeasonalData(72, 100.0, 2.0, 20.0, 24)

	hw := NewHoltWinters(HoltWintersConfig{
		Alpha:           0.3,
		Beta:            0.1,
		Gamma:           0.2,
		SeasonalPeriods: 24,
	})

	params, err := hw.Train(context.Background(), data)
	if err != nil {
		t.Fatalf("Train() error: %v", err)
	}

	if params.Algorithm != "HoltWinters" {
		t.Errorf("expected algorithm HoltWinters, got %s", params.Algorithm)
	}
	if params.RMSE < 0 {
		t.Errorf("RMSE should be non-negative, got %f", params.RMSE)
	}
	if len(params.Coefficients) != 3+24 {
		t.Errorf("expected %d coefficients, got %d", 3+24, len(params.Coefficients))
	}

	result, err := hw.Predict(context.Background(), 12*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}

	if len(result.PredictedValues) != 12 {
		t.Errorf("expected 12 predicted values, got %d", len(result.PredictedValues))
	}

	for i := range result.PredictedValues {
		if result.ConfidenceUpper[i].Value < result.PredictedValues[i].Value {
			t.Errorf("upper %f < predicted %f at %d",
				result.ConfidenceUpper[i].Value, result.PredictedValues[i].Value, i)
		}
	}
}

func TestHoltWinters_InsufficientData(t *testing.T) {
	hw := NewHoltWinters(HoltWintersConfig{
		Alpha:           0.3,
		Beta:            0.1,
		Gamma:           0.2,
		SeasonalPeriods: 24,
	})

	data := generateSeasonalData(20, 100.0, 1.0, 10.0, 24)
	_, err := hw.Train(context.Background(), data)
	if err == nil {
		t.Error("expected error for insufficient data (< 2 seasons)")
	}
}

func TestHoltWinters_PredictWithoutTrain(t *testing.T) {
	hw := NewHoltWinters(HoltWintersConfig{
		Alpha:           0.3,
		Beta:            0.1,
		Gamma:           0.2,
		SeasonalPeriods: 12,
	})

	_, err := hw.Predict(context.Background(), 5*time.Hour, time.Hour)
	if err == nil {
		t.Error("expected error when predicting without training")
	}
}

func TestHoltWinters_LoadParams(t *testing.T) {
	data := generateSeasonalData(72, 100.0, 1.0, 15.0, 12)

	original := NewHoltWinters(HoltWintersConfig{
		Alpha:           0.3,
		Beta:            0.1,
		Gamma:           0.2,
		SeasonalPeriods: 12,
	})
	params, err := original.Train(context.Background(), data)
	if err != nil {
		t.Fatalf("Train() error: %v", err)
	}

	loaded := NewHoltWinters(HoltWintersConfig{
		Alpha:           0.3,
		Beta:            0.1,
		Gamma:           0.2,
		SeasonalPeriods: 12,
	})
	if err := loaded.LoadParams(params); err != nil {
		t.Fatalf("LoadParams() error: %v", err)
	}

	result, err := loaded.Predict(context.Background(), 6*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("Predict() error: %v", err)
	}

	if len(result.PredictedValues) != 6 {
		t.Errorf("expected 6 predictions, got %d", len(result.PredictedValues))
	}
}

func TestHoltWinters_LoadParams_WrongAlgorithm(t *testing.T) {
	hw := NewHoltWinters(HoltWintersConfig{SeasonalPeriods: 12})
	err := hw.LoadParams(&ModelParams{Algorithm: "ARIMA"})
	if err == nil {
		t.Error("expected error for wrong algorithm")
	}
}

func TestHoltWinters_Name(t *testing.T) {
	hw := NewHoltWinters(HoltWintersConfig{
		Alpha:           0.3,
		Beta:            0.1,
		Gamma:           0.2,
		SeasonalPeriods: 24,
	})
	name := hw.Name()
	if name != "HoltWinters(α=0.30,β=0.10,γ=0.20,m=24)" {
		t.Errorf("unexpected name: %s", name)
	}
}

// generateSeasonalData creates data with trend + seasonality.
func generateSeasonalData(n int, intercept, slope, amplitude float64, period int) []DataPoint {
	start := time.Now().Add(-time.Duration(n) * time.Hour)
	data := make([]DataPoint, n)
	for i := 0; i < n; i++ {
		seasonal := amplitude * math.Sin(2*math.Pi*float64(i)/float64(period))
		data[i] = DataPoint{
			Timestamp: start.Add(time.Duration(i) * time.Hour),
			Value:     intercept + slope*float64(i) + seasonal,
		}
	}
	return data
}

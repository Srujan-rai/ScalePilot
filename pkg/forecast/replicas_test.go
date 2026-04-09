package forecast

import (
	"testing"
	"time"
)

func TestPeakOverHorizon_Point(t *testing.T) {
	res := &ForecastResult{
		PredictedValues: []DataPoint{
			{Timestamp: time.Now(), Value: 1},
			{Timestamp: time.Now(), Value: 5},
			{Timestamp: time.Now(), Value: 3},
		},
		ConfidenceUpper: []DataPoint{
			{Value: 2}, {Value: 7}, {Value: 4},
		},
	}
	if got := PeakOverHorizon(res, false); got != 5 {
		t.Errorf("PeakOverHorizon(point) = %v, want 5", got)
	}
}

func TestPeakOverHorizon_Upper(t *testing.T) {
	res := &ForecastResult{
		PredictedValues: []DataPoint{{Value: 5}},
		ConfidenceUpper: []DataPoint{{Value: 9}},
	}
	if got := PeakOverHorizon(res, true); got != 9 {
		t.Errorf("PeakOverHorizon(upper) = %v, want 9", got)
	}
}

func TestPeakOverHorizon_Nil(t *testing.T) {
	if got := PeakOverHorizon(nil, false); got != 0 {
		t.Errorf("got %v, want 0", got)
	}
}

func TestReplicasFromForecastPeak_LegacyCeil(t *testing.T) {
	r, err := ReplicasFromForecastPeak(12.3, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r != 13 {
		t.Errorf("got %d, want 13", r)
	}
}

func TestReplicasFromForecastPeak_DivideByTarget(t *testing.T) {
	r, err := ReplicasFromForecastPeak(100, "25", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r != 4 {
		t.Errorf("got %d, want 4", r)
	}
}

func TestReplicasFromForecastPeak_Cap(t *testing.T) {
	cap := int32(3)
	r, err := ReplicasFromForecastPeak(1000, "1", &cap)
	if err != nil {
		t.Fatal(err)
	}
	if r != 3 {
		t.Errorf("got %d, want 3", r)
	}
}

func TestReplicasFromForecastPeak_MinOne(t *testing.T) {
	r, err := ReplicasFromForecastPeak(0, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r != 1 {
		t.Errorf("got %d, want 1", r)
	}
}

func TestReplicasFromForecastPeak_InvalidTarget(t *testing.T) {
	_, err := ReplicasFromForecastPeak(10, "0", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = ReplicasFromForecastPeak(10, "not-a-number", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReplicasFromForecastPeak_TrimSpace(t *testing.T) {
	r, err := ReplicasFromForecastPeak(50, " 10 ", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r != 5 {
		t.Errorf("got %d, want 5", r)
	}
}

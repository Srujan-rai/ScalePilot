package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/forecast"
)

type fakeQuerier struct {
	instant float64
	err     error
}

func (f fakeQuerier) RangeQuery(context.Context, string, time.Time, time.Time, time.Duration) ([]forecast.DataPoint, error) {
	return nil, errors.New("not used")
}

func (f fakeQuerier) InstantQuery(context.Context, string) (float64, error) {
	return f.instant, f.err
}

func TestEvaluateScaleUpGuard(t *testing.T) {
	ctx := context.Background()
	basePolicy := func() *autoscalingv1alpha1.ForecastPolicy {
		return &autoscalingv1alpha1.ForecastPolicy{
			Spec: autoscalingv1alpha1.ForecastPolicySpec{
				MetricSource: autoscalingv1alpha1.PrometheusMetricSource{
					Address: "http://prometheus:9090",
				},
			},
		}
	}

	t.Run("nil guard allows", func(t *testing.T) {
		r := &ForecastPolicyReconciler{}
		allow, _, _, err := r.evaluateScaleUpGuard(ctx, basePolicy())
		if err != nil || !allow {
			t.Fatalf("allow=%v err=%v", allow, err)
		}
	})

	t.Run("no factory denies", func(t *testing.T) {
		p := basePolicy()
		p.Spec.ScaleUpGuard = &autoscalingv1alpha1.ScaleUpGuard{
			Query:           "up",
			MaxMetricValue:  "1",
		}
		r := &ForecastPolicyReconciler{}
		_, _, _, err := r.evaluateScaleUpGuard(ctx, p)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blocks when value above max", func(t *testing.T) {
		p := basePolicy()
		p.Spec.ScaleUpGuard = &autoscalingv1alpha1.ScaleUpGuard{
			Query:           "errors_total",
			MaxMetricValue:  "10",
		}
		r := &ForecastPolicyReconciler{
			MetricQuerierFactory: func(string) (MetricQuerier, error) {
				return fakeQuerier{instant: 11}, nil
			},
		}
		allow, v, detail, err := r.evaluateScaleUpGuard(ctx, p)
		if err != nil || allow || v != 11 || detail == "" {
			t.Fatalf("allow=%v v=%v detail=%q err=%v", allow, v, detail, err)
		}
	})

	t.Run("allows when value at max", func(t *testing.T) {
		p := basePolicy()
		p.Spec.ScaleUpGuard = &autoscalingv1alpha1.ScaleUpGuard{
			Query:           "x",
			MaxMetricValue:  "10",
		}
		r := &ForecastPolicyReconciler{
			MetricQuerierFactory: func(string) (MetricQuerier, error) {
				return fakeQuerier{instant: 10}, nil
			},
		}
		allow, _, _, err := r.evaluateScaleUpGuard(ctx, p)
		if err != nil || !allow {
			t.Fatalf("allow=%v err=%v", allow, err)
		}
	})

	t.Run("custom address passed to factory", func(t *testing.T) {
		p := basePolicy()
		p.Spec.ScaleUpGuard = &autoscalingv1alpha1.ScaleUpGuard{
			Address:         "http://other:9090",
			Query:           "x",
			MaxMetricValue:  "1",
		}
		var seen string
		r := &ForecastPolicyReconciler{
			MetricQuerierFactory: func(addr string) (MetricQuerier, error) {
				seen = addr
				return fakeQuerier{instant: 0}, nil
			},
		}
		if _, _, _, err := r.evaluateScaleUpGuard(ctx, p); err != nil {
			t.Fatal(err)
		}
		if seen != "http://other:9090" {
			t.Fatalf("factory address: got %q", seen)
		}
	})
}

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ForecastTrainingTotal counts async model training outcomes.
	ForecastTrainingTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scalepilot",
			Subsystem: "forecastpolicy",
			Name:      "training_total",
			Help:      "ForecastPolicy background training runs by result (success or failure).",
		},
		[]string{"result"},
	)
	// ForecastHPAPatchesTotal counts attempts to raise HPA minReplicas.
	ForecastHPAPatchesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scalepilot",
			Subsystem: "forecastpolicy",
			Name:      "hpa_minreplicas_patch_total",
			Help:      "HPA minReplicas patch attempts (applied, skipped_dry_run, skipped_guard, skipped_profile, skipped_no_change).",
		},
		[]string{"result"},
	)
)

func init() {
	metrics.Registry.MustRegister(ForecastTrainingTotal, ForecastHPAPatchesTotal)
}

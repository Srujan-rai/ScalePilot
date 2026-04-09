// metrics-demo exposes Prometheus metrics matching the ForecastPolicy sample query
// (http_requests_total{deployment="web-frontend"}) for local/minikube demos.
package main

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Synthetic request counter for ScalePilot demos",
		},
		[]string{"deployment"},
	)
	prometheus.MustRegister(counter)

	// Steady load so rate(...[5m]) is non-zero in Prometheus after a few minutes.
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			counter.WithLabelValues("web-frontend").Inc()
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		counter.WithLabelValues("web-frontend").Inc()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	const addr = ":8080"
	panic(http.ListenAndServe(addr, nil))
}

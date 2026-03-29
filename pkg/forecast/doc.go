// Package forecast provides time-series forecasting engines for predicting
// Kubernetes workload demand. It implements ARIMA(p,d,q) and Holt-Winters
// (triple exponential smoothing) models using gonum primitives.
//
// Models are trained asynchronously in background goroutines and cached in
// ConfigMaps so the reconcile path never blocks on expensive computations.
// The Forecaster interface allows callers to train a model on historical
// metric data and produce point forecasts for future horizons.
package forecast

package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/forecast"
	promclient "github.com/srujan-rai/scalepilot/pkg/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var simulateCmd = &cobra.Command{
	Use:   "simulate [forecast-policy-name]",
	Short: "Dry-run a forecast against past data to validate model accuracy",
	Args:  cobra.ExactArgs(1),
	RunE:  runSimulate,
}

func init() {
	simulateCmd.Flags().String("horizon", "1h", "forecast horizon (e.g. 30m, 1h, 2h)")
	simulateCmd.Flags().String("step", "5m", "forecast step interval")
	rootCmd.AddCommand(simulateCmd)
}

func runSimulate(cmd *cobra.Command, args []string) error {
	policyName := args[0]
	ns := viper.GetString("namespace")
	if ns == "" {
		ns = "default"
	}

	c, err := buildClient()
	if err != nil {
		return err
	}

	var policy autoscalingv1alpha1.ForecastPolicy
	if err := c.Get(context.Background(), client.ObjectKey{Name: policyName, Namespace: ns}, &policy); err != nil {
		return fmt.Errorf("fetching ForecastPolicy %s/%s: %w", ns, policyName, err)
	}

	horizonStr, _ := cmd.Flags().GetString("horizon")
	stepStr, _ := cmd.Flags().GetString("step")
	horizon, err := time.ParseDuration(horizonStr)
	if err != nil {
		return fmt.Errorf("invalid horizon %q: %w", horizonStr, err)
	}
	step, err := time.ParseDuration(stepStr)
	if err != nil {
		return fmt.Errorf("invalid step %q: %w", stepStr, err)
	}

	querier, err := promclient.NewClient(policy.Spec.MetricSource.Address)
	if err != nil {
		return fmt.Errorf("creating prometheus client: %w", err)
	}

	historyDur := parseDuration(policy.Spec.MetricSource.HistoryDuration)
	stepInterval := 5 * time.Minute
	if policy.Spec.MetricSource.StepInterval != "" {
		stepInterval = parseDuration(policy.Spec.MetricSource.StepInterval)
	}

	now := time.Now()
	result, err := querier.RangeQuery(context.Background(),
		policy.Spec.MetricSource.Query,
		now.Add(-historyDur), now, stepInterval)
	if err != nil {
		return fmt.Errorf("querying prometheus: %w", err)
	}

	var forecaster forecast.Forecaster
	switch policy.Spec.Algorithm {
	case autoscalingv1alpha1.ForecastAlgorithmARIMA:
		p, d, q := 2, 1, 1
		if policy.Spec.ARIMAParams != nil {
			p = policy.Spec.ARIMAParams.P
			d = policy.Spec.ARIMAParams.D
			q = policy.Spec.ARIMAParams.Q
		}
		forecaster = forecast.NewARIMA(forecast.ARIMAConfig{P: p, D: d, Q: q})
	case autoscalingv1alpha1.ForecastAlgorithmHoltWinters:
		cfg := forecast.HoltWintersConfig{
			Alpha: 0.3, Beta: 0.1, Gamma: 0.2,
			SeasonalPeriods: 24,
		}
		if policy.Spec.HoltWintersParams != nil {
			cfg.SeasonalPeriods = policy.Spec.HoltWintersParams.SeasonalPeriods
		}
		forecaster = forecast.NewHoltWinters(cfg)
	default:
		return fmt.Errorf("unsupported algorithm: %s", policy.Spec.Algorithm)
	}

	fmt.Printf("Training %s on %d data points...\n", forecaster.Name(), len(result.DataPoints))
	params, err := forecaster.Train(context.Background(), result.DataPoints)
	if err != nil {
		return fmt.Errorf("training model: %w", err)
	}
	fmt.Printf("Model trained (RMSE: %.4f)\n\n", params.RMSE)

	forecastResult, err := forecaster.Predict(context.Background(), horizon, step)
	if err != nil {
		return fmt.Errorf("generating forecast: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TIME\tPREDICTED\tLOWER_95\tUPPER_95")
	for i, dp := range forecastResult.PredictedValues {
		_, _ = fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%.2f\n",
			dp.Timestamp.Format("15:04:05"),
			dp.Value,
			forecastResult.ConfidenceLower[i].Value,
			forecastResult.ConfidenceUpper[i].Value)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	peak := forecast.PeakOverHorizon(forecastResult, policy.Spec.UseUpperConfidenceBound)
	bound := "point"
	if policy.Spec.UseUpperConfidenceBound {
		bound = "upper_95"
	}
	replicas, err := forecast.ReplicasFromForecastPeak(
		peak,
		policy.Spec.TargetMetricValuePerReplica,
		policy.Spec.MaxReplicaCap,
	)
	if err != nil {
		return fmt.Errorf("replica mapping: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"\nPeak (%s)=%.4f  implied minReplicas=%d (targetMetricValuePerReplica=%q)\n",
		bound, peak, replicas, policy.Spec.TargetMetricValuePerReplica)
	return nil
}

// parseDuration converts a shorthand duration like "7d" or "24h" to time.Duration.
func parseDuration(s string) time.Duration {
	if len(s) == 0 {
		return time.Hour
	}

	unit := s[len(s)-1]
	numStr := s[:len(s)-1]

	var num int
	for _, c := range numStr {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		}
	}

	switch unit {
	case 's':
		return time.Duration(num) * time.Second
	case 'm':
		return time.Duration(num) * time.Minute
	case 'h':
		return time.Duration(num) * time.Hour
	case 'd':
		return time.Duration(num) * 24 * time.Hour
	default:
		return time.Hour
	}
}

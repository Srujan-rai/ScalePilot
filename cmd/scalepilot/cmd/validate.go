package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

var validateCmd = &cobra.Command{
	Use:   "validate [file...]",
	Short: "Lint and validate ScalePilot CRD manifests before applying",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	scheme := runtime.NewScheme()
	if err := autoscalingv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("adding scheme: %w", err)
	}

	var errCount int
	for _, pattern := range args {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("expanding glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			matches = []string{pattern}
		}

		for _, path := range matches {
			if err := validateFile(path); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", path, err)
				errCount++
			} else {
				fmt.Printf("OK   %s\n", path)
			}
		}
	}

	if errCount > 0 {
		return fmt.Errorf("%d file(s) failed validation", errCount)
	}
	return nil
}

func validateFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	decoder := yaml.NewYAMLOrJSONDecoder(f, 4096)
	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	apiVersion, _ := raw["apiVersion"].(string)
	kind, _ := raw["kind"].(string)

	if !strings.Contains(apiVersion, "scalepilot.io") {
		return fmt.Errorf("not a ScalePilot resource (apiVersion: %s)", apiVersion)
	}

	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshaling to JSON: %w", err)
	}

	switch kind {
	case "ForecastPolicy":
		var obj autoscalingv1alpha1.ForecastPolicy
		if err := json.Unmarshal(jsonBytes, &obj); err != nil {
			return fmt.Errorf("invalid ForecastPolicy: %w", err)
		}
		return validateForecastPolicy(&obj)
	case "FederatedScaledObject":
		var obj autoscalingv1alpha1.FederatedScaledObject
		if err := json.Unmarshal(jsonBytes, &obj); err != nil {
			return fmt.Errorf("invalid FederatedScaledObject: %w", err)
		}
	case "ScalingBudget":
		var obj autoscalingv1alpha1.ScalingBudget
		if err := json.Unmarshal(jsonBytes, &obj); err != nil {
			return fmt.Errorf("invalid ScalingBudget: %w", err)
		}
	case "ClusterScaleProfile":
		var obj autoscalingv1alpha1.ClusterScaleProfile
		if err := json.Unmarshal(jsonBytes, &obj); err != nil {
			return fmt.Errorf("invalid ClusterScaleProfile: %w", err)
		}
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}

	return nil
}

func validateForecastPolicy(p *autoscalingv1alpha1.ForecastPolicy) error {
	if p.Spec.Algorithm == autoscalingv1alpha1.ForecastAlgorithmARIMA && p.Spec.ARIMAParams == nil {
		return fmt.Errorf("arimaParams required when algorithm is ARIMA")
	}
	if p.Spec.Algorithm == autoscalingv1alpha1.ForecastAlgorithmHoltWinters && p.Spec.HoltWintersParams == nil {
		return fmt.Errorf("holtWintersParams required when algorithm is HoltWinters")
	}
	if p.Spec.MetricSource.Address == "" {
		return fmt.Errorf("metricSource.address is required")
	}
	if p.Spec.MetricSource.Query == "" {
		return fmt.Errorf("metricSource.query is required")
	}
	return nil
}

package v1alpha1

import (
	"testing"
)

func TestValidateForecastPolicy_MissingARIMAParams(t *testing.T) {
	fp := &ForecastPolicy{
		Spec: ForecastPolicySpec{
			Algorithm:    ForecastAlgorithmARIMA,
			ARIMAParams:  nil,
			MetricSource: PrometheusMetricSource{Address: "http://prom:9090", Query: "up", HistoryDuration: "7d"},
		},
	}
	errs := validateForecastPolicy(fp)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "spec.arimaParams" {
		t.Errorf("expected error on spec.arimaParams, got %s", errs[0].Field)
	}
}

func TestValidateForecastPolicy_MissingHoltWintersParams(t *testing.T) {
	fp := &ForecastPolicy{
		Spec: ForecastPolicySpec{
			Algorithm:         ForecastAlgorithmHoltWinters,
			HoltWintersParams: nil,
			MetricSource:      PrometheusMetricSource{Address: "http://prom:9090", Query: "up", HistoryDuration: "7d"},
		},
	}
	errs := validateForecastPolicy(fp)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateForecastPolicy_InvalidHoltWintersValues(t *testing.T) {
	fp := &ForecastPolicy{
		Spec: ForecastPolicySpec{
			Algorithm: ForecastAlgorithmHoltWinters,
			HoltWintersParams: &HoltWintersParams{
				Alpha:           "abc",
				Beta:            "0",
				Gamma:           "1.5",
				SeasonalPeriods: 7,
			},
			MetricSource: PrometheusMetricSource{Address: "http://prom:9090", Query: "up", HistoryDuration: "7d"},
		},
	}
	errs := validateForecastPolicy(fp)
	if len(errs) != 3 {
		t.Fatalf("expected 3 errors for invalid alpha/beta/gamma, got %d: %v", len(errs), errs)
	}
}

func TestValidateForecastPolicy_ValidHoltWinters(t *testing.T) {
	fp := &ForecastPolicy{
		Spec: ForecastPolicySpec{
			Algorithm: ForecastAlgorithmHoltWinters,
			HoltWintersParams: &HoltWintersParams{
				Alpha:           "0.3",
				Beta:            "0.1",
				Gamma:           "0.2",
				SeasonalPeriods: 7,
			},
			MetricSource: PrometheusMetricSource{Address: "http://prom:9090", Query: "up", HistoryDuration: "7d"},
		},
	}
	errs := validateForecastPolicy(fp)
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateForecastPolicy_InvalidPrometheusAddress(t *testing.T) {
	fp := &ForecastPolicy{
		Spec: ForecastPolicySpec{
			Algorithm:   ForecastAlgorithmARIMA,
			ARIMAParams: &ARIMAParams{P: 1, D: 1, Q: 1},
			MetricSource: PrometheusMetricSource{
				Address:         "ftp://bad-proto:9090",
				Query:           "up",
				HistoryDuration: "7d",
			},
		},
	}
	errs := validateForecastPolicy(fp)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid address, got %d: %v", len(errs), errs)
	}
}

func TestValidateForecastPolicy_InvalidTargetMetricValuePerReplica(t *testing.T) {
	for _, tc := range []string{"0", "-1", "x", "inf"} {
		fp := &ForecastPolicy{
			Spec: ForecastPolicySpec{
				Algorithm:                   ForecastAlgorithmARIMA,
				ARIMAParams:                 &ARIMAParams{P: 1, D: 1, Q: 1},
				MetricSource:                PrometheusMetricSource{Address: "http://prom:9090", Query: "up", HistoryDuration: "7d"},
				TargetMetricValuePerReplica: tc,
			},
		}
		errs := validateForecastPolicy(fp)
		if len(errs) != 1 {
			t.Fatalf("target %q: expected 1 error, got %d: %v", tc, len(errs), errs)
		}
		if errs[0].Field != "spec.targetMetricValuePerReplica" {
			t.Errorf("field = %s", errs[0].Field)
		}
	}
}

func TestValidateForecastPolicy_ValidTargetMetricValuePerReplica(t *testing.T) {
	fp := &ForecastPolicy{
		Spec: ForecastPolicySpec{
			Algorithm:                   ForecastAlgorithmARIMA,
			ARIMAParams:                 &ARIMAParams{P: 1, D: 1, Q: 1},
			MetricSource:                PrometheusMetricSource{Address: "http://prom:9090", Query: "up", HistoryDuration: "7d"},
			TargetMetricValuePerReplica: "0.5",
		},
	}
	errs := validateForecastPolicy(fp)
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %v", errs)
	}
}

func TestValidateFederatedScaledObject_InvalidThreshold(t *testing.T) {
	fso := &FederatedScaledObject{
		Spec: FederatedScaledObjectSpec{
			PrimaryCluster: ClusterRef{Name: "primary"},
			OverflowClusters: []ClusterRef{
				{Name: "overflow-1"},
			},
			Metric: SpilloverMetric{
				Query:             "queue_depth",
				PrometheusAddress: "http://prom:9090",
				ThresholdValue:    "not-a-number",
			},
			Workload: WorkloadTemplate{DeploymentName: "api", Namespace: "default"},
		},
	}
	errs := validateFederatedScaledObject(fso)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid threshold, got %d: %v", len(errs), errs)
	}
}

func TestValidateFederatedScaledObject_DuplicateClusterNames(t *testing.T) {
	fso := &FederatedScaledObject{
		Spec: FederatedScaledObjectSpec{
			PrimaryCluster: ClusterRef{Name: "cluster-a"},
			OverflowClusters: []ClusterRef{
				{Name: "cluster-a"},
			},
			Metric: SpilloverMetric{
				Query:             "queue_depth",
				PrometheusAddress: "http://prom:9090",
				ThresholdValue:    "100",
			},
			Workload: WorkloadTemplate{DeploymentName: "api", Namespace: "default"},
		},
	}
	errs := validateFederatedScaledObject(fso)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for duplicate names, got %d: %v", len(errs), errs)
	}
}

func TestValidateFederatedScaledObject_Valid(t *testing.T) {
	fso := &FederatedScaledObject{
		Spec: FederatedScaledObjectSpec{
			PrimaryCluster: ClusterRef{Name: "primary"},
			OverflowClusters: []ClusterRef{
				{Name: "overflow-1"},
				{Name: "overflow-2"},
			},
			Metric: SpilloverMetric{
				Query:             "queue_depth",
				PrometheusAddress: "http://prom:9090",
				ThresholdValue:    "100.5",
			},
			Workload: WorkloadTemplate{DeploymentName: "api", Namespace: "default"},
		},
	}
	errs := validateFederatedScaledObject(fso)
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateScalingBudget_InvalidSlackURL(t *testing.T) {
	sb := &ScalingBudget{
		Spec: ScalingBudgetSpec{
			Namespace:               "prod",
			CeilingMillidollars:     100000,
			WarningThresholdPercent: 80,
			Notifications: &NotificationConfig{
				Slack: &SlackNotification{WebhookURL: "http://evil.example.com/webhook"},
			},
		},
	}
	errs := validateScalingBudget(sb)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for invalid Slack URL, got %d: %v", len(errs), errs)
	}
}

func TestValidateScalingBudget_ShortPagerDutyKey(t *testing.T) {
	sb := &ScalingBudget{
		Spec: ScalingBudgetSpec{
			Namespace:               "prod",
			CeilingMillidollars:     100000,
			WarningThresholdPercent: 80,
			Notifications: &NotificationConfig{
				PagerDuty: &PagerDutyNotification{RoutingKey: "short", Severity: "warning"},
			},
		},
	}
	errs := validateScalingBudget(sb)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for short PagerDuty key, got %d: %v", len(errs), errs)
	}
}

func TestValidateScalingBudget_WarningAt100(t *testing.T) {
	sb := &ScalingBudget{
		Spec: ScalingBudgetSpec{
			Namespace:               "prod",
			CeilingMillidollars:     100000,
			WarningThresholdPercent: 100,
		},
	}
	errs := validateScalingBudget(sb)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for 100%% warning, got %d: %v", len(errs), errs)
	}
}

func TestValidateScalingBudget_Valid(t *testing.T) {
	sb := &ScalingBudget{
		Spec: ScalingBudgetSpec{
			Namespace:               "prod",
			CeilingMillidollars:     100000,
			WarningThresholdPercent: 80,
		},
	}
	errs := validateScalingBudget(sb)
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateClusterScaleProfile_InvalidCron(t *testing.T) {
	csp := &ClusterScaleProfile{
		Spec: ClusterScaleProfileSpec{
			MaxSurgePercent: 25,
			BlackoutWindows: []BlackoutWindow{
				{Name: "bad", Start: "invalid", End: "also invalid"},
			},
		},
	}
	errs := validateClusterScaleProfile(csp)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors for invalid cron, got %d: %v", len(errs), errs)
	}
}

func TestValidateClusterScaleProfile_DuplicateTeams(t *testing.T) {
	csp := &ClusterScaleProfile{
		Spec: ClusterScaleProfileSpec{
			MaxSurgePercent: 25,
			TeamOverrides: []TeamOverride{
				{TeamName: "team-a", Namespaces: []string{"ns1"}},
				{TeamName: "team-a", Namespaces: []string{"ns2"}},
			},
		},
	}
	errs := validateClusterScaleProfile(csp)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for duplicate teams, got %d: %v", len(errs), errs)
	}
}

func TestValidateClusterScaleProfile_TeamSurgeExceedsCluster(t *testing.T) {
	surge := int32(50)
	csp := &ClusterScaleProfile{
		Spec: ClusterScaleProfileSpec{
			MaxSurgePercent: 25,
			TeamOverrides: []TeamOverride{
				{TeamName: "team-a", Namespaces: []string{"ns1"}, MaxSurgePercent: &surge},
			},
		},
	}
	errs := validateClusterScaleProfile(csp)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for team surge exceeding cluster, got %d: %v", len(errs), errs)
	}
}

func TestValidateClusterScaleProfile_Valid(t *testing.T) {
	csp := &ClusterScaleProfile{
		Spec: ClusterScaleProfileSpec{
			MaxSurgePercent: 25,
			BlackoutWindows: []BlackoutWindow{
				{Name: "friday", Start: "0 22 * * 5", End: "0 6 * * 6"},
			},
		},
	}
	errs := validateClusterScaleProfile(csp)
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

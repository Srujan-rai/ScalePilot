/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"time"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	autoscalingv2 "k8s.io/api/autoscaling/v2"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/internal/controller"

	_ "github.com/srujan-rai/scalepilot/pkg/metrics" // register Prometheus metrics
	"github.com/srujan-rai/scalepilot/pkg/cloudcost"
	"github.com/srujan-rai/scalepilot/pkg/forecast"
	"github.com/srujan-rai/scalepilot/pkg/multicluster"
	promclient "github.com/srujan-rai/scalepilot/pkg/prometheus"
	webhookpkg "github.com/srujan-rai/scalepilot/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(autoscalingv2.AddToScheme(scheme))
	utilruntime.Must(autoscalingv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. "+
		"Use the port :8080. If not set, it will be '0' in order to disable the metrics server")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "d31fa85b.scalepilot.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Build shared dependencies for reconcilers.
	clusterRegistry := multicluster.NewRegistry(multicluster.NewAPIServerHealthChecker())

	// Start periodic health checks for multi-cluster registry.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				clusterRegistry.RunHealthChecks(context.Background())
			case <-mgr.Elected():
				return
			}
		}
	}()

	// MetricQuerier factory: creates a prometheus client for a given address.
	// The controller defines its own MetricQuerier interface (consumer-side),
	// so we adapt the pkg/prometheus implementation to match.
	metricQuerierFactory := func(address string) (controller.MetricQuerier, error) {
		q, err := promclient.NewClient(address)
		if err != nil {
			return nil, err
		}
		return &metricQuerierAdapter{inner: q}, nil
	}

	// CostQuerier factory: reads cloud credentials from a Secret and creates
	// the appropriate cloud cost adapter (AWS/GCP/Azure), wrapped with a cache.
	costQuerierFactory := buildCostQuerierFactory(mgr)

	// NotificationSender factory: builds Slack/PagerDuty senders from
	// a ScalingBudget's notification config.
	notificationFactory := func(nc *autoscalingv1alpha1.NotificationConfig) []webhookpkg.Sender {
		if nc == nil {
			return nil
		}
		var senders []webhookpkg.Sender
		if nc.Slack != nil {
			senders = append(senders, webhookpkg.NewSlackSender(nc.Slack.WebhookURL, nc.Slack.Channel))
		}
		if nc.PagerDuty != nil {
			senders = append(senders, webhookpkg.NewPagerDutySender(nc.PagerDuty.RoutingKey, nc.PagerDuty.Severity))
		}
		return senders
	}

	// Register controllers.
	if err = (&controller.ClusterScaleProfileReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterScaleProfile")
		os.Exit(1)
	}

	if err = (&controller.ForecastPolicyReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		ForecasterFactory:    nil, // uses defaultForecasterFactory
		MetricQuerierFactory: metricQuerierFactory,
		Recorder:             mgr.GetEventRecorderFor("forecastpolicy-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ForecastPolicy")
		os.Exit(1)
	}

	if err = (&controller.ScalingBudgetReconciler{
		Client:                    mgr.GetClient(),
		Scheme:                    mgr.GetScheme(),
		CostQuerierFactory:        costQuerierFactory,
		NotificationSenderFactory: notificationFactory,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ScalingBudget")
		os.Exit(1)
	}

	if err = (&controller.FederatedScaledObjectReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		ClusterRegistry:      clusterRegistry,
		MetricQuerierFactory: metricQuerierFactory,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FederatedScaledObject")
		os.Exit(1)
	}

	// Register validating webhooks for all CRDs.
	if err = (&autoscalingv1alpha1.ForecastPolicyValidator{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ForecastPolicy")
		os.Exit(1)
	}
	if err = (&autoscalingv1alpha1.FederatedScaledObjectValidator{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "FederatedScaledObject")
		os.Exit(1)
	}
	if err = (&autoscalingv1alpha1.ScalingBudgetValidator{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ScalingBudget")
		os.Exit(1)
	}
	if err = (&autoscalingv1alpha1.ClusterScaleProfileValidator{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "ClusterScaleProfile")
		os.Exit(1)
	}

	// Register the breach-block webhook that enforces the Block breach action
	// by rejecting Deployment/HPA scale-ups in breached namespaces.
	breachHandler := controller.NewBreachBlockWebhook(mgr.GetClient(), mgr.GetScheme())
	mgr.GetWebhookServer().Register("/validate-scale-block", &webhook.Admission{Handler: breachHandler})

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// metricQuerierAdapter adapts the pkg/prometheus.MetricQuerier to the
// controller.MetricQuerier interface. This is needed because the controller
// defines its own interface (consumer-side convention) that returns
// []forecast.DataPoint instead of *prometheus.QueryResult.
type metricQuerierAdapter struct {
	inner promclient.MetricQuerier
}

func (a *metricQuerierAdapter) RangeQuery(
	ctx context.Context, query string, start, end time.Time, step time.Duration,
) ([]forecast.DataPoint, error) {
	result, err := a.inner.RangeQuery(ctx, query, start, end, step)
	if err != nil {
		return nil, err
	}
	return result.DataPoints, nil
}

func (a *metricQuerierAdapter) InstantQuery(ctx context.Context, query string) (float64, error) {
	return a.inner.InstantQuery(ctx, query)
}

// buildCostQuerierFactory creates a CostQuerierFactory closure that reads cloud
// credentials from Kubernetes Secrets and constructs the appropriate cost adapter.
func buildCostQuerierFactory(mgr ctrl.Manager) controller.CostQuerierFactory {
	return func(config autoscalingv1alpha1.CloudCostConfig) (cloudcost.CostQuerier, error) {
		k8sClient := mgr.GetClient()

		var secret corev1.Secret
		secretKey := types.NamespacedName{
			Name:      config.CredentialsSecretRef.Name,
			Namespace: config.CredentialsSecretRef.Namespace,
		}
		if err := k8sClient.Get(context.Background(), secretKey, &secret); err != nil {
			return nil, fmt.Errorf("reading credentials Secret %s: %w", secretKey, err)
		}

		var (
			querier   cloudcost.CostQuerier
			createErr error
		)
		switch config.Provider {
		case autoscalingv1alpha1.CloudProviderAWS:
			querier = cloudcost.NewAWSQuerier(cloudcost.AWSConfig{
				AccessKeyID:     string(secret.Data["aws_access_key_id"]),
				SecretAccessKey: string(secret.Data["aws_secret_access_key"]),
				Region:          config.Region,
				AccountID:       config.AccountID,
			})
		case autoscalingv1alpha1.CloudProviderGCP:
			querier, createErr = cloudcost.NewGCPQuerier(cloudcost.GCPConfig{
				ServiceAccountJSON: string(secret.Data["service_account_json"]),
				ProjectID:          config.AccountID,
			})
			if createErr != nil {
				return nil, fmt.Errorf("creating GCP querier: %w", createErr)
			}
		case autoscalingv1alpha1.CloudProviderAzure:
			querier, createErr = cloudcost.NewAzureQuerier(cloudcost.AzureConfig{
				TenantID:       string(secret.Data["tenant_id"]),
				ClientID:       string(secret.Data["client_id"]),
				ClientSecret:   string(secret.Data["client_secret"]),
				SubscriptionID: string(secret.Data["subscription_id"]),
			})
			if createErr != nil {
				return nil, fmt.Errorf("creating Azure querier: %w", createErr)
			}
		default:
			return nil, fmt.Errorf("unsupported cloud provider: %s", config.Provider)
		}

		return cloudcost.NewCachedQuerier(querier, 5*time.Minute), nil
	}
}

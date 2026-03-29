package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show a live table of forecast predictions vs actual values",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	c, err := buildClient()
	if err != nil {
		return err
	}

	ns := viper.GetString("namespace")

	var policies autoscalingv1alpha1.ForecastPolicyList
	listOpts := []client.ListOption{}
	if ns != "" {
		listOpts = append(listOpts, client.InNamespace(ns))
	}

	if err := c.List(context.Background(), &policies, listOpts...); err != nil {
		return fmt.Errorf("listing ForecastPolicies: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAMESPACE\tNAME\tALGORITHM\tDEPLOYMENT\tPREDICTED\tACTIVE\tLAST TRAINED\tSTATUS")

	for _, p := range policies.Items {
		predicted := "-"
		if p.Status.PredictedMinReplicas != nil {
			predicted = fmt.Sprintf("%d", *p.Status.PredictedMinReplicas)
		}
		active := "-"
		if p.Status.ActiveMinReplicas != nil {
			active = fmt.Sprintf("%d", *p.Status.ActiveMinReplicas)
		}
		lastTrained := "-"
		if p.Status.LastTrainedAt != nil {
			lastTrained = p.Status.LastTrainedAt.Format("2006-01-02 15:04")
		}
		status := conditionStatus(p.Status.Conditions)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Namespace, p.Name, p.Spec.Algorithm,
			p.Spec.TargetDeployment.Name, predicted, active,
			lastTrained, status)
	}

	return w.Flush()
}

func conditionStatus(conditions []metav1.Condition) string {
	for _, c := range conditions {
		if c.Type == string(autoscalingv1alpha1.ForecastConditionModelReady) && c.Status == metav1.ConditionTrue {
			return "Ready"
		}
		if c.Type == string(autoscalingv1alpha1.ForecastConditionError) && c.Status == metav1.ConditionTrue {
			return "Error: " + c.Message
		}
	}
	return "Pending"
}

func buildClient() (client.Client, error) {
	kubeconfigPath := viper.GetString("kubeconfig")

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := autoscalingv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("adding scheme: %w", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return c, nil
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

var clustersCmd = &cobra.Command{
	Use:   "clusters",
	Short: "Multi-cluster management commands",
}

var clustersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List overflow clusters and their health status",
	RunE:  runClustersList,
}

func init() {
	clustersCmd.AddCommand(clustersListCmd)
	rootCmd.AddCommand(clustersCmd)
}

func runClustersList(cmd *cobra.Command, args []string) error {
	c, err := buildClient()
	if err != nil {
		return err
	}

	var fsos autoscalingv1alpha1.FederatedScaledObjectList
	if err := c.List(context.Background(), &fsos); err != nil {
		return fmt.Errorf("listing FederatedScaledObjects: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FSO\tCLUSTER\tROLE\tHEALTHY\tREPLICAS\tPRIORITY\tLAST PROBE")

	for _, fso := range fsos.Items {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			fso.Name, fso.Spec.PrimaryCluster.Name, "primary", "✓",
			fso.Status.PrimaryReplicas, 0, "-")

		for _, overflow := range fso.Status.OverflowClusters {
			healthStr := "✗"
			if overflow.Healthy {
				healthStr = "✓"
			}
			lastProbe := "-"
			if overflow.LastProbeTime != nil {
				lastProbe = overflow.LastProbeTime.Format("15:04:05")
			}
			priority := int32(0)
			for _, specOC := range fso.Spec.OverflowClusters {
				if specOC.Name == overflow.Name {
					priority = specOC.Priority
					break
				}
			}

			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
				fso.Name, overflow.Name, "overflow", healthStr,
				overflow.Replicas, priority, lastProbe)
		}
	}

	return w.Flush()
}

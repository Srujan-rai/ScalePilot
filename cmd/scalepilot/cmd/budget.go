package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

var budgetCmd = &cobra.Command{
	Use:   "budget",
	Short: "FinOps budget management commands",
}

var budgetStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show namespace spend vs budget ceiling for all ScalingBudgets",
	RunE:  runBudgetStatus,
}

func init() {
	budgetCmd.AddCommand(budgetStatusCmd)
	rootCmd.AddCommand(budgetCmd)
}

func runBudgetStatus(cmd *cobra.Command, args []string) error {
	c, err := buildClient()
	if err != nil {
		return err
	}

	var budgets autoscalingv1alpha1.ScalingBudgetList
	if err := c.List(context.Background(), &budgets); err != nil {
		return fmt.Errorf("listing ScalingBudgets: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAMESPACE\tNAME\tPROVIDER\tCEILING\tSPEND\tUTILIZATION\tBREACHED\tACTION\tBLOCKED")

	for _, b := range budgets.Items {
		ceiling := formatMillidollars(b.Spec.CeilingMillidollars)
		spend := formatMillidollars(b.Status.CurrentSpendMillidollars)
		breachedStr := "No"
		if b.Status.Breached {
			breachedStr = "YES"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d%%\t%s\t%s\t%d\n",
			b.Spec.Namespace, b.Name,
			b.Spec.CloudCost.Provider,
			ceiling, spend,
			b.Status.UtilizationPercent,
			breachedStr,
			b.Spec.BreachAction,
			b.Status.BlockedScaleEvents)
	}

	return w.Flush()
}

// formatMillidollars converts millidollars to a human-readable dollar string.
func formatMillidollars(md int64) string {
	dollars := md / 1000
	cents := (md % 1000) / 10
	return fmt.Sprintf("$%d.%02d", dollars, cents)
}

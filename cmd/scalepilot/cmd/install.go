package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Render and apply the ScalePilot Helm chart to the current cluster",
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().String("chart", "", "path to the Helm chart (default: bundled)")
	installCmd.Flags().String("values", "", "path to a values override file")
	installCmd.Flags().String("release-name", "scalepilot", "Helm release name")
	installCmd.Flags().String("target-namespace", "scalepilot-system", "target namespace for installation")
	installCmd.Flags().Bool("dry-run", false, "render manifests without applying")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	releaseName, _ := cmd.Flags().GetString("release-name")
	targetNS, _ := cmd.Flags().GetString("target-namespace")
	valuesFile, _ := cmd.Flags().GetString("values")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	chartPath, _ := cmd.Flags().GetString("chart")

	if chartPath == "" {
		chartPath = "charts/scalepilot"
	}

	if _, err := os.Stat(chartPath); os.IsNotExist(err) {
		return fmt.Errorf("helm chart not found at %s: %w", chartPath, err)
	}

	helmArgs := []string{
		"upgrade", "--install", releaseName, chartPath,
		"--namespace", targetNS,
		"--create-namespace",
	}
	if valuesFile != "" {
		helmArgs = append(helmArgs, "-f", valuesFile)
	}
	if dryRun {
		helmArgs = append(helmArgs, "--dry-run")
	}

	fmt.Printf("Running: helm %v\n", helmArgs)
	helmCmd := exec.Command("helm", helmArgs...)
	helmCmd.Stdout = os.Stdout
	helmCmd.Stderr = os.Stderr

	if err := helmCmd.Run(); err != nil {
		return fmt.Errorf("helm install failed: %w", err)
	}

	if !dryRun {
		fmt.Printf("\nScalePilot installed successfully in namespace %s\n", targetNS)
	}
	return nil
}

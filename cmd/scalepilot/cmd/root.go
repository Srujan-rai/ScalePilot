package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "scalepilot",
	Short: "ScalePilot — predictive autoscaling, multi-cluster federation & FinOps for Kubernetes",
	Long: `ScalePilot is a Kubernetes operator that extends HPA and KEDA with:

  • Predictive scaling using ARIMA and Holt-Winters forecasting
  • Multi-cluster workload federation with automatic spillover
  • Namespace-scoped FinOps cost budgets with breach actions

Use 'scalepilot <command> --help' for more information about a command.`,
}

// Execute runs the root command. Called from main().
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.scalepilot.yaml)")
	rootCmd.PersistentFlags().String("kubeconfig", "", "path to kubeconfig file")
	rootCmd.PersistentFlags().String("namespace", "", "Kubernetes namespace to operate in")

	_ = viper.BindPFlag("kubeconfig", rootCmd.PersistentFlags().Lookup("kubeconfig"))
	_ = viper.BindPFlag("namespace", rootCmd.PersistentFlags().Lookup("namespace"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not find home directory: %v\n", err)
			return
		}
		viper.AddConfigPath(home)
		viper.SetConfigName(".scalepilot")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()
	_ = viper.ReadInConfig()
}

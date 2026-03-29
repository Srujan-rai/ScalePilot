package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// These are set via -ldflags at build time.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the operator version, git commit, and build date",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ScalePilot CLI\n")
		fmt.Printf("  Version:    %s\n", Version)
		fmt.Printf("  Git Commit: %s\n", GitCommit)
		fmt.Printf("  Build Date: %s\n", BuildDate)

		if info, ok := debug.ReadBuildInfo(); ok {
			fmt.Printf("  Go Version: %s\n", info.GoVersion)
			for _, dep := range info.Deps {
				if dep.Path == "sigs.k8s.io/controller-runtime" {
					fmt.Printf("  Controller Runtime: %s\n", dep.Version)
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

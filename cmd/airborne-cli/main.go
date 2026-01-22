package main

import (
	"os"

	"github.com/ai8future/airborne/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "airborne",
		Short: "Airborne CLI - interact with the Airborne admin API",
		Long:  "Command-line tool to test and debug Airborne without a browser.",
	}

	// Global flags
	rootCmd.PersistentFlags().StringP("url", "u", "", "Admin API URL (default: http://localhost:50054 or AIRBORNE_ADMIN_URL)")
	rootCmd.PersistentFlags().StringP("tenant", "t", "ai8", "Tenant ID")
	rootCmd.PersistentFlags().Bool("json", false, "Output as JSON")

	// Create client factory
	clientFactory := func(cmd *cobra.Command) *cli.Client {
		url, _ := cmd.Flags().GetString("url")
		if url == "" {
			url = os.Getenv("AIRBORNE_ADMIN_URL")
		}
		if url == "" {
			url = "http://localhost:50054"
		}
		return cli.NewClient(url)
	}

	// Add commands
	rootCmd.AddCommand(cli.HealthCmd(clientFactory))
	rootCmd.AddCommand(cli.ActivityCmd(clientFactory))
	rootCmd.AddCommand(cli.TestCmd(clientFactory))
	rootCmd.AddCommand(cli.DebugCmd(clientFactory))
	rootCmd.AddCommand(cli.ThreadCmd(clientFactory))
	rootCmd.AddCommand(cli.WatchCmd(clientFactory))

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

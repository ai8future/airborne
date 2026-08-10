package main

import (
	"log"
	"os"

	chassis "github.com/ai8future/chassis-go/v11"
	"github.com/ai8future/chassis-go/v11/registry"

	"github.com/ai8future/airborne"
	"github.com/ai8future/airborne/internal/cli"
	"github.com/spf13/cobra"
)

func main() {
	chassis.SetAppVersion(airborne.AppVersion)
	chassis.RequireMajor(11)
	if err := registry.InitCLI(chassis.Version); err != nil {
		log.Fatalf("registry: %v", err)
	}

	rootCmd := &cobra.Command{
		Use:   "airborne",
		Short: "Airborne CLI - interact with the Airborne admin API",
		Long:  "Command-line tool to test and debug Airborne without a browser.",
	}

	// Global flags
	rootCmd.PersistentFlags().StringP("url", "u", "", "Admin API URL (default: http://localhost:50054 or AIRBORNE_ADMIN_URL)")
	rootCmd.PersistentFlags().String("token", "", "Admin bearer token (default: AIRBORNE_ADMIN_TOKEN)")
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
		client := cli.NewClient(url)
		if token, _ := cmd.Flags().GetString("token"); token != "" {
			client.AuthToken = token
		}
		return client
	}

	// Add commands
	rootCmd.AddCommand(cli.HealthCmd(clientFactory))
	rootCmd.AddCommand(cli.ActivityCmd(clientFactory))
	rootCmd.AddCommand(cli.TestCmd(clientFactory))
	rootCmd.AddCommand(cli.DebugCmd(clientFactory))
	rootCmd.AddCommand(cli.ThreadCmd(clientFactory))
	rootCmd.AddCommand(cli.WatchCmd(clientFactory))

	if err := rootCmd.Execute(); err != nil {
		registry.ShutdownCLI(1)
		os.Exit(1)
	}
	registry.ShutdownCLI(0)
}

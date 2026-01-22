package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type ClientFactory func(cmd *cobra.Command) *Client

func HealthCmd(cf ClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check if Airborne is reachable",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := cf(cmd)
			health, err := client.Health()
			if err != nil {
				fmt.Printf("%s Connection failed: %v\n", red("✗"), err)
				return err
			}

			fmt.Printf("%s Airborne %s (database: %s)\n",
				green("✓"),
				health.Status,
				health.Database)
			return nil
		},
	}
}

func ActivityCmd(cf ClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "List recent requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := cf(cmd)
			tenant, _ := cmd.Flags().GetString("tenant")
			limit, _ := cmd.Flags().GetInt("limit")
			asJSON, _ := cmd.Flags().GetBool("json")

			resp, err := client.Activity(limit, tenant)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Activity)
			}

			if len(resp.Activity) == 0 {
				fmt.Println("No recent activity")
				return nil
			}

			PrintActivityTable(resp.Activity)
			return nil
		},
	}

	cmd.Flags().IntP("limit", "l", 10, "Number of results")
	return cmd
}

func TestCmd(cf ClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test [prompt]",
		Short: "Send a test prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := cf(cmd)
			tenant, _ := cmd.Flags().GetString("tenant")
			provider, _ := cmd.Flags().GetString("provider")
			asJSON, _ := cmd.Flags().GetBool("json")

			req := TestRequest{
				Prompt:   args[0],
				TenantID: tenant,
				Provider: provider,
			}

			fmt.Printf("Sending test prompt to %s...\n", tenant)
			resp, err := client.Test(req)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			fmt.Println()
			PrintTestResult(resp)
			return nil
		},
	}

	cmd.Flags().StringP("provider", "p", "", "Provider to use (gemini, openai, anthropic)")
	return cmd
}

func DebugCmd(cf ClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug [message-id]",
		Short: "Get full request/response details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := cf(cmd)
			asJSON, _ := cmd.Flags().GetBool("json")
			showRaw, _ := cmd.Flags().GetBool("raw")

			resp, err := client.Debug(args[0])
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			PrintDebugInfo(resp)

			if showRaw {
				fmt.Println()
				fmt.Printf("%s\n", bold("Raw Request JSON:"))
				printPrettyJSON(resp.RawRequestJSON)
				fmt.Println()
				fmt.Printf("%s\n", bold("Raw Response JSON:"))
				printPrettyJSON(resp.RawResponseJSON)
			}

			return nil
		},
	}

	cmd.Flags().Bool("raw", false, "Show raw request/response JSON")
	return cmd
}

func ThreadCmd(cf ClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread [thread-id]",
		Short: "View conversation history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := cf(cmd)
			asJSON, _ := cmd.Flags().GetBool("json")

			resp, err := client.Thread(args[0])
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if len(resp.Messages) == 0 {
				fmt.Println("No messages in thread")
				return nil
			}

			fmt.Printf("%s %s\n\n", bold("Thread:"), resp.ThreadID)
			PrintThreadMessages(resp.Messages)
			return nil
		},
	}

	return cmd
}

func WatchCmd(cf ClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Live tail of activity",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := cf(cmd)
			tenant, _ := cmd.Flags().GetString("tenant")
			interval, _ := cmd.Flags().GetInt("interval")

			// Track seen IDs to only show new activity
			seen := make(map[string]bool)

			// Handle Ctrl+C gracefully
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			fmt.Printf("Watching activity for tenant %s (Ctrl+C to stop)...\n\n", cyan(tenant))

			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			defer ticker.Stop()

			// Initial fetch to populate seen
			resp, err := client.Activity(50, tenant)
			if err != nil {
				return err
			}
			for _, a := range resp.Activity {
				seen[a.ID] = true
			}

			// Print header
			fmt.Printf("%-19s  %-6s  %-20s  %-9s  %-8s  %-6s  %s\n",
				"TIME", "TENANT", "MODEL", "IN/OUT", "COST", "DUR", "STATUS")
			fmt.Println(strings.Repeat("-", 85))

			for {
				select {
				case <-sigChan:
					fmt.Println("\nStopped watching.")
					return nil
				case <-ticker.C:
					resp, err := client.Activity(20, tenant)
					if err != nil {
						fmt.Printf("Error: %v\n", err)
						continue
					}

					// Show new activity (in reverse order to show oldest first)
					var newActivity []Activity
					for _, a := range resp.Activity {
						if !seen[a.ID] {
							newActivity = append(newActivity, a)
							seen[a.ID] = true
						}
					}

					// Print in chronological order
					for i := len(newActivity) - 1; i >= 0; i-- {
						a := newActivity[i]
						fmt.Printf("%-19s  %-6s  %-20s  %-9s  %-8s  %-6s  %s\n",
							FormatTimestamp(a.Timestamp),
							a.Tenant,
							TruncateString(a.Model, 20),
							fmt.Sprintf("%s/%s", FormatTokens(a.InputTokens), FormatTokens(a.OutputTokens)),
							FormatCost(a.CostUSD+a.GroundingCostUSD),
							FormatDuration(a.ProcessingTimeMs),
							FormatStatus(a.Status))
					}
				}
			}
		},
	}

	cmd.Flags().IntP("interval", "i", 3, "Poll interval in seconds")
	return cmd
}

func printPrettyJSON(jsonStr string) {
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		fmt.Println(jsonStr)
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}

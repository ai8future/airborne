package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
)

var (
	green  = color.New(color.FgGreen).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	cyan   = color.New(color.FgCyan).SprintFunc()
	bold   = color.New(color.Bold).SprintFunc()
)

func FormatTokens(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func FormatCost(cost float64) string {
	if cost == 0 {
		return "$0.000"
	}
	return fmt.Sprintf("$%.3f", cost)
}

func FormatDuration(ms int) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dms", ms)
}

func FormatStatus(status string) string {
	if status == "success" {
		return green("✓")
	}
	return red("✗")
}

func FormatTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try other formats
		t, err = time.Parse("2006-01-02T15:04:05Z", ts)
		if err != nil {
			return ts
		}
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func TruncateString(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func PrintActivityTable(activities []Activity) {
	// Print header
	fmt.Printf("%-19s  %-6s  %-20s  %-9s  %-8s  %-6s  %s\n",
		"TIME", "TENANT", "MODEL", "IN/OUT", "COST", "DUR", "STATUS")
	fmt.Println(strings.Repeat("-", 85))

	for _, a := range activities {
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

func PrintActivityDetail(a Activity) {
	fmt.Printf("%s %s\n", bold("ID:"), a.ID)
	fmt.Printf("%s %s\n", bold("Thread:"), a.ThreadID)
	fmt.Printf("%s %s\n", bold("Time:"), FormatTimestamp(a.Timestamp))
	fmt.Printf("%s %s\n", bold("Tenant:"), a.Tenant)
	fmt.Printf("%s %s (%s)\n", bold("Model:"), a.Model, a.Provider)
	fmt.Printf("%s %d in / %d out\n", bold("Tokens:"), a.InputTokens, a.OutputTokens)
	fmt.Printf("%s %s (grounding: %s)\n", bold("Cost:"), FormatCost(a.CostUSD), FormatCost(a.GroundingCostUSD))
	fmt.Printf("%s %s\n", bold("Duration:"), FormatDuration(a.ProcessingTimeMs))
	fmt.Printf("%s %s\n", bold("Status:"), FormatStatus(a.Status))
	fmt.Printf("%s\n%s\n", bold("Content:"), a.FullContent)
}

func PrintDebugInfo(d *DebugResponse) {
	fmt.Printf("%s %s\n", bold("Message ID:"), d.MessageID)
	fmt.Printf("%s %s\n", bold("Thread ID:"), d.ThreadID)
	fmt.Printf("%s %s\n", bold("Tenant:"), d.TenantID)
	fmt.Printf("%s %s\n", bold("Time:"), FormatTimestamp(d.Timestamp))
	fmt.Printf("%s %s\n", bold("Status:"), FormatStatus(d.Status))
	fmt.Println()

	fmt.Printf("%s %s (%s)\n", bold("Model:"), d.ResponseModel, d.RequestProvider)
	fmt.Printf("%s %d in / %d out\n", bold("Tokens:"), d.TokensIn, d.TokensOut)
	fmt.Printf("%s %s\n", bold("Cost:"), FormatCost(d.CostUSD))
	if d.GroundingQueries > 0 {
		fmt.Printf("%s %d queries, %s\n", bold("Grounding:"), d.GroundingQueries, FormatCost(d.GroundingCostUSD))
	}
	fmt.Printf("%s %s\n", bold("Duration:"), FormatDuration(d.DurationMs))
	fmt.Println()

	fmt.Printf("%s\n", bold("System Prompt:"))
	fmt.Printf("%s\n\n", cyan(d.SystemPrompt))

	fmt.Printf("%s\n", bold("User Input:"))
	fmt.Printf("%s\n\n", d.UserInput)

	fmt.Printf("%s\n", bold("Response:"))
	fmt.Printf("%s\n", d.ResponseText)
}

func PrintThreadMessages(messages []ThreadMessage) {
	for _, m := range messages {
		roleColor := cyan
		if m.Role == "user" {
			roleColor = yellow
		}

		fmt.Printf("%s [%s] %s\n", roleColor(m.Role), FormatTimestamp(m.Timestamp), m.Model)
		fmt.Printf("%s\n\n", m.Content)
	}
}

func PrintTestResult(r *TestResponse) {
	fmt.Printf("%s %s (%s)\n", bold("Model:"), r.Model, r.Provider)
	fmt.Printf("%s %d in / %d out\n", bold("Tokens:"), r.InputTokens, r.OutputTokens)
	fmt.Printf("%s %s\n", bold("Duration:"), FormatDuration(r.ProcessingMs))
	fmt.Println()
	fmt.Printf("%s\n", bold("Response:"))
	fmt.Println(r.Reply)
}

# Airborne CLI Design

## Overview

Command-line tool to interact with the Airborne admin API, enabling testing and debugging without a browser. Useful for Claude Code to audit and troubleshoot the system.

## Architecture

```
airborne/
├── cmd/
│   └── airborne-cli/
│       └── main.go          # Entry point, command routing
├── internal/
│   └── cli/
│       ├── client.go        # HTTP client for admin API
│       ├── commands.go      # Command implementations
│       └── output.go        # Formatting (table, JSON, etc.)
```

## Configuration

- Default URL: `http://localhost:50054` (port-forwarded admin API)
- Override via `--url` flag or `AIRBORNE_ADMIN_URL` env var
- Default tenant: `ai8`, override via `--tenant` flag

## Commands

### health
Check if backend is reachable.
```bash
airborne health
# Output: ✓ Airborne healthy (database: healthy)
```

### activity
List recent requests.
```bash
airborne activity [--limit 10] [--tenant ai8] [--json]
```
Output table:
```
TIME                 TENANT  MODEL              IN/OUT    COST     STATUS
2026-01-22 15:12:11  ai8     gemini-3-pro       12/8      $0.000   ✓
2026-01-22 15:03:36  ai8     gemini-2.0-flash   11/4      $0.000   ✓
```

### test
Send a test prompt.
```bash
airborne test "What is 2+2?" [--tenant ai8] [--provider gemini]
# Output: Response text, tokens used, duration
```

### debug
Get full request/response details.
```bash
airborne debug <message-id> [--json]
```

### thread
View conversation history.
```bash
airborne thread <thread-id>
```

### watch
Live tail of activity.
```bash
airborne watch [--tenant ai8]
# Continuously polls and displays new activity
```

## Global Flags

- `--url` - Admin API URL (default: `http://localhost:50054`)
- `--tenant` - Tenant ID (default: `ai8`)
- `--json` - Output as JSON instead of formatted

## Dependencies

- `github.com/spf13/cobra` - CLI framework
- `github.com/fatih/color` - Terminal colors
- `github.com/olekukonko/tablewriter` - Table formatting

## Build

```bash
go build -o airborne ./cmd/airborne-cli
# Or install globally:
go install ./cmd/airborne-cli
```

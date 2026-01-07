# Small Code Fix Report
Date Created: 2026-01-07 23:03:36 +0100

## Issues & Fixes

### 1) Tenant IDs are normalized on requests but stored raw
Bug: `resolveTenant` lowercases incoming IDs, but `loadTenants` stores the original casing. Mixed-case tenant IDs fail lookup and can cause false 404s.

Fix: Normalize tenant IDs on load (trim + lowercase) before validation and deduping.

Patch-ready diff:
```diff
diff --git a/internal/tenant/loader.go b/internal/tenant/loader.go
index 0000000..0000000 100644
--- a/internal/tenant/loader.go
+++ b/internal/tenant/loader.go
@@
 		switch ext {
 		case ".json":
 			if err := json.Unmarshal(raw, &cfg); err != nil {
 				return nil, fmt.Errorf("decoding %s: %w", path, err)
 			}
 		case ".yaml", ".yml":
 			if err := yaml.Unmarshal(raw, &cfg); err != nil {
 				return nil, fmt.Errorf("decoding %s: %w", path, err)
 			}
 		}
+
+		cfg.TenantID = strings.ToLower(strings.TrimSpace(cfg.TenantID))
 
 		// Skip files without tenant_id (e.g., shared config files)
 		if cfg.TenantID == "" {
 			continue
 		}
```

### 2) Rate limiter accepts negative token counts
Bug: A negative token delta can reduce counters, effectively widening the allowed rate window.

Fix: Ignore non-positive token values in `RecordTokens`.

Patch-ready diff:
```diff
diff --git a/internal/auth/ratelimit.go b/internal/auth/ratelimit.go
index 0000000..0000000 100644
--- a/internal/auth/ratelimit.go
+++ b/internal/auth/ratelimit.go
@@
 func (r *RateLimiter) RecordTokens(ctx context.Context, clientID string, tokens int64, limit int) error {
 	if !r.enabled {
 		return nil
 	}
+	if tokens <= 0 {
+		return nil
+	}
 
 	// Apply default TPM limit if client-specific limit is 0
 	if limit == 0 {
 		limit = r.defaultLimits.TokensPerMinute
 	}
```

### 3) Logging config is loaded but not applied
Issue: `config.Logging` is never used, so log format/level in `configs/aibox.yaml` or env is ignored.

Fix: Configure the logger after `config.Load` and honor `level`/`format`.

Patch-ready diff:
```diff
diff --git a/cmd/aibox/main.go b/cmd/aibox/main.go
index 0000000..0000000 100644
--- a/cmd/aibox/main.go
+++ b/cmd/aibox/main.go
@@
 	"fmt"
 	"log/slog"
 	"net"
 	"os"
 	"os/signal"
+	"strings"
 	"syscall"
 	"time"
@@
 	// Load configuration
 	cfg, err := config.Load()
 	if err != nil {
 		slog.Error("failed to load configuration", "error", err)
 		os.Exit(1)
 	}
+
+	configureLogger(cfg.Logging)
@@
 }
+
+func configureLogger(cfg config.LoggingConfig) {
+	level := slog.LevelInfo
+	switch strings.ToLower(cfg.Level) {
+	case "debug":
+		level = slog.LevelDebug
+	case "warn", "warning":
+		level = slog.LevelWarn
+	case "error":
+		level = slog.LevelError
+	}
+
+	format := strings.ToLower(cfg.Format)
+	var handler slog.Handler
+	if format == "text" {
+		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
+	} else {
+		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
+	}
+
+	slog.SetDefault(slog.New(handler))
+}
```

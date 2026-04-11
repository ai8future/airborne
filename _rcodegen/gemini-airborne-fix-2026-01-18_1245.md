Date Created: Sunday, January 18, 2026 12:45
TOTAL_SCORE: 85/100

# Airborne Codebase Audit

I have analyzed the Airborne codebase and found a few issues primarily related to resource management, observability, and configuration robustness. The overall code quality is good, with clear separation of concerns and consistent style.

## Issues and Fixes

### 1. Resource Leak: Redis Client Not Closed
**Severity:** Medium
**Location:** `internal/server/grpc.go`
**Description:** The `ServerComponents.Close` method closes the database client but fails to close the Redis client, leading to potential connection leaks upon server shutdown.

#### Patch
```go
<<<<
// Close closes all server components that need cleanup.
func (c *ServerComponents) Close() {
	if c.DBClient != nil {
		c.DBClient.Close()
	}
}
====
// Close closes all server components that need cleanup.
func (c *ServerComponents) Close() {
	if c.DBClient != nil {
		c.DBClient.Close()
	}
	if c.RedisClient != nil {
		c.RedisClient.Close()
	}
}
>>>>
```

### 2. Observability: Missing Error Log in Stream Handler
**Severity:** Medium
**Location:** `internal/service/chat.go`
**Description:** In `GenerateReplyStream`, if the provider fails immediately, the error is returned to the client but not logged on the server. This makes debugging "internal errors" difficult as the original cause is lost.

#### Patch
```go
<<<<
	// Generate streaming reply
	streamChunks, err := prepared.provider.GenerateReplyStream(ctx, prepared.params)
	if err != nil {
		return status.Error(codes.Internal, sanitize.SanitizeForClient(err))
	}

	var accumulatedText strings.Builder
====
	// Generate streaming reply
	streamChunks, err := prepared.provider.GenerateReplyStream(ctx, prepared.params)
	if err != nil {
		slog.Error("provider stream request failed",
			"provider", prepared.provider.Name(),
			"error", err,
			"request_id", prepared.requestID,
		)
		return status.Error(codes.Internal, sanitize.SanitizeForClient(err))
	}

	var accumulatedText strings.Builder
>>>>
```

### 3. Configuration: Hardcoded Pricing Config Path
**Severity:** Low
**Location:** `internal/pricing/pricing.go`
**Description:** The `ensureInitialized` function hardcodes the configuration directory to `"configs"`. This is brittle and may fail if the binary is executed from a different directory or in a containerized environment where paths differ.

#### Patch
```go
<<<<
// ensureInitialized lazily initializes the default pricer
func ensureInitialized() {
	initOnce.Do(func() {
		defaultPricer, initErr = NewPricer("configs")
		if initErr != nil {
			// Log but don't fail - CalculateCost will return 0 for unknown models
			defaultPricer = &Pricer{
				models:    make(map[string]ModelPricing),
				providers: make(map[string]ProviderPricing),
			}
		}
	})
}
====
// ensureInitialized lazily initializes the default pricer
func ensureInitialized() {
	initOnce.Do(func() {
		configDir := os.Getenv("AIRBORNE_CONFIG_DIR")
		if configDir == "" {
			configDir = "configs"
		}
		defaultPricer, initErr = NewPricer(configDir)
		if initErr != nil {
			// Log but don't fail - CalculateCost will return 0 for unknown models
			defaultPricer = &Pricer{
				models:    make(map[string]ModelPricing),
				providers: make(map[string]ProviderPricing),
			}
		}
	})
}
>>>>
```

### 4. Code Smell: Incomplete Image Handling in OpenAI Provider
**Severity:** Low
**Location:** `internal/provider/openai/client.go`
**Description:** The OpenAI provider detects image outputs from code interpreter execution but does not retrieve the actual image content, creating a placeholder "output.png" file. A comment has been added to clarify this limitation.

#### Patch
```go
<<<<
				case "image":
					// Image outputs have a URL
					exec.Files = append(exec.Files, provider.GeneratedFile{
						Name:     "output.png",
						MIMEType: "image/png",
					})
				}
====
				case "image":
					// Image outputs have a URL
					// TODO: Retrieve actual image content using OpenAI Files API
					// Currently we only signal that an image was generated
					exec.Files = append(exec.Files, provider.GeneratedFile{
						Name:     "output.png",
						MIMEType: "image/png",
					})
				}
>>>>
```

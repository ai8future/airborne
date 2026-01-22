package envutil

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// GetStringEnv reads a string environment variable with a default fallback.
func GetStringEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

// GetIntEnv reads an integer environment variable with a default fallback.
// Logs a warning if the value is invalid.
func GetIntEnv(key string, defaultValue int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}

	intVal, err := strconv.Atoi(val)
	if err != nil {
		slog.Warn("invalid integer value for environment variable",
			"key", key,
			"value", val,
			"default", defaultValue)
		return defaultValue
	}

	return intVal
}

// GetBoolEnv reads a boolean environment variable with a default fallback.
// Accepts: "true", "1" (true), "false", "0" (false), case-insensitive.
// Logs a warning if the value is invalid.
func GetBoolEnv(key string, defaultValue bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}

	switch strings.ToLower(val) {
	case "true", "1":
		return true
	case "false", "0":
		return false
	default:
		slog.Warn("invalid boolean value for environment variable",
			"key", key,
			"value", val,
			"default", defaultValue)
		return defaultValue
	}
}

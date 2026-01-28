package provider

import "strings"

// SelectModel returns the model to use based on priority:
// 1. Override model (if non-empty after trimming)
// 2. Config model (if non-empty)
// 3. Default model
func SelectModel(configModel, defaultModel, overrideModel string) string {
	if override := strings.TrimSpace(overrideModel); override != "" {
		return override
	}
	if configModel != "" {
		return configModel
	}
	return defaultModel
}

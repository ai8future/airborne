package provider

import "testing"

func TestSelectModel(t *testing.T) {
	tests := []struct {
		name          string
		configModel   string
		defaultModel  string
		overrideModel string
		want          string
	}{
		{
			name:         "default when all empty",
			defaultModel: "gpt-4o",
			want:         "gpt-4o",
		},
		{
			name:         "config overrides default",
			configModel:  "gpt-4o-mini",
			defaultModel: "gpt-4o",
			want:         "gpt-4o-mini",
		},
		{
			name:          "override wins over config",
			configModel:   "gpt-4o-mini",
			defaultModel:  "gpt-4o",
			overrideModel: "gemini-2.5-flash",
			want:          "gemini-2.5-flash",
		},
		{
			name:          "whitespace-only override ignored",
			configModel:   "gpt-4o-mini",
			defaultModel:  "gpt-4o",
			overrideModel: "   ",
			want:          "gpt-4o-mini",
		},
		{
			name:          "override trimmed of whitespace",
			defaultModel:  "gpt-4o",
			overrideModel: "  gemini-2.5-flash  ",
			want:          "gemini-2.5-flash",
		},
		{
			name:         "empty config falls to default",
			configModel:  "",
			defaultModel: "gpt-4o",
			want:         "gpt-4o",
		},
		{
			name:          "override wins even with empty config",
			configModel:   "",
			defaultModel:  "gpt-4o",
			overrideModel: "claude-sonnet-4",
			want:          "claude-sonnet-4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectModel(tt.configModel, tt.defaultModel, tt.overrideModel)
			if got != tt.want {
				t.Errorf("SelectModel(%q, %q, %q) = %q, want %q",
					tt.configModel, tt.defaultModel, tt.overrideModel, got, tt.want)
			}
		})
	}
}

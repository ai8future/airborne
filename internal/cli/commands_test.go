package cli

import (
	"github.com/spf13/cobra"
	"testing"
)

func TestCommandConstruction(t *testing.T) {
	factory := func(*cobra.Command) *Client { return NewClient("http://127.0.0.1") }
	for _, cmd := range []*cobra.Command{HealthCmd(factory), ActivityCmd(factory), TestCmd(factory), DebugCmd(factory), ThreadCmd(factory), WatchCmd(factory)} {
		if cmd == nil || cmd.Use == "" {
			t.Fatal("invalid command")
		}
	}
}

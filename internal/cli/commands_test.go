package cli

import (
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCommandsExecuteAgainstServer(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/admin/health":
			w.Write([]byte(`{"status":"ok"}`))
		case r.URL.Path == "/admin/activity":
			w.Write([]byte(`{"activity":[]}`))
		case r.URL.Path == "/admin/test":
			w.Write([]byte(`{"reply":"ok"}`))
		case strings.HasPrefix(r.URL.Path, "/admin/debug/"):
			w.Write([]byte(`{"message_id":"x"}`))
		case strings.HasPrefix(r.URL.Path, "/admin/thread/"):
			w.Write([]byte(`{"thread_id":"x"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	f := func(*cobra.Command) *Client { return NewClient(s.URL) }
	cases := []struct {
		c    *cobra.Command
		args []string
	}{{HealthCmd(f), nil}, {ActivityCmd(f), nil}, {TestCmd(f), []string{"hello"}}, {DebugCmd(f), []string{"id"}}, {ThreadCmd(f), []string{"id"}}}
	for _, tt := range cases {
		tt.c.SetArgs(tt.args)
		tt.c.SetOut(io.Discard)
		tt.c.SetErr(io.Discard)
		_ = tt.c.Execute()
	}
}

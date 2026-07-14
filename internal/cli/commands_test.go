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

func TestPrintPrettyJSON(t *testing.T) {
	valid := captureStdout(t, func() { printPrettyJSON(`{"answer":42}`) })
	if !strings.Contains(valid, "\n  \"answer\": 42\n") {
		t.Fatalf("pretty JSON output = %q", valid)
	}
	invalid := captureStdout(t, func() { printPrettyJSON(`not-json`) })
	if invalid != "not-json\n" {
		t.Fatalf("invalid JSON output = %q", invalid)
	}
}

func TestCommandJSONAndRawOutputFixtures(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/admin/activity":
			_, _ = w.Write([]byte(`{"activity":[{"id":"a","tenant":"tenant-a"}]}`))
		case "/admin/test":
			_, _ = w.Write([]byte(`{"reply":"reply","provider":"openai"}`))
		case "/admin/debug/id":
			_, _ = w.Write([]byte(`{"message_id":"id","raw_request_json":"{\"request\":true}","raw_response_json":"bad-json"}`))
		case "/admin/thread/id":
			_, _ = w.Write([]byte(`{"thread_id":"id","messages":[{"role":"user","content":"hello"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	factory := func(*cobra.Command) *Client { return NewClient(s.URL) }
	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
		want string
	}{
		{"activity-json", ActivityCmd(factory), []string{"--json"}, "tenant-a"},
		{"test-json", TestCmd(factory), []string{"--json", "hello"}, "reply"},
		{"debug-raw", DebugCmd(factory), []string{"--raw", "id"}, "request"},
		{"thread-json", ThreadCmd(factory), []string{"--json", "id"}, "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cmd.Flags().Bool("json", false, "json output")
			tc.cmd.SetArgs(tc.args)
			out := captureStdout(t, func() {
				if err := tc.cmd.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output %q does not contain %q", out, tc.want)
			}
		})
	}
}

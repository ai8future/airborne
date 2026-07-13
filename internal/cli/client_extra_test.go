package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAllEndpointsAndAuth(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/admin/health":
			w.Write([]byte(`{"status":"healthy","database":"ok"}`))
		case "/admin/activity":
			if r.URL.Query().Get("tenant_id") != "t" {
				t.Error("tenant")
			}
			w.Write([]byte(`{"activity":[]}`))
		case "/admin/test":
			w.Write([]byte(`{"reply":"ok"}`))
		case "/admin/debug/a/b":
			w.Write([]byte(`{"message_id":"x"}`))
		case "/admin/thread/a/b":
			w.Write([]byte(`{"thread_id":"x"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	c := NewClient(s.URL)
	c.AuthToken = "token"
	if _, e := c.Health(); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Activity(2, "t"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Test(TestRequest{Prompt: "x"}); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Debug("a/b"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Thread("a/b"); e != nil {
		t.Fatal(e)
	}
}
func TestClientHTTPAndDecodeErrors(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/health" {
			http.Error(w, "bad", 500)
			return
		}
		w.Write([]byte(`bad`))
	}))
	defer s.Close()
	c := NewClient(s.URL)
	if _, e := c.Health(); e == nil {
		t.Fatal("health status error")
	}
	if _, e := c.Activity(1, ""); e == nil {
		t.Fatal("decode error")
	}
	if got := readErrorBody(strings.NewReader("")); got != "" {
		t.Fatal(got)
	}
}

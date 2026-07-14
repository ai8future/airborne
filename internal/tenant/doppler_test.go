package tenant

import (
	"io"
	"net/http"
	"strings"
	"testing"

	chassis "github.com/ai8future/chassis-go/v11"
	"github.com/ai8future/chassis-go/v11/call"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestMain(m *testing.M) {
	chassis.RequireMajor(11)
	m.Run()
}

func TestDopplerFetchCachesSuccessfulResponse(t *testing.T) {
	calls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if got := req.URL.Query().Get("project"); got != "tenant-a" {
			t.Errorf("project = %q", got)
		}
		if user, _, ok := req.BasicAuth(); !ok || user != "token" {
			t.Errorf("missing basic auth")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"secrets":{"API_KEY":{"raw":"secret"}}}`)), Request: req}, nil
	})}
	client := &dopplerClient{
		token:      "token",
		config:     "test",
		cache:      make(map[string]map[string]string),
		httpClient: call.New(call.WithHTTPClient(httpClient)),
	}

	first, err := client.fetchProjectSecrets("tenant-a")
	if err != nil || first["API_KEY"] != "secret" {
		t.Fatalf("fetchProjectSecrets() = %#v, %v", first, err)
	}
	second, err := client.fetchProjectSecrets("tenant-a")
	if err != nil || second["API_KEY"] != "secret" || calls != 1 {
		t.Fatalf("cached fetch = %#v, %v; calls=%d", second, err, calls)
	}
}

func TestDopplerRetryability(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		if !isRetryableError(status) {
			t.Errorf("%d should be retryable", status)
		}
	}
	for _, status := range []int{0, 400, 401, 404} {
		if isRetryableError(status) {
			t.Errorf("%d should not be retryable", status)
		}
	}
}

func TestLoadTenantsFromDopplerUsesCachedSecrets(t *testing.T) {
	previous := globalDopplerClient
	globalDopplerClient = &dopplerClient{cache: map[string]map[string]string{
		"code_airborne": {"BRAND_TENANTS": "brand"},
		"brand":         {"AIRBORNE_TENANT_CONFIG": `{"tenant_id":" Brand ","providers":{"openai":{"enabled":true,"api_key":"key","model":"model"}}}`},
	}}
	t.Cleanup(func() { globalDopplerClient = previous })
	configs, err := LoadTenantsFromDoppler()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := configs["brand"]; !ok {
		t.Fatalf("configs = %#v", configs)
	}
}

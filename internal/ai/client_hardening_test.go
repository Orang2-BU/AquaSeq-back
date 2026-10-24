package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func callJevChoice(t *testing.T, action bool, answer string) (string, float64, error) {
	t.Helper()
	question := "doc_type"
	if action {
		question = "action_pick"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"model":"jev-latest","answers":{%q:%s}}`, question, answer)
	}))
	defer server.Close()
	client := NewJevClient(server.URL, "test-key", "", server.Client())
	if action {
		return client.SelectActionLabel(context.Background(), "text", []string{"siphon", "aerasi"})
	}
	return client.ClassifyDocument(context.Background(), "text")
}

func TestJevClientRejectsNullProbability(t *testing.T) {
	for _, action := range []bool{false, true} {
		t.Run(fmt.Sprint("action=", action), func(t *testing.T) {
			choice, probabilities := "lab_report", `{"lab_report":1,"demo_file":0,"worksheet":0,"other":null}`
			if action {
				choice, probabilities = "siphon", `{"siphon":1,"aerasi":null}`
			}
			answer := fmt.Sprintf(`{"type":"choice","choice":%q,"confidence":1,"probabilities":%s}`, choice, probabilities)
			if _, _, err := callJevChoice(t, action, answer); err == nil {
				t.Fatal("accepted null probability as zero")
			}
		})
	}
}

func TestJevClientActionMissingProbabilitiesDistribution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"jev-latest","answers":{"action_pick":{"type":"choice","choice":"siphon","confidence":0.9}}}`))
	}))
	defer server.Close()

	client := NewJevClient(server.URL, "test-key", "", server.Client())
	if _, _, err := client.SelectActionLabel(context.Background(), "text", []string{"siphon", "aerasi"}); err == nil {
		t.Fatal("accepted missing action probabilities distribution")
	}

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"jev-latest","answers":{"action_pick":{"type":"choice","choice":"siphon","confidence":0.9,"probabilities":{"siphon":1.0}}}}`))
	}))
	defer server2.Close()

	client2 := NewJevClient(server2.URL, "test-key", "", server2.Client())
	if _, _, err := client2.SelectActionLabel(context.Background(), "text", []string{"siphon", "aerasi"}); err == nil {
		t.Fatal("accepted incomplete action probabilities distribution")
	}
}

func TestJevClientRejectsExtraUnknownKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"jev-latest","answers":{"action_pick":{"type":"choice","choice":"siphon","confidence":0.9,"probabilities":{"siphon":0.6,"aerasi":0.4,"unknown_key":0.0}}}}`))
	}))
	defer server.Close()

	client := NewJevClient(server.URL, "test-key", "", server.Client())
	if _, _, err := client.SelectActionLabel(context.Background(), "text", []string{"siphon", "aerasi"}); err == nil {
		t.Fatal("accepted extra unknown keys in probabilities")
	}
}

func TestJevClientRejectsOutOfRangeSum(t *testing.T) {
	for _, action := range []bool{false, true} {
		t.Run(fmt.Sprint("action=", action), func(t *testing.T) {
			choice, probabilities := "lab_report", `{"lab_report":0.7,"demo_file":0.7,"worksheet":0,"other":0}`
			if action {
				choice, probabilities = "siphon", `{"siphon":0.7,"aerasi":0.7}`
			}
			answer := fmt.Sprintf(`{"type":"choice","choice":%q,"confidence":1,"probabilities":%s}`, choice, probabilities)
			if _, _, err := callJevChoice(t, action, answer); err == nil {
				t.Fatal("accepted out-of-range sum")
			}
		})
	}
}

func TestJevClientRejectsEmptyOptions(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		fmt.Fprint(w, `{"model":"jev-latest","answers":{}}`)
	}))
	defer server.Close()
	client := NewJevClient(server.URL, "test-key", "", server.Client())
	if _, _, err := client.SelectActionLabel(context.Background(), "text", nil); err == nil {
		t.Fatal("accepted empty options")
	}
	if called.Load() {
		t.Fatal("empty options sent to provider")
	}
}

func TestJevClientRejectsRedirectsAndDoesNotForwardCredentials(t *testing.T) {
	var redirectedCalled atomic.Bool
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedCalled.Store(true)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"model":"jev-latest","answers":{}}`))
	}))
	defer targetServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL+"/v1/systemone", http.StatusFound)
	}))
	defer redirectServer.Close()

	client := NewJevClient(redirectServer.URL, "super-secret-key", "", redirectServer.Client())
	_, _, err := client.ClassifyDocument(context.Background(), "text")
	if err == nil {
		t.Fatal("expected error on redirect, got nil")
	}
	if redirectedCalled.Load() {
		t.Fatal("HTTP client followed redirect; credentials could have been forwarded")
	}
}

func TestJevClientConstructorCopiesPreservingTransportAndTimeout(t *testing.T) {
	transport := &http.Transport{}
	original := &http.Client{
		Transport: transport,
		Timeout:   42 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}

	client := NewJevClient("https://api.typesafe.ai", "test-key", "", original)

	if original.Timeout != 42*time.Second {
		t.Fatalf("original timeout mutated: %v", original.Timeout)
	}
	if original.CheckRedirect == nil || original.CheckRedirect(&http.Request{}, nil) != nil {
		t.Fatal("original CheckRedirect was mutated")
	}

	if client.httpClient.Transport != transport {
		t.Fatal("transport was not preserved")
	}
	if client.httpClient.Timeout != 42*time.Second {
		t.Fatalf("expected timeout 42s, got %v", client.httpClient.Timeout)
	}

	if client.httpClient.CheckRedirect == nil {
		t.Fatal("CheckRedirect not set on copied client")
	}
	if err := client.httpClient.CheckRedirect(&http.Request{}, nil); err == nil {
		t.Fatal("CheckRedirect did not reject redirect")
	}
}

func TestJevClientDoesNotEchoVendorErrorBody(t *testing.T) {
	secretPayload := `{"error":"upstream_token=SECRET_KEY_12345 user_email=patient@example.com"}`
	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				w.Write([]byte(secretPayload))
			}))
			defer server.Close()

			client := NewJevClient(server.URL, "test-key", "", server.Client())
			_, _, err := client.ClassifyDocument(context.Background(), "text")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			errStr := err.Error()
			if strings.Contains(errStr, "SECRET_KEY_12345") || strings.Contains(errStr, "patient@example.com") {
				t.Fatalf("vendor error body was echoed: %s", errStr)
			}
		})
	}
}

func TestJevClientEnforcesProductionHTTPS(t *testing.T) {
	c1 := NewJevClient("http://api.typesafe.ai", "key", "", nil)
	if !strings.HasPrefix(c1.baseURL, "https://") {
		t.Fatalf("expected HTTPS baseURL, got: %s", c1.baseURL)
	}

	localURL := "http://127.0.0.1:54321"
	c2 := NewJevClient(localURL, "key", "", &http.Client{})
	if c2.baseURL != localURL {
		t.Fatalf("injected test client baseURL altered: %s", c2.baseURL)
	}
}

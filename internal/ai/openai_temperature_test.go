package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Model Claude menolak `temperature` (400 sejak 21 Okt 2026); model lain tetap menerimanya.
func TestOpenAIClientOmitsTemperatureForClaude(t *testing.T) {
	for model, wantTemp := range map[string]bool{"ag/claude-opus-5-5": false, "claude-opus-5-5": false, "ag/gemini-3.8-flash-high": true} {
		var body map[string]any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		}))
		client := NewOpenAIClient(ts.URL, "test-key", model, ts.Client())
		if _, err := client.Complete(context.Background(), "sistem", "pengguna"); err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		ts.Close()
		if _, has := body["temperature"]; has != wantTemp {
			t.Errorf("%s: temperature terkirim=%v, ingin %v", model, has, wantTemp)
		}
	}
}

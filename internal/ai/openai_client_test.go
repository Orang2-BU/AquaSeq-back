package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIClientClassifyDocument(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices": [
				{
					"message": {
						"content": "{\"choice\": \"lab_report\", \"confidence\": 0.95}"
					}
				}
			]
		}`))
	}))
	defer ts.Close()

	client := NewOpenAIClient(ts.URL, "test-key", "ag/gemini-3.8-flash-high", ts.Client())
	choice, conf, err := client.ClassifyDocument(context.Background(), "sample qpcr data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != "lab_report" {
		t.Errorf("expected lab_report, got %s", choice)
	}
	if conf != 0.95 {
		t.Errorf("expected 0.95, got %f", conf)
	}
}

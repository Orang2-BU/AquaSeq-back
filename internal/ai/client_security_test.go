package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestJevClientEnforcesValidationAndLimits(t *testing.T) {
	cases := []struct {
		name     string
		respJSON string
		status   int
		wantErr  string
	}{
		{
			name:     "invalid_json",
			respJSON: `not json`,
			status:   http.StatusOK,
			wantErr:  "JSON",
		},
		{
			name:     "missing_answers",
			respJSON: `{"model":"jev-latest"}`,
			status:   http.StatusOK,
			wantErr:  "answers",
		},
		{
			name:     "unsupported_answer_type",
			respJSON: `{"model":"jev-latest","answers":{"doc_type":{"type":"score","score":1.0}}}`,
			status:   http.StatusOK,
			wantErr:  "tipe jawaban tidak sesuai",
		},
		{
			name:     "confidence_out_of_range_high",
			respJSON: `{"model":"jev-latest","answers":{"doc_type":{"type":"choice","choice":"lab_report","confidence":1.5}}}`,
			status:   http.StatusOK,
			wantErr:  "confidence di luar rentang",
		},
		{
			name:     "confidence_out_of_range_negative",
			respJSON: `{"model":"jev-latest","answers":{"doc_type":{"type":"choice","choice":"lab_report","confidence":-0.1}}}`,
			status:   http.StatusOK,
			wantErr:  "confidence di luar rentang",
		},
		{
			name:     "noul_out_of_range",
			respJSON: `{"model":"jev-latest","answers":{"id_match":{"type":"noul","noul":2.0}}}`,
			status:   http.StatusOK,
			wantErr:  "noul di luar rentang",
		},
		{
			name:     "choice_not_in_criteria_enums",
			respJSON: `{"model":"jev-latest","answers":{"action_pick":{"type":"choice","choice":"invented_action","confidence":0.9}}}`,
			status:   http.StatusOK,
			wantErr:  "pilihan tidak valid",
		},
		{
			name:     "http_401_unauthorized",
			respJSON: `{"error":"invalid_api_key"}`,
			status:   http.StatusUnauthorized,
			wantErr:  "401",
		},
		{
			name:     "http_422_unprocessable",
			respJSON: `{"error":"invalid question"}`,
			status:   http.StatusUnprocessableEntity,
			wantErr:  "422",
		},
		{
			name:     "http_429_rate_limit",
			respJSON: `{"error":"rate limit exceeded"}`,
			status:   http.StatusTooManyRequests,
			wantErr:  "429",
		},
		{
			name:     "http_529_overloaded",
			respJSON: `{"error":"overloaded"}`,
			status:   529,
			wantErr:  "529",
		},
		{
			name:     "oversized_response",
			respJSON: `{"model":"jev-latest","answers":{"doc_type":{"type":"choice","choice":"lab_report","confidence":0.9,"padding":"` + strings.Repeat("A", 2<<20) + `"}}`,
			status:   http.StatusOK,
			wantErr:  "terlalu besar",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.respJSON))
			}))
			defer server.Close()

			client := NewJevClient(server.URL, "test-key", "jev-latest", server.Client())
			if strings.Contains(tc.name, "noul") || strings.Contains(tc.respJSON, "id_match") {
				_, _, err := client.ValidateSampleIDMatch(context.Background(), "SMPL-1", "doc text")
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
			} else if strings.Contains(tc.name, "action") || strings.Contains(tc.respJSON, "action_pick") {
				_, _, err := client.SelectActionLabel(context.Background(), "doc text", []string{"siphon", "aerasi"})
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
			} else {
				_, _, err := client.ClassifyDocument(context.Background(), "doc text")
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
			}
		})
	}
}

func TestJevClientOptInOnly(t *testing.T) {
	origProvider := os.Getenv("LLM_PROVIDER")
	origKey := os.Getenv("JEV_API_KEY")
	defer func() {
		os.Setenv("LLM_PROVIDER", origProvider)
		os.Setenv("JEV_API_KEY", origKey)
	}()

	os.Setenv("LLM_PROVIDER", "")
	os.Setenv("JEV_API_KEY", "some-key")
	if client := NewClientFromEnv(); client != nil {
		t.Fatalf("expected nil when LLM_PROVIDER is empty, got: %+v", client)
	}

	os.Setenv("LLM_PROVIDER", "openai")
	if client := NewClientFromEnv(); client != nil {
		t.Fatalf("expected nil when LLM_PROVIDER is not jev, got: %+v", client)
	}

	os.Setenv("LLM_PROVIDER", "jev")
	os.Setenv("JEV_API_KEY", "")
	os.Setenv("TYPESAFE_API_KEY", "")
	if client := NewClientFromEnv(); client != nil {
		t.Fatalf("expected nil when API key is missing, got: %+v", client)
	}
}

func TestJevClientEnforcesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`{"model":"jev-latest","answers":{}}`))
	}))
	defer server.Close()

	client := NewJevClient(server.URL, "test-key", "jev-latest", &http.Client{Timeout: 20 * time.Millisecond})
	_, _, err := client.ClassifyDocument(context.Background(), "text")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

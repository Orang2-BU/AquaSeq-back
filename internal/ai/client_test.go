package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJevClientSystemOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		questions, _ := payload["questions"].(map[string]interface{})
		resp := map[string]interface{}{
			"model":   "jev-latest",
			"answers": map[string]interface{}{},
		}
		answers := resp["answers"].(map[string]interface{})
		if _, ok := questions["doc_type"]; ok {
			answers["doc_type"] = map[string]interface{}{
				"type":          "choice",
				"choice":        "lab_report",
				"confidence":    0.94,
				"probabilities": map[string]float64{"lab_report": 0.94, "demo_file": 0.04, "worksheet": 0.01, "other": 0.01},
			}
		}
		if _, ok := questions["id_match"]; ok {
			answers["id_match"] = map[string]interface{}{
				"type": "noul",
				"noul": 0.88,
			}
		}
		if _, ok := questions["action_pick"]; ok {
			answers["action_pick"] = map[string]interface{}{
				"type":       "choice",
				"choice":     "siphon_and_probiotic",
				"confidence": 0.91,
				"probabilities": map[string]float64{
					"siphon_and_probiotic": 0.91,
					"harvest":              0.09,
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewJevClient(server.URL, "secret-key", "jev-latest", server.Client())
	docType, conf, err := client.ClassifyDocument(context.Background(), "SampleCode, amoA, 16S")
	if err != nil || docType != "lab_report" || conf < 0.9 {
		t.Fatalf("%s %f %v", docType, conf, err)
	}
	matched, p, err := client.ValidateSampleIDMatch(context.Background(), "SMPL-01", "Sheet Sample SMPL-01")
	if err != nil || !matched || p < 0.8 {
		t.Fatalf("%v %f %v", matched, p, err)
	}
	label, lConf, err := client.SelectActionLabel(context.Background(), "TOM tinggi", []string{"siphon_and_probiotic", "harvest"})
	if err != nil || label != "siphon_and_probiotic" || lConf < 0.9 {
		t.Fatalf("%s %f %v", label, lConf, err)
	}
}

func TestJevClientDisabledOrTimeoutGraceful(t *testing.T) {
	client := NewJevClient("", "", "", nil)
	docType, conf, err := client.ClassifyDocument(context.Background(), "test")
	if err == nil || docType != "" || conf != 0 {
		t.Fatalf("expected unconfigured error: %v", err)
	}
}

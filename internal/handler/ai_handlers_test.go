package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"aquaseq-back/internal/ai"
	"aquaseq-back/internal/learning"
)

type mockPondProvider struct {
	points []learning.Sample
	facts  ai.MachineFacts
}

func (m *mockPondProvider) GetTrajectoryPoints(ctx context.Context, pondID string) ([]learning.Sample, error) {
	return m.points, nil
}

func (m *mockPondProvider) GetLatestMachineFacts(ctx context.Context, pondID string) (ai.MachineFacts, error) {
	return m.facts, nil
}

func TestAIDocumentIngestEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	deps := AIDependencies{
		Auth: func(r *http.Request) (string, string, error) {
			return "user-1", "owner", nil
		},
	}
	RegisterAI(mux, deps)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "gsi_run.csv")
	part.Write([]byte("SampleCode,ct_ahpnd,ct_ppc\nSMPL-01,32.0,19.9\n"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/v1/documents/ingest", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("ingest failed: %+v", resp.Error)
	}
}

func TestAIExplainEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	facts := ai.MachineFacts{
		PondID:                 "POND-01",
		Disease:                "AHPND",
		SDRIClass:              2,
		RiskLevel:              "WASPADA_TINGGI",
		ProbabilityPct:         35.0,
		NH3MgL:                 0.08,
		TOMMgL:                 80.0,
		DOMgL:                  4.0,
		ForecastHorizonDays:    7,
		ForecastProbabilityPct: 45.0,
		DataAvailable:          true,
		QCStatus:               "VALID",
	}

	deps := AIDependencies{
		Auth: func(r *http.Request) (string, string, error) {
			return "user-1", "pemilik", nil
		},
		AuthorizePond: func(ctx context.Context, userID, pondID string, owner bool) error {
			return nil
		},
		PondData: &mockPondProvider{facts: facts},
	}
	RegisterAI(mux, deps)

	req := httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewReader([]byte(`{"pond_id":"POND-01"}`)))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp APIResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Success {
		t.Fatalf("explain failed: %+v", resp.Error)
	}
}

func TestAIPondInsightsEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	caseBase := &learning.CaseBase{}
	_ = caseBase.Add(learning.Case{
		ID:         "CASE-01",
		FarmID:     "FARM-01",
		PondID:     "KOLAM-1",
		CycleID:    "cycle-1",
		Scope:      learning.CaseScopePrivate,
		Provenance: "curated_field_observation",
		Features:   map[string]float64{"tom": 80.0, "do": 4.0},
		SDRIClass:  2,
		Action:     "Siphon dan kurangi pakan",
		Outcome:    "HARVEST_SUCCESS",
		Synthetic:  true,
	})

	var points []learning.Sample
	for i := 0; i < 5; i++ {
		points = append(points, learning.Sample{
			Day:            float64(i * 7),
			ProbabilityPct: float64(10 + i*5),
			RiskClass:      1,
			Synthetic:      true,
			Features:       map[string]float64{"tom": float64(60 + i*5), "do": float64(6 - i)},
		})
	}

	provider := &mockPondProvider{
		points: points,
		facts: ai.MachineFacts{
			PondID:                 "KOLAM-1",
			Disease:                "AHPND",
			SDRIClass:              2,
			RiskLevel:              "WASPADA_TINGGI",
			ProbabilityPct:         30.0,
			NH3MgL:                 0.05,
			TOMMgL:                 80.0,
			DOMgL:                  4.0,
			ForecastHorizonDays:    7,
			ForecastProbabilityPct: 35.0,
			DataAvailable:          true,
			QCStatus:               "VALID",
		},
	}

	deps := AIDependencies{
		Auth: func(r *http.Request) (string, string, error) {
			return "user-1", "pemilik", nil
		},
		AuthorizePond: func(ctx context.Context, userID, pondID string, owner bool) error {
			return nil
		},
		PondData: provider,
		CaseBase: caseBase,
	}
	RegisterAI(mux, deps)

	req := httptest.NewRequest("GET", "/api/v1/ponds/KOLAM-1/insights", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp APIResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Success {
		t.Fatalf("insights failed: %+v", resp.Error)
	}
}

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"aquaseq-back/internal/ai"
	"aquaseq-back/internal/learning"
)

func TestAIRoutesFailClosedWithoutAuth(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAI(mux, AIDependencies{})
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/v1/documents/ingest", ""},
		{"POST", "/api/v1/ai/explain", `{"pond_id":"pond-1"}`},
		{"GET", "/api/v1/ponds/pond-1/insights", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body)))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAIExplainSecurityAndFactEnforcement(t *testing.T) {
	stubAuth := func(r *http.Request) (string, string, error) {
		return "user-1", "pemilik", nil
	}

	validFacts := ai.MachineFacts{
		PondID:                 "POND-1",
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

	provider := &mockPondProvider{
		facts: validFacts,
	}

	// 1. AuthorizePond returns error -> must return 403 Forbidden
	muxForbidden := http.NewServeMux()
	RegisterAI(muxForbidden, AIDependencies{
		Auth: stubAuth,
		AuthorizePond: func(ctx context.Context, userID, pondID string, owner bool) error {
			return errors.New("pond not owned")
		},
		PondData: provider,
	})
	w := httptest.NewRecorder()
	muxForbidden.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewBufferString(`{"pond_id":"POND-1"}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("unauthorized pond: want 403, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Client passes extra risk/disease fields -> must be rejected with 400 Bad Request
	muxAuthorized := http.NewServeMux()
	RegisterAI(muxAuthorized, AIDependencies{
		Auth: stubAuth,
		AuthorizePond: func(ctx context.Context, userID, pondID string, owner bool) error {
			return nil
		},
		PondData: provider,
	})

	wClientExtras := httptest.NewRecorder()
	muxAuthorized.ServeHTTP(wClientExtras, httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewBufferString(`{"pond_id":"POND-1","risk_level":"AMAN","disease":"NONE"}`)))
	if wClientExtras.Code != http.StatusBadRequest {
		t.Fatalf("client risk/disease extras: want 400, got %d: %s", wClientExtras.Code, wClientExtras.Body.String())
	}

	// 3. Valid pond_id only -> reads facts from provider, succeeds 200
	wValid := httptest.NewRecorder()
	muxAuthorized.ServeHTTP(wValid, httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewBufferString(`{"pond_id":"POND-1"}`)))
	if wValid.Code != http.StatusOK {
		t.Fatalf("valid explain: want 200, got %d: %s", wValid.Code, wValid.Body.String())
	}

	// 4. Provider facts invalid/QC rejected -> must return 422 Unprocessable Entity
	qcFailProvider := &mockPondProvider{
		facts: ai.MachineFacts{
			PondID:        "POND-1",
			DataAvailable: false,
			QCStatus:      "REJECTED_INHIBITOR",
		},
	}
	muxQCFail := http.NewServeMux()
	RegisterAI(muxQCFail, AIDependencies{
		Auth: stubAuth,
		AuthorizePond: func(ctx context.Context, userID, pondID string, owner bool) error {
			return nil
		},
		PondData: qcFailProvider,
	})
	wQCFail := httptest.NewRecorder()
	muxQCFail.ServeHTTP(wQCFail, httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewBufferString(`{"pond_id":"POND-1"}`)))
	if wQCFail.Code != http.StatusUnprocessableEntity {
		t.Fatalf("QC fail: want 422, got %d: %s", wQCFail.Code, wQCFail.Body.String())
	}
}

func TestAIInvalidServerFactsNeverProduceNarrative(t *testing.T) {
	p := &mockPondProvider{facts: ai.MachineFacts{PondID: "POND-1", Disease: "invented", SDRIClass: 1, RiskLevel: "AMAN", DataAvailable: true, QCStatus: "VALID"}}
	for i := 0; i < 4; i++ {
		p.points = append(p.points, learning.Sample{Day: float64(i * 7), ProbabilityPct: 10, RiskClass: 1})
	}
	mux := http.NewServeMux()
	RegisterAI(mux, AIDependencies{Auth: func(*http.Request) (string, string, error) { return "user", "pemilik", nil }, AuthorizePond: func(context.Context, string, string, bool) error { return nil }, PondData: p})
	
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ponds/POND-1/insights", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid server facts must fail validation: %d %s", w.Code, w.Body.String())
	}

	// POST explain: invalid server facts must return 422
	wExp := httptest.NewRecorder()
	mux.ServeHTTP(wExp, httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewBufferString(`{"pond_id":"POND-1"}`)))
	if wExp.Code != 422 {
		t.Fatalf("invalid server facts in explain must fail with 422, got %d: %s", wExp.Code, wExp.Body.String())
	}

	// POST explain: trailing body rejected with 400
	for _, body := range []string{`{"pond_id":"POND-1"} {"risk_class":1}`, `{"pond_id":"POND-1"} trailing`} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewBufferString(body)))
		if w.Code != 400 {
			t.Fatalf("trailing body accepted: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAIPondInsightsStatusAndNullableNarrative(t *testing.T) {
	stubAuth := func(r *http.Request) (string, string, error) {
		return "user-1", "pemilik", nil
	}
	stubAuthPond := func(ctx context.Context, userID, pondID string, owner bool) error {
		return nil
	}

	// Case 1: Insufficient data (<4 points) -> status INSUFFICIENT_DATA, forecast null, explanation null
	insufficientProvider := &mockPondProvider{
		points: []learning.Sample{
			{Day: 7, ProbabilityPct: 15.0, RiskClass: 1, Features: map[string]float64{"tom": 40.0}},
		},
		facts: ai.MachineFacts{
			PondID:        "POND-1",
			DataAvailable: true,
			QCStatus:      "VALID",
			SDRIClass:     1,
			RiskLevel:     "AMAN",
			Disease:       "AHPND",
		},
	}
	muxInsufficient := http.NewServeMux()
	RegisterAI(muxInsufficient, AIDependencies{
		Auth:          stubAuth,
		AuthorizePond: stubAuthPond,
		PondData:      insufficientProvider,
	})

	wIns := httptest.NewRecorder()
	muxInsufficient.ServeHTTP(wIns, httptest.NewRequest("GET", "/api/v1/ponds/POND-1/insights", nil))
	if wIns.Code != http.StatusOK {
		t.Fatalf("insufficient data: want 200, got %d: %s", wIns.Code, wIns.Body.String())
	}

	var respIns APIResponse
	if err := json.Unmarshal(wIns.Body.Bytes(), &respIns); err != nil {
		t.Fatal(err)
	}
	insData, ok := respIns.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %v", respIns.Data)
	}
	if insData["status"] != "INSUFFICIENT_DATA" {
		t.Fatalf("expected status INSUFFICIENT_DATA, got %v", insData["status"])
	}
	fc7, _ := insData["forecast_7d"].(map[string]any)
	if fc7["predicted_probability_pct"] != nil {
		t.Fatalf("expected forecast predicted_probability_pct to be nil, got %v", fc7["predicted_probability_pct"])
	}
	if insData["explanation"] != nil {
		t.Fatalf("expected explanation to be null on insufficient data, got %v", insData["explanation"])
	}

	// Case 2: QC Rejected -> status QC_REJECTED, explanation null, no fabricated safe fallback
	qcRejectedProvider := &mockPondProvider{
		points: []learning.Sample{
			{Day: 7, ProbabilityPct: 15.0, RiskClass: 1, Features: map[string]float64{"tom": 40.0}},
			{Day: 14, ProbabilityPct: 18.0, RiskClass: 1, Features: map[string]float64{"tom": 42.0}},
			{Day: 21, ProbabilityPct: 20.0, RiskClass: 1, Features: map[string]float64{"tom": 44.0}},
			{Day: 28, ProbabilityPct: 22.0, RiskClass: 1, Features: map[string]float64{"tom": 46.0}},
		},
		facts: ai.MachineFacts{
			PondID:        "POND-1",
			DataAvailable: false,
			QCStatus:      "REJECTED_INHIBITOR",
		},
	}
	muxQC := http.NewServeMux()
	RegisterAI(muxQC, AIDependencies{
		Auth:          stubAuth,
		AuthorizePond: stubAuthPond,
		PondData:      qcRejectedProvider,
	})

	wQC := httptest.NewRecorder()
	muxQC.ServeHTTP(wQC, httptest.NewRequest("GET", "/api/v1/ponds/POND-1/insights", nil))
	if wQC.Code != http.StatusOK {
		t.Fatalf("qc rejected: want 200, got %d: %s", wQC.Code, wQC.Body.String())
	}

	var respQC APIResponse
	if err := json.Unmarshal(wQC.Body.Bytes(), &respQC); err != nil {
		t.Fatal(err)
	}
	qcData := respQC.Data.(map[string]any)
	if qcData["status"] != "QC_REJECTED" {
		t.Fatalf("expected status QC_REJECTED, got %v", qcData["status"])
	}
	if qcData["explanation"] != nil {
		t.Fatalf("expected explanation to be null on QC_REJECTED, got %v", qcData["explanation"])
	}
}

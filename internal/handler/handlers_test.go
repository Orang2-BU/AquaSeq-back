package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"aquaseq-back/internal/cache"
	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/repository"
	"aquaseq-back/internal/service"
)

func floatPtr(v float64) *float64 {
	return &v
}

func setupTestServer() http.Handler {
	repo := repository.NewInMemRepository()
	appCache := cache.New()
	svc := service.NewAppService(repo, appCache)
	h := NewHandler(svc)
	return h.RegisterRoutes()
}

func TestHealthEndpoint(t *testing.T) {
	ts := setupTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	ts.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success true, got false")
	}
}

func TestCalculateSDRIEndpoint(t *testing.T) {
	ts := setupTestServer()

	// 1. Valid calculation
	body, _ := json.Marshal(domain.SDRIInput{
		CTPPC: 19.95,
		SQPCR: 0.0,
		SBio:  -2.10588,
		SChem: 0.65714,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sdri/calculate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ts.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp APIResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	dataMap := resp.Data.(map[string]interface{})
	if dataMap["status"] != "VALID" {
		t.Errorf("expected status VALID, got %v", dataMap["status"])
	}
	if dataMap["risk_class"].(float64) != 0 {
		t.Errorf("expected class 0, got %v", dataMap["risk_class"])
	}

	// 2. PPC Inhibited Rejection
	bodyInhibit, _ := json.Marshal(domain.SDRIInput{
		CTPPC: 24.46, // QIAGEN > 22.0
		SQPCR: 1.0,
		SBio:  1.0,
		SChem: 1.0,
	})

	reqInhibit := httptest.NewRequest(http.MethodPost, "/api/v1/sdri/calculate", bytes.NewReader(bodyInhibit))
	wInhibit := httptest.NewRecorder()

	ts.ServeHTTP(wInhibit, reqInhibit)

	var respInhibit APIResponse
	_ = json.NewDecoder(wInhibit.Body).Decode(&respInhibit)

	dataInhibitMap := respInhibit.Data.(map[string]interface{})
	if dataInhibitMap["status"] != "REJECTED_INHIBITOR" {
		t.Errorf("expected status REJECTED_INHIBITOR, got %v", dataInhibitMap["status"])
	}
}

func TestSampleIngestionAndTrajectory(t *testing.T) {
	ts := setupTestServer()

	pondID := "pond-c3"

	// Post 3 samples to build trajectory
	days := []int{30, 35, 40}
	ahpndCts := []float64{40.0, 32.0, 24.0} // worsening AHPND

	for i := 0; i < 3; i++ {
		tom := 60.0 + float64(i)*25.0
		sample := domain.Sample{
			PondID:       pondID,
			SamplingDate: "2026-10-10",
			CycleDay:     days[i],
			Features: domain.SampleFeatures{
				CTAHPND: floatPtr(ahpndCts[i]),
				CTamoA:  floatPtr(26.0),
				CT16S:   floatPtr(18.4),
				CTPPC:   19.95,
				TempC:   floatPtr(30.0),
				PH:      floatPtr(8.2),
				TANMgL:  floatPtr(1.2),
				TOMMgL:  &tom,
			},
		}

		body, _ := json.Marshal(sample)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/samples", bytes.NewReader(body))
		w := httptest.NewRecorder()

		ts.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("failed to ingest sample %d: code %d, body %s", i, w.Code, w.Body.String())
		}
	}

	// Fetch trajectory
	reqTraj := httptest.NewRequest(http.MethodGet, "/api/v1/ponds/pond-c3/trajectory", nil)
	wTraj := httptest.NewRecorder()

	ts.ServeHTTP(wTraj, reqTraj)

	if wTraj.Code != http.StatusOK {
		t.Fatalf("expected status 200 on trajectory, got %d", wTraj.Code)
	}

	var respTraj APIResponse
	_ = json.NewDecoder(wTraj.Body).Decode(&respTraj)

	dataTraj := respTraj.Data.(map[string]interface{})
	if dataTraj["data_points"].(float64) != 3 {
		t.Errorf("expected 3 data points, got %v", dataTraj["data_points"])
	}
}

func TestConcurrency1000Requests(t *testing.T) {
	ts := setupTestServer()

	concurrency := 1000
	var wg sync.WaitGroup
	wg.Add(concurrency)

	errChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()

			body, _ := json.Marshal(domain.SDRIInput{
				CTPPC: 19.95,
				SQPCR: float64(idx%5) * 0.5,
				SBio:  -1.5,
				SChem: 1.0,
			})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/sdri/calculate", bytes.NewReader(body))
			w := httptest.NewRecorder()

			ts.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("request %d failed with code %d", idx, w.Code)
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			t.Fatalf("concurrency error: %v", err)
		}
	}
}

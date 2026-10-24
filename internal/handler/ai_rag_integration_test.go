package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"aquaseq-back/internal/ai"
	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/learning"
	"aquaseq-back/internal/repository"
)

func TestRAGPersistentReconstructionAndCrossfarmProof(t *testing.T) {
	runRAGFlow(t, repository.NewMemoryStore())

	if pgURL := os.Getenv("DATABASE_URL_TEST"); pgURL != "" {
		t.Run("PostgreSQL", func(t *testing.T) {
			store, err := repository.OpenPostgres(context.Background(), pgURL, 4)
			if err != nil {
				t.Fatalf("failed to connect to test postgres: %v", err)
			}
			defer store.Close()
			runRAGFlow(t, store)
		})
	}
}

func runRAGFlow(t *testing.T, store repository.WebStore) {
	ctx := context.Background()
	jwtAuth, err := auth.New("test-jwt-secret-min-16-bytes-ok")
	if err != nil {
		t.Fatal(err)
	}

	farmAID := "farm-rag-a-" + generateUUID()[:8]
	farmBID := "farm-rag-b-" + generateUUID()[:8]
	farmSID := "farm-rag-s-" + generateUUID()[:8]

	userAID := "user-rag-a-" + generateUUID()[:8]
	userBID := "user-rag-b-" + generateUUID()[:8]

	pondA1ID := "pond-rag-a1-" + generateUUID()[:8]
	pondB1ID := "pond-rag-b1-" + generateUUID()[:8]
	pondS1ID := "pond-rag-s1-" + generateUUID()[:8]

	// 1. Seed Farms, Users, Memberships, Ponds into store
	err = store.Write(ctx, func(tx repository.WebTx) error {
		// Users
		_ = tx.Put("users", repository.Record{"id": userAID, "full_name": "Owner A", "email": userAID + "@test.com", "password_hash": "hash"})
		_ = tx.Put("users", repository.Record{"id": userBID, "full_name": "Owner B", "email": userBID + "@test.com", "password_hash": "hash"})

		// Farms (A=nyata, B=nyata, S=simulasi)
		_ = tx.Put("farms", repository.Record{"id": farmAID, "name": "Tambak Real A", "owner_id": userAID, "provenance": "nyata"})
		_ = tx.Put("farms", repository.Record{"id": farmBID, "name": "Tambak Real B", "owner_id": userBID, "provenance": "nyata"})
		_ = tx.Put("farms", repository.Record{"id": farmSID, "name": "Tambak Simulasi S", "owner_id": userAID, "provenance": "simulasi"})

		// Memberships
		_ = tx.Put("memberships", repository.Record{"id": "m-" + userAID, "farm_id": farmAID, "user_id": userAID, "role": "pemilik", "active": true})
		_ = tx.Put("memberships", repository.Record{"id": "m-" + userBID, "farm_id": farmBID, "user_id": userBID, "role": "pemilik", "active": true})
		_ = tx.Put("memberships", repository.Record{"id": "m-s-" + userAID, "farm_id": farmSID, "user_id": userAID, "role": "pemilik", "active": true})

		// Ponds: pond A1 starts at current_cycle: 2 (cycle 1 is past/completed)
		_ = tx.Put("ponds", repository.Record{"id": pondA1ID, "farm_id": farmAID, "pond_code": "A1", "current_cycle": 2, "status": "ACTIVE"})
		_ = tx.Put("ponds", repository.Record{"id": pondB1ID, "farm_id": farmBID, "pond_code": "B1", "current_cycle": 1, "status": "ACTIVE"})
		_ = tx.Put("ponds", repository.Record{"id": pondS1ID, "farm_id": farmSID, "pond_code": "S1", "current_cycle": 2, "status": "ACTIVE"})

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Cycle 1 on Pond A1:
	// a. Ingest sample in cycle 1
	sampleA1Cycle1ID := "smp-a1-c1-" + generateUUID()[:8]
	err = store.Write(ctx, func(tx repository.WebTx) error {
		_ = tx.Put("samples", repository.Record{
			"id":            sampleA1Cycle1ID,
			"pond_id":       pondA1ID,
			"sample_code":   "SMP-A1-C1",
			"sampling_date": "2026-09-01",
			"cycle_day":     30,
			"meta":          map[string]any{"cycle_number": 1},
			"created_at":    now(),
		})
		_ = tx.Put("sample_features", repository.Record{
			"id":           "feat-" + sampleA1Cycle1ID,
			"sample_id":    sampleA1Cycle1ID,
			"ct_ahpnd":     22.0,
			"ct_wssv":      40.0,
			"ct_ehp":       30.0,
			"ct_amoa":      28.22,
			"ct_16s":       18.44,
			"ct_ppc":       19.95,
			"do_mg_l":      4.2,
			"tom_mg_l":     82.0,
			"ph":           7.8,
			"salinity_ppt": 22.5,
			"temp_c":       29.5,
			"tan_mg_l":     0.5,
			"no2_mg_l":     0.2,
			"h2s_mg_l":     0.005,
			"alkalinity":   120.0,
			"created_at":   now(),
		})
		_ = tx.Put("sdri_results", repository.Record{
			"id":              "res-" + sampleA1Cycle1ID,
			"sample_id":       sampleA1Cycle1ID,
			"status":          "VALID",
			"probability_pct": 38.0,
			"risk_class":      2,
			"risk_level":      "WASPADA_TINGGI",
			"disease_name":    "AHPND",
			"created_at":      now(),
		})

		// b. Task created with source_sample_id and performed report
		taskA1ID := "task-a1-c1-" + generateUUID()[:8]
		_ = tx.Put("tasks", repository.Record{
			"id":               taskA1ID,
			"farm_id":          farmAID,
			"pond_id":          pondA1ID,
			"source_sample_id": sampleA1Cycle1ID,
			"title":            "Siphon dan pengenceran",
			"instruction":      "Siphon lumpur dasar dan turunkan pakan 20%",
			"status":           "selesai",
			"assignee_id":      userAID,
			"created_by":       userAID,
			"due_at":           time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"created_at":       now(),
		})
		_ = tx.Put("task_reports", repository.Record{
			"id":          "rep-" + taskA1ID,
			"task_id":     taskA1ID,
			"note":        "Telah dilakukan siphon dasar petak A1 dan pakan dikurangi 20%",
			"reported_by": userAID,
			"reported_at": now(),
		})

		// c. Cycle 1 outcome recorded
		_ = tx.Put("cycle_outcomes", repository.Record{
			"id":           "co-a1-c1-" + generateUUID()[:8],
			"pond_id":      pondA1ID,
			"cycle_number": 1,
			"outcome":      "HARVEST_SUCCESS",
			"notes":        "Panen sukses parsial hari 75",
			"created_at":   now(),
		})

		// d. Also add an INCOMPLETE sample in cycle 1 (no task performed) -> should be skipped
		sampleIncompleteID := "smp-a1-c1-incomplete-" + generateUUID()[:8]
		_ = tx.Put("samples", repository.Record{
			"id":          sampleIncompleteID,
			"pond_id":     pondA1ID,
			"sample_code": "SMP-A1-INCOMP",
			"meta":        map[string]any{"cycle_number": 1},
			"created_at":  now(),
		})
		_ = tx.Put("sample_features", repository.Record{
			"id":        "feat-" + sampleIncompleteID,
			"sample_id": sampleIncompleteID,
			"do_mg_l":   5.0,
		})
		_ = tx.Put("sdri_results", repository.Record{
			"id":         "res-" + sampleIncompleteID,
			"sample_id":  sampleIncompleteID,
			"status":     "VALID",
			"risk_class": 1,
		})

		// e. Add a QC-rejected sample in cycle 1 -> should be skipped
		sampleQCFailID := "smp-a1-c1-qc-" + generateUUID()[:8]
		_ = tx.Put("samples", repository.Record{
			"id":          sampleQCFailID,
			"pond_id":     pondA1ID,
			"sample_code": "SMP-A1-QCFAIL",
			"meta":        map[string]any{"cycle_number": 1},
			"created_at":  now(),
		})
		_ = tx.Put("sdri_results", repository.Record{
			"id":         "res-" + sampleQCFailID,
			"sample_id":  sampleQCFailID,
			"status":     "REJECTED_INHIBITOR",
			"risk_class": 0,
		})

		// f. Add a synthetic case from farm S (simulasi)
		sampleSID := "smp-s1-c1-" + generateUUID()[:8]
		taskSID := "task-s1-c1-" + generateUUID()[:8]
		_ = tx.Put("samples", repository.Record{
			"id":          sampleSID,
			"pond_id":     pondS1ID,
			"sample_code": "SMP-S1-C1",
			"meta":        map[string]any{"cycle_number": 1},
			"created_at":  now(),
		})
		_ = tx.Put("sample_features", repository.Record{
			"id":        "feat-" + sampleSID,
			"sample_id": sampleSID,
			"ct_ahpnd":  24.5,
			"do_mg_l":   4.2,
			"tom_mg_l":  82.0,
		})
		_ = tx.Put("sdri_results", repository.Record{
			"id":         "res-" + sampleSID,
			"sample_id":  sampleSID,
			"status":     "VALID",
			"risk_class": 2,
		})
		_ = tx.Put("tasks", repository.Record{
			"id":               taskSID,
			"farm_id":          farmSID,
			"pond_id":          pondS1ID,
			"source_sample_id": sampleSID,
			"status":           "selesai",
		})
		_ = tx.Put("task_reports", repository.Record{
			"id":      "rep-" + taskSID,
			"task_id": taskSID,
			"note":    "Tindakan simulasi sintetik",
		})
		_ = tx.Put("cycle_outcomes", repository.Record{
			"id":           "co-s1-c1-" + generateUUID()[:8],
			"pond_id":      pondS1ID,
			"cycle_number": 1,
			"outcome":      "HARVEST_SUCCESS",
		})

		// 3. Cycle 2 samples for Pond A1 (current cycle 2): 4 points for trajectory
		for i := 1; i <= 4; i++ {
			smpID := fmt.Sprintf("smp-a1-c2-%d-%s", i, generateUUID()[:8])
			_ = tx.Put("samples", repository.Record{
				"id":            smpID,
				"pond_id":       pondA1ID,
				"sample_code":   fmt.Sprintf("SMP-A1-C2-%d", i),
				"sampling_date": fmt.Sprintf("2026-10-%02d", i*5),
				"cycle_day":     i * 7,
				"meta":          map[string]any{"cycle_number": 2},
				"created_at":    now(),
			})
			_ = tx.Put("sample_features", repository.Record{
				"id":           "feat-" + smpID,
				"sample_id":    smpID,
				"ct_ahpnd":     22.0,
				"ct_wssv":      40.0,
				"ct_ehp":       30.0,
				"ct_amoa":      28.22,
				"ct_16s":       18.44,
				"ct_ppc":       19.95,
				"do_mg_l":      4.5,
				"tom_mg_l":     82.0,
				"ph":           7.8,
				"salinity_ppt": 22.5,
				"temp_c":       29.5,
				"tan_mg_l":     0.5,
				"no2_mg_l":     0.2,
				"h2s_mg_l":     0.005,
				"alkalinity":   120.0,
			})
			_ = tx.Put("sdri_results", repository.Record{
				"id":                "res-" + smpID,
				"sample_id":         smpID,
				"status":            "VALID",
				"probability_pct":   36.0,
				"risk_class":        2,
				"risk_level":        "WASPADA_TINGGI",
				"predicted_disease": "AHPND",
				"disease_name":      "AHPND",
			})
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. SIMULATE FRESH RESTART:
	// A new LiveHandler, new StorePondProvider, new CaseBase (empty in RAM) pointing to the same store
	freshCaseBase := &learning.CaseBase{}
	freshProvider := NewStorePondProvider(store)

	var freshLiveHandler *LiveHandler
	cfg := Config{
		Auth:        jwtAuth,
		DemoEnabled: true,
		FilesDir:    t.TempDir(),
	}
	freshLiveHandler = NewLiveHandler(store, cfg)

	mux := http.NewServeMux()
	aiDeps := AIDependencies{
		Auth:          freshLiveHandler.Authenticate,
		AuthorizePond: freshLiveHandler.AuthorizePond,
		Decider:       ai.NewClientFromEnv(),
		PondData:      freshProvider,
		CaseBase:      freshCaseBase,
	}
	RegisterAI(mux, aiDeps)

	tokenA, _ := jwtAuth.Access(userAID)
	tokenB, _ := jwtAuth.Access(userBID)

	// 5. Query insights for Pond A1 as User A (owner of farm A):
	reqA := httptest.NewRequest("GET", "/api/v1/ponds/"+pondA1ID+"/insights", nil)
	reqA.Header.Set("Authorization", "Bearer "+tokenA)
	wA := httptest.NewRecorder()
	mux.ServeHTTP(wA, reqA)

	if wA.Code != http.StatusOK {
		t.Fatalf("insights for Pond A1 failed: %d, body: %s", wA.Code, wA.Body.String())
	}

	var respA APIResponse
	if err := json.Unmarshal(wA.Body.Bytes(), &respA); err != nil {
		t.Fatal(err)
	}
	dataA := respA.Data.(map[string]any)

	if dataA["status"] != "OK" {
		t.Fatalf("expected status OK, got %v", dataA["status"])
	}
	if forecast := dataA["forecast_7d"].(map[string]any); forecast["data_points"] != float64(4) {
		t.Fatalf("trajectory mixed cycles: %v", forecast)
	}
	if explanation := dataA["explanation"].(map[string]any); !strings.Contains(explanation["evidence_reason"].(string), "36,0%") {
		t.Fatalf("facts must come from current cycle, not historical sample: %v", explanation)
	}

	// Verify similar cases:
	simCases, ok := dataA["similar_cases"].([]any)
	if !ok || len(simCases) == 0 {
		t.Fatalf("expected at least 1 similar case reconstructed on demand, got %v", dataA["similar_cases"])
	}

	firstCaseMatch := simCases[0].(map[string]any)
	caseObj := firstCaseMatch["case"].(map[string]any)

	// Key derived from farm pond cycle:
	expectedCaseKey := fmt.Sprintf("%s-%s-1", farmAID, pondA1ID)
	if caseObj["id"] != expectedCaseKey {
		t.Errorf("expected case ID %s, got %v", expectedCaseKey, caseObj["id"])
	}
	if caseObj["farm_id"] != farmAID {
		t.Errorf("expected farm_id %s, got %v", farmAID, caseObj["farm_id"])
	}
	if caseObj["pond_id"] != pondA1ID {
		t.Errorf("expected pond_id %s, got %v", pondA1ID, caseObj["pond_id"])
	}
	if caseObj["outcome"] != "HARVEST_SUCCESS" {
		t.Errorf("expected outcome HARVEST_SUCCESS, got %v", caseObj["outcome"])
	}
	expectedAction := "Telah dilakukan siphon dasar petak A1 dan pakan dikurangi 20%"
	if caseObj["action"] != expectedAction {
		t.Errorf("expected action %q, got %v", expectedAction, caseObj["action"])
	}
	if caseObj["synthetic"] == true {
		t.Errorf("expected real case (synthetic=false), got synthetic=true")
	}

	// Verify no synthetic case from farm S was retrieved
	for _, sc := range simCases {
		c := sc.(map[string]any)["case"].(map[string]any)
		if c["farm_id"] == farmSID || c["synthetic"] == true {
			t.Fatalf("SYNTHETIC EXCLUSION VIOLATED: synthetic case retrieved in real query: %+v", c)
		}
		// Verify no current cycle (cycle 2) was retrieved as historical case
		if c["cycle_id"] == "cycle-2" {
			t.Fatalf("CURRENT CYCLE LEAKAGE: current cycle retrieved as history: %+v", c)
		}
	}

	// 6. Crossfarm 403: User B tries to query Pond A1:
	reqB := httptest.NewRequest("GET", "/api/v1/ponds/"+pondA1ID+"/insights", nil)
	reqB.Header.Set("Authorization", "Bearer "+tokenB)
	wB := httptest.NewRecorder()
	mux.ServeHTTP(wB, reqB)
	if wB.Code != http.StatusForbidden {
		t.Fatalf("crossfarm query: want 403 Forbidden, got %d: %s", wB.Code, wB.Body.String())
	}

	// 7. Crossfarm 403 on POST /api/v1/ai/explain:
	reqExplainB := httptest.NewRequest("POST", "/api/v1/ai/explain", bytes.NewBufferString(fmt.Sprintf(`{"pond_id":%q}`, pondA1ID)))
	reqExplainB.Header.Set("Authorization", "Bearer "+tokenB)
	wExpB := httptest.NewRecorder()
	mux.ServeHTTP(wExpB, reqExplainB)
	if wExpB.Code != http.StatusForbidden {
		t.Fatalf("crossfarm explain: want 403 Forbidden, got %d: %s", wExpB.Code, wExpB.Body.String())
	}
}

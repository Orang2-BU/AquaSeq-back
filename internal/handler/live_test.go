package handler

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/repository"
)

func newTestLiveServer(t *testing.T) (*httptest.Server, *repository.MemoryStore, *auth.Auth) {
	store := repository.NewMemoryStore()
	jwtAuth, err := auth.New("test-jwt-secret-minimum-16-bytes-ok")
	if err != nil {
		t.Fatalf("auth setup failed: %v", err)
	}

	lh := NewLiveHandler(store, Config{
		Auth:        jwtAuth,
		DemoEnabled: true,
		FilesDir:    t.TempDir(),
	})

	srv := httptest.NewServer(lh.Routes())
	t.Cleanup(srv.Close)
	return srv, store, jwtAuth
}

func doJSON(t *testing.T, method, url, token string, body any) (int, map[string]any) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	return resp.StatusCode, res
}

func TestAuthDemoAndSeeding(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)

	// 1. Login demo pemilik
	status, res := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{
		"role": "pemilik",
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %v", status, res)
	}
	data, ok := res["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %v", res)
	}
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatal("missing access_token")
	}
	user, _ := data["user"].(map[string]any)
	if user["full_name"] != "Hendra Kusuma" {
		t.Errorf("expected Hendra Kusuma, got %v", user["full_name"])
	}

	// 2. GET /farms shows seeded simulation farms
	status, resFarms := doJSON(t, "GET", srv.URL+"/api/v1/farms", token, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %v", status, resFarms)
	}
	farmsData, _ := resFarms["data"].(map[string]any)
	items, _ := farmsData["items"].([]any)
	if len(items) < 2 {
		t.Fatalf("expected at least 2 seeded farms, got %d", len(items))
	}

	// 3. Login demo teknisi
	statusTeknisi, resTeknisi := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{
		"role": "teknisi",
	})
	if statusTeknisi != http.StatusOK {
		t.Fatalf("expected 200 for teknisi, got %d", statusTeknisi)
	}
	dataTek, _ := resTeknisi["data"].(map[string]any)
	userTek, _ := dataTek["user"].(map[string]any)
	if userTek["full_name"] != "Rizky Pratama" {
		t.Errorf("expected Rizky Pratama, got %v", userTek["full_name"])
	}
}

func TestAuthorizationCrossAccountAndTechnicianPond(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)

	// Register Pemilik A
	statusA, resA := doJSON(t, "POST", srv.URL+"/api/v1/auth/register", "", map[string]any{
		"full_name": "Pemilik A",
		"email":     "pemilik-a@tambak.test",
		"password":  "password123456",
	})
	if statusA != http.StatusOK {
		t.Fatalf("register A failed: %d, %v", statusA, resA)
	}
	tokenA := resA["data"].(map[string]any)["access_token"].(string)

	// Pemilik A creates Farm A
	statusFarm, resFarm := doJSON(t, "POST", srv.URL+"/api/v1/farms", tokenA, map[string]any{
		"name":    "Tambak A",
		"regency": "Banyuwangi",
	})
	if statusFarm != http.StatusOK {
		t.Fatalf("create farm A failed: %d, %v", statusFarm, resFarm)
	}
	farmA := resFarm["data"].(map[string]any)
	farmAID := farmA["id"].(string)

	// Register Pemilik B
	statusB, resB := doJSON(t, "POST", srv.URL+"/api/v1/auth/register", "", map[string]any{
		"full_name": "Pemilik B",
		"email":     "pemilik-b@tambak.test",
		"password":  "password123456",
	})
	if statusB != http.StatusOK {
		t.Fatalf("register B failed: %d, %v", statusB, resB)
	}
	tokenB := resB["data"].(map[string]any)["access_token"].(string)

	// Pemilik B tries to access Farm A -> MUST be 403 FORBIDDEN (not 404)
	statusCross, resCross := doJSON(t, "GET", srv.URL+"/api/v1/farms/"+farmAID+"/ponds", tokenB, nil)
	if statusCross != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for cross-owner access, got %d, body: %v", statusCross, resCross)
	}

	// Without token -> MUST be 401 UNAUTHORIZED
	statusNoAuth, _ := doJSON(t, "GET", srv.URL+"/api/v1/farms/"+farmAID+"/ponds", "", nil)
	if statusNoAuth != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without token, got %d", statusNoAuth)
	}
}

func TestSampleIngestionBatchAndResultContract(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)

	// Login demo
	_, resDemo := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "pemilik"})
	token := resDemo["data"].(map[string]any)["access_token"].(string)

	// Get farms
	_, resFarms := doJSON(t, "GET", srv.URL+"/api/v1/farms", token, nil)
	farms := resFarms["data"].(map[string]any)["items"].([]any)
	farmID := farms[0].(map[string]any)["id"].(string)

	// Get ponds for farm
	_, resPonds := doJSON(t, "GET", srv.URL+"/api/v1/farms/"+farmID+"/ponds", token, nil)
	ponds := resPonds["data"].(map[string]any)["items"].([]any)
	pondID := ponds[0].(map[string]any)["id"].(string)

	// POST /samples
	samplePayload := map[string]any{
		"pond_id":       pondID,
		"sample_code":   "B1-1024",
		"sampling_date": "2026-10-24",
		"cycle_day":     35,
		"features": map[string]any{
			"ct_ahpnd":     22.0,
			"ct_wssv":      40.0,
			"ct_ehp":       30.0,
			"ct_amoa":      28.22,
			"ct_16s":       18.44,
			"ct_ppc":       19.95,
			"do_mg_l":      2.65,
			"tom_mg_l":     128.4,
			"ph":           8.4,
			"salinity_ppt": 22.5,
			"temp_c":       30.0,
			"tan_mg_l":     3.2,
			"no2_mg_l":     4.85,
			"h2s_mg_l":     0.092,
			"alkalinity":   82.0,
		},
		"meta": map[string]any{
			"wells": []any{
				map[string]any{"position": "C7", "target": "pirA", "ct": 22.0, "tm": 81.2, "excluded_reason": nil},
			},
			"standards": []any{
				map[string]any{"copies_rxn": 100000, "ct": 19.8},
			},
		},
	}

	statusSample, resSample := doJSON(t, "POST", srv.URL+"/api/v1/samples", token, samplePayload)
	if statusSample != http.StatusCreated && statusSample != http.StatusOK {
		t.Fatalf("create sample failed: %d, body: %v", statusSample, resSample)
	}
	smpData := resSample["data"].(map[string]any)
	sdriRes, ok := smpData["sdri_result"].(map[string]any)
	if !ok {
		t.Fatalf("missing sdri_result in sample creation: %v", smpData)
	}

	// Verify required contract & disease names
	if sdriRes["risk_class"].(float64) != 4 {
		t.Errorf("expected Class 4 Bahaya for high tox, got %v", sdriRes["risk_class"])
	}
	if sdriRes["disease_name"] == nil || sdriRes["disease_name"] == "" {
		t.Errorf("expected non-empty disease_name, got %v", sdriRes["disease_name"])
	}
	if sdriRes["mitigation_actions"] == nil {
		t.Errorf("expected non-empty mitigation_actions, got %v", sdriRes["mitigation_actions"])
	}

	// Fetch sample detail
	sampleID := smpData["sample_id"].(string)
	statusDetail, resDetail := doJSON(t, "GET", srv.URL+"/api/v1/samples/"+sampleID, token, nil)
	if statusDetail != http.StatusOK {
		t.Fatalf("get sample detail failed: %d, body: %v", statusDetail, resDetail)
	}
	detailData := resDetail["data"].(map[string]any)
	if detailData["nh3_mg_l"] == nil {
		t.Errorf("missing nh3_mg_l in sample detail")
	}
	if detailData["copies_per_ml"] == nil {
		t.Errorf("missing copies_per_ml in sample detail")
	}
}

func TestWaterLogValidationAndEmerson(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)

	_, resDemo := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "pemilik"})
	token := resDemo["data"].(map[string]any)["access_token"].(string)

	_, resFarms := doJSON(t, "GET", srv.URL+"/api/v1/farms", token, nil)
	farms := resFarms["data"].(map[string]any)["items"].([]any)
	farmID := farms[0].(map[string]any)["id"].(string)

	_, resPonds := doJSON(t, "GET", srv.URL+"/api/v1/farms/"+farmID+"/ponds", token, nil)
	ponds := resPonds["data"].(map[string]any)["items"].([]any)
	pondID := ponds[0].(map[string]any)["id"].(string)

	// 1. Invalid range (temp 50 C > 40 C) -> VALIDATION_ERROR with fields
	statusInvalid, resInvalid := doJSON(t, "POST", srv.URL+"/api/v1/ponds/"+pondID+"/water-logs", token, map[string]any{
		"measured_at": time.Now().Format(time.RFC3339),
		"temp_c":      55.0, // Invalid!
		"ph":          8.0,
		"do_mg_l":     5.0,
		"tan_mg_l":    1.0,
	})
	if statusInvalid != http.StatusBadRequest {
		t.Errorf("expected 400 for out-of-range water log, got %d, body: %v", statusInvalid, resInvalid)
	}
	errObj, _ := resInvalid["error"].(map[string]any)
	if errObj["code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR code, got %v", errObj["code"])
	}

	// 2. Valid water log -> calculates Emerson nh3_mg_l
	statusValid, resValid := doJSON(t, "POST", srv.URL+"/api/v1/ponds/"+pondID+"/water-logs", token, map[string]any{
		"measured_at": time.Now().Format(time.RFC3339),
		"temp_c":      30.0,
		"ph":          7.8,
		"do_mg_l":     6.4,
		"tan_mg_l":    0.25,
	})
	if statusValid != http.StatusOK && statusValid != http.StatusCreated {
		t.Fatalf("expected 200/201, got %d, body: %v", statusValid, resValid)
	}
	logData := resValid["data"].(map[string]any)
	nh3, ok := logData["nh3_mg_l"].(float64)
	if !ok || nh3 <= 0 || nh3 > 0.1 {
		t.Errorf("expected Emerson nh3 ~0.0119 mg/L, got %v", nh3)
	}
}

func TestTaskReportAndPhotoUpload(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)

	// Pemilik creates task
	_, resDemo := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "pemilik"})
	ownerToken := resDemo["data"].(map[string]any)["access_token"].(string)

	_, resFarms := doJSON(t, "GET", srv.URL+"/api/v1/farms", ownerToken, nil)
	farms := resFarms["data"].(map[string]any)["items"].([]any)
	farmID := farms[0].(map[string]any)["id"].(string)

	_, resPonds := doJSON(t, "GET", srv.URL+"/api/v1/farms/"+farmID+"/ponds", ownerToken, nil)
	ponds := resPonds["data"].(map[string]any)["items"].([]any)
	pondID := ponds[0].(map[string]any)["id"].(string)

	// Teknisi demo
	_, resTek := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "teknisi"})
	tekToken := resTek["data"].(map[string]any)["access_token"].(string)
	tekID := resTek["data"].(map[string]any)["user"].(map[string]any)["id"].(string)

	// Create task
	statusTask, resTask := doJSON(t, "POST", srv.URL+"/api/v1/tasks", ownerToken, map[string]any{
		"farm_id":     farmID,
		"pond_id":     pondID,
		"assignee_id": tekID,
		"kind":        "tindakan",
		"title":       "Siphon Dasar Kolam",
		"instruction": "Siphon endapan pakan di central drain.",
		"due_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	if statusTask != http.StatusOK && statusTask != http.StatusCreated {
		t.Fatalf("create task failed: %d, body: %v", statusTask, resTask)
	}
	taskID := resTask["data"].(map[string]any)["id"].(string)

	// Teknisi reports task with multipart upload
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("note", "Penyiponan dasar selesai dikerjakan.")
	_ = w.WriteField("measured", `{"do_mg_l": 5.8}`)
	part, _ := w.CreateFormFile("photos", "kondisi_anco.jpg")
	var photo bytes.Buffer
	_ = png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_, _ = part.Write(photo.Bytes())
	_ = w.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/tasks/"+taskID+"/reports", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tekToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("multipart report failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 200/201 on report, got %d", resp.StatusCode)
	}

	var resReport map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&resReport)
	reportData := resReport["data"].(map[string]any)
	photos, ok := reportData["photos"].([]any)
	if !ok || len(photos) == 0 {
		t.Fatalf("missing photos in report response: %v", reportData)
	}

	photoObj := photos[0].(map[string]any)
	fileID := photoObj["id"].(string)

	// Fetch file with owner or technician token
	reqFile, _ := http.NewRequest("GET", srv.URL+"/api/v1/files/"+fileID, nil)
	reqFile.Header.Set("Authorization", "Bearer "+ownerToken)
	respFile, err := http.DefaultClient.Do(reqFile)
	if err != nil {
		t.Fatalf("get file failed: %v", err)
	}
	defer respFile.Body.Close()

	if respFile.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on file fetch, got %d", respFile.StatusCode)
	}
	fileBytes, _ := io.ReadAll(respFile.Body)
	if !bytes.Equal(fileBytes, photo.Bytes()) {
		t.Errorf("file content mismatch")
	}
}

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/repository"
)

func qaUUID() string {
	return fmt.Sprintf("%08x-%04x-4000-8000-%012x", uint32(time.Now().UnixNano()), uint16(time.Now().UnixNano()>>32), uint64(time.Now().UnixNano())&0xffffffffffff)
}

func setupQAHandler(t *testing.T, store repository.WebStore) (*httptest.Server, *auth.Auth, string) {
	a, err := auth.New("qa-jwt-secret-minimum-16-bytes-ok")
	if err != nil {
		t.Fatal(err)
	}
	filesDir := t.TempDir()
	lh := NewLiveHandler(store, Config{
		Auth:        a,
		DemoEnabled: true,
		FilesDir:    filesDir,
	})
	srv := httptest.NewServer(lh.Routes())
	t.Cleanup(srv.Close)
	return srv, a, filesDir
}

func qaPond(t *testing.T, store repository.WebStore, a *auth.Auth) (string, string, string, string, string) {
	t.Helper()
	owner, tech, farm, pond := qaUUID(), qaUUID(), qaUUID(), qaUUID()
	err := store.Write(context.Background(), func(tx repository.WebTx) error {
		for _, entry := range []struct {
			table string
			row   rec
		}{
			{"users", rec{"id": owner, "email": owner + "@example.test", "full_name": "QA owner", "password_hash": "test"}},
			{"users", rec{"id": tech, "email": tech + "@example.test", "full_name": "QA technician", "password_hash": "test"}},
			{"farms", rec{"id": farm, "owner_id": owner, "name": "QA farm", "regency": "QA"}},
			{"memberships", rec{"id": qaUUID(), "farm_id": farm, "user_id": tech, "role": "teknisi", "active": true}},
			{"ponds", rec{"id": pond, "farm_id": farm, "pond_code": "QA", "assignee_id": tech, "current_cycle": 4}},
		} {
			if e := tx.Put(entry.table, entry.row); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerToken, err := a.Access(owner)
	if err != nil {
		t.Fatal(err)
	}
	techToken, err := a.Access(tech)
	if err != nil {
		t.Fatal(err)
	}
	return ownerToken, techToken, tech, farm, pond
}

func TestPaginationOverflowGuard(t *testing.T) {
	rows := []rec{
		{"id": "1", "created_at": "2026-10-24T10:00:00Z"},
		{"id": "2", "created_at": "2026-10-24T10:01:00Z"},
	}
	// Normal first page
	req1, _ := http.NewRequest("GET", "/test?page=1&per_page=10", nil)
	res1 := page(req1, rows)
	if len(res1["items"].([]rec)) != 2 {
		t.Fatalf("expected 2 items on page 1, got %d", len(res1["items"].([]rec)))
	}

	// Page 2 beyond total: must be empty, NOT clamped to last page!
	req2, _ := http.NewRequest("GET", "/test?page=2&per_page=10", nil)
	res2 := page(req2, rows)
	if len(res2["items"].([]rec)) != 0 {
		t.Fatalf("expected 0 items on page 2 beyond max, got %d", len(res2["items"].([]rec)))
	}

	// Huge page overflow: must be empty, must NOT overflow or panic
	for _, pageStr := range []string{"9223372036854775807", "9999999999999999999999999999"} {
		req, _ := http.NewRequest("GET", "/test?page="+pageStr+"&per_page=10", nil)
		res := page(req, rows)
		items := res["items"].([]rec)
		if len(items) != 0 {
			t.Fatalf("expected 0 items for page=%s, got %d", pageStr, len(items))
		}
	}

	// Negative, zero, or invalid page falls back to page 1
	for _, pageStr := range []string{"-1", "-999999999999999999", "0", "abc"} {
		req, _ := http.NewRequest("GET", "/test?page="+pageStr+"&per_page=10", nil)
		res := page(req, rows)
		items := res["items"].([]rec)
		if len(items) != 2 {
			t.Fatalf("expected 2 items on fallback to page 1 for page=%s, got %d", pageStr, len(items))
		}
	}
}

type qaReadOnlyStore struct{ repository.WebStore }

func (qaReadOnlyStore) Write(context.Context, func(repository.WebTx) error) error {
	return fmt.Errorf("GET used write transaction")
}

func TestNotificationsReadPath(t *testing.T) {
	srv, a, _ := setupQAHandler(t, qaReadOnlyStore{repository.NewMemoryStore()})
	token, err := a.Access(qaUUID())
	if err != nil {
		t.Fatal(err)
	}

	code, res := doJSON(t, "GET", srv.URL+"/api/v1/me/notifications", token, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d: %v", code, res)
	}
	data := object(res["data"])
	if data["items"] == nil {
		t.Fatalf("expected items array, got %v", data)
	}
}

func TestRefreshReplayRevokesOnlyFamily(t *testing.T) {
	for name, store := range coreStores(t) {
		t.Run(name, func(t *testing.T) {
			srv, _, _ := setupQAHandler(t, store)
			credentials := rec{"full_name": "QA replay", "email": qaUUID() + "@example.test", "password": "password123456"}
			code, out := doJSON(t, "POST", srv.URL+"/api/v1/auth/register", "", credentials)
			if code != 200 {
				t.Fatal("register", code)
			}
			original := str(object(out["data"]), "refresh_token")
			code, out = doJSON(t, "POST", srv.URL+"/api/v1/auth/login", "", credentials)
			if code != 200 {
				t.Fatal("login", code)
			}
			unrelated := str(object(out["data"]), "refresh_token")
			code, out = doJSON(t, "POST", srv.URL+"/api/v1/auth/refresh", "", rec{"refresh_token": original})
			if code != 200 {
				t.Fatal("rotation", code)
			}
			child := str(object(out["data"]), "refresh_token")
			for _, token := range []string{original, child} {
				code, _ = doJSON(t, "POST", srv.URL+"/api/v1/auth/refresh", "", rec{"refresh_token": token})
				if code != 401 {
					t.Fatal("replayed family survived", code)
				}
			}
			code, _ = doJSON(t, "POST", srv.URL+"/api/v1/auth/refresh", "", rec{"refresh_token": unrelated})
			if code != 200 {
				t.Fatal("unrelated session revoked", code)
			}
		})
	}
}

func TestConcurrentRefreshOnlyOnce(t *testing.T) {
	for name, store := range coreStores(t) {
		t.Run(name, func(t *testing.T) {
			srv, _, _ := setupQAHandler(t, store)

			// Register a user
			email := "refresh-" + qaUUID() + "@example.test"
			code, out := doJSON(t, "POST", srv.URL+"/api/v1/auth/register", "", rec{
				"full_name": "QA Refresh",
				"email":     email,
				"password":  "password123456",
			})
			if code != 200 {
				t.Fatalf("register failed: %d %v", code, out)
			}
			data := object(out["data"])
			rt := str(data, "refresh_token")

			// Launch 2 concurrent refresh requests
			var wg sync.WaitGroup
			codes := make([]int, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					c, _ := doJSON(t, "POST", srv.URL+"/api/v1/auth/refresh", "", rec{"refresh_token": rt})
					codes[idx] = c
				}(i)
			}
			wg.Wait()

			successCount := 0
			failCount := 0
			for _, c := range codes {
				if c == 200 {
					successCount++
				} else if c == 401 {
					failCount++
				}
			}
			if successCount != 1 || failCount != 1 {
				t.Fatalf("expected exactly 1 success (200) and 1 failure (401), got %v", codes)
			}
		})
	}
}

func TestFileConfinementAndPGPhotoUpload(t *testing.T) {
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST tidak diatur")
	}
	store, err := repository.OpenPostgres(context.Background(), url, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	srv, a, filesDir := setupQAHandler(t, store)
	ownerToken, tekToken, tekID, farmID, pondID := qaPond(t, store, a)

	// Create task
	codeTask, resTask := doJSON(t, "POST", srv.URL+"/api/v1/tasks", ownerToken, map[string]any{
		"farm_id":     farmID,
		"pond_id":     pondID,
		"assignee_id": tekID,
		"kind":        "tindakan",
		"title":       "Siphon Kolam QA",
		"instruction": "Siphon endapan pakan",
		"due_at":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	if codeTask != 200 {
		t.Fatalf("create task failed: %d %v", codeTask, resTask)
	}
	taskID := str(object(resTask["data"]), "id")

	// Upload photo report
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("note", "Laporan selesai dengan foto")
	part, _ := w.CreateFormFile("photos", "uji_kolam.png")
	var photo bytes.Buffer
	_ = png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_, _ = part.Write(photo.Bytes())
	_ = w.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/tasks/"+taskID+"/reports", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tekToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var resReport map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&resReport)
	photos := object(resReport["data"])["photos"].([]any)
	fileID := str(object(photos[0]), "id")

	// Verify file in files table has pond_id matching pondID in Postgres!
	err = store.Read(context.Background(), func(tx repository.WebTx) error {
		f, e := tx.Get("files", fileID)
		if e != nil {
			return e
		}
		if str(f, "pond_id") != pondID {
			return fmt.Errorf("expected pond_id %s, got %v", pondID, f["pond_id"])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("file record check in PG failed: %v", err)
	}

	// Verify path confinement: creating an escape file record outside filesDir must be rejected
	outsideDir := t.TempDir()
	outsideFile := outsideDir + "/secret.txt"
	_ = os.WriteFile(outsideFile, []byte("secret"), 0600)
	evilFileID := qaUUID()
	if err = store.Write(context.Background(), func(tx repository.WebTx) error {
		return tx.Put("files", rec{
			"id":          evilFileID,
			"farm_id":     farmID,
			"pond_id":     pondID,
			"uploader_id": tekID,
			"file_name":   "secret.txt",
			"file_path":   outsideFile,
			"file_size":   6,
			"mime_type":   "text/plain",
			"created_at":  now(),
		})
	}); err != nil {
		t.Fatal(err)
	}

	reqEvil, _ := http.NewRequest("GET", srv.URL+"/api/v1/files/"+evilFileID, nil)
	reqEvil.Header.Set("Authorization", "Bearer "+ownerToken)
	respEvil, err := http.DefaultClient.Do(reqEvil)
	if err != nil {
		t.Fatal(err)
	}
	defer respEvil.Body.Close()
	if respEvil.StatusCode != http.StatusForbidden && respEvil.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 403 or 404 for path outside FilesDir, got %d", respEvil.StatusCode)
	}
	_ = filesDir
}

func TestPostgresBatchFailureRollsBack(t *testing.T) {
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST not set")
	}
	store, err := repository.OpenPostgres(context.Background(), url, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	srv, a, _ := setupQAHandler(t, store)
	owner, _, _, farm, pond := qaPond(t, store, a)
	good := rec{"pond_id": pond, "sample_code": "QA", "sampling_date": "2026-10-24", "cycle_day": 35, "features": rec{"ct_ppc": 19.95}}
	bad := clone(good)
	bad["sample_code"] = strings.Repeat("X", 101)
	code, _ := doJSON(t, "POST", srv.URL+"/api/v1/samples/batch", owner, rec{"run": rec{"lab_name": "QA", "instrument": "QA", "tested_at": now(), "file_name": "qa.csv"}, "samples": []rec{good, bad}})
	if code == 200 {
		t.Fatal("batch storage error returned success")
	}
	if err = store.Read(context.Background(), func(tx repository.WebTx) error {
		for _, q := range []struct{ table, key, value string }{{"samples", "pond_id", pond}, {"batches", "farm_id", farm}} {
			rows, e := tx.FindEqual(q.table, q.key, q.value)
			if e != nil {
				return e
			}
			if len(rows) != 0 {
				t.Fatal("batch partially committed")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSampleCycleLinkageServerControlled(t *testing.T) {
	for name, store := range coreStores(t) {
		t.Run(name, func(t *testing.T) {
			srv, a, _ := setupQAHandler(t, store)
			token, _, _, _, pondID := qaPond(t, store, a)
			initialCycle := 4

			feats := rec{"ct_ahpnd": 22, "ct_wssv": 40, "ct_ehp": 30, "ct_amoa": 28.22, "ct_16s": 18.44, "ct_ppc": 19.95, "do_mg_l": 2.65, "tom_mg_l": 128.4, "ph": 8.4, "salinity_ppt": 22.5, "temp_c": 30, "tan_mg_l": 3.2, "no2_mg_l": 4.85, "h2s_mg_l": .092, "alkalinity": 82}

			// Client attempts to spoof cycle_number = 999
			spoofedMeta := rec{
				"cycle_number": 999,
				"wells":        []any{rec{"position": "A1", "target": "pirA"}},
				"standards":    []any{rec{"copies": 1000}},
				"water_log_id": "test-wlog-id",
			}

			codeSample, resSample := doJSON(t, "POST", srv.URL+"/api/v1/samples", token, rec{
				"pond_id":       pondID,
				"sample_code":   "SMP-SPOOF-TEST",
				"sampling_date": "2026-10-24",
				"cycle_day":     35,
				"features":      feats,
				"meta":          spoofedMeta,
			})
			if codeSample != 200 {
				t.Fatalf("create sample failed: %d %v", codeSample, resSample)
			}
			smpID := str(object(resSample["data"]), "sample_id")

			// Fetch sample detail
			codeDetail, resDetail := doJSON(t, "GET", srv.URL+"/api/v1/samples/"+smpID, token, nil)
			if codeDetail != 200 {
				t.Fatalf("get sample detail failed: %d %v", codeDetail, resDetail)
			}
			metaMap := object(object(resDetail["data"])["meta"])

			// Spoofed cycle_number 999 must be OVERRIDDEN with authorized pond's cycle
			if int(num(metaMap, "cycle_number")) != initialCycle {
				t.Fatalf("expected cycle_number %d (server controlled), got %v (spoof succeeded!)", initialCycle, metaMap["cycle_number"])
			}
			// Wells, standards, and water_log_id must be preserved
			if metaMap["wells"] == nil || len(metaMap["wells"].([]any)) != 1 {
				t.Fatalf("wells metadata lost: %v", metaMap)
			}
			if len(metaMap["standards"].([]any)) != 1 {
				t.Fatal("standards metadata lost")
			}
			if str(metaMap, "water_log_id") != "test-wlog-id" {
				t.Fatalf("water_log_id lost: %v", metaMap)
			}

			// Batch path spoof test
			batchCode, batchRes := doJSON(t, "POST", srv.URL+"/api/v1/samples/batch", token, rec{
				"run": rec{"lab_name": "Test Lab", "instrument": "Tester", "tested_at": time.Now().Format(time.RFC3339), "file_name": "test.csv"},
				"samples": []rec{{
					"pond_id":       pondID,
					"sample_code":   "SMP-BATCH-SPOOF",
					"sampling_date": "2026-10-24",
					"cycle_day":     35,
					"features":      feats,
					"meta":          spoofedMeta,
				}},
			})
			if batchCode != 200 {
				t.Fatalf("batch create failed: %d %v", batchCode, batchRes)
			}
			batchResults := object(batchRes["data"])["results"].([]any)
			batchSmpID := str(object(batchResults[0]), "sample_id")

			_, batchDetail := doJSON(t, "GET", srv.URL+"/api/v1/samples/"+batchSmpID, token, nil)
			batchMeta := object(object(batchDetail["data"])["meta"])
			if int(num(batchMeta, "cycle_number")) != initialCycle {
				t.Fatalf("batch expected cycle_number %d, got %v", initialCycle, batchMeta["cycle_number"])
			}

			// Advance pond to next cycle
			if err := store.Write(context.Background(), func(tx repository.WebTx) error {
				p, e := tx.Get("ponds", pondID)
				if e != nil {
					return e
				}
				p["current_cycle"] = initialCycle + 1
				return tx.Put("ponds", p)
			}); err != nil {
				t.Fatal(err)
			}

			// Persisted cycle_number of previously saved samples must remain IMMUTABLE
			_, resDetailAfter := doJSON(t, "GET", srv.URL+"/api/v1/samples/"+smpID, token, nil)
			metaAfter := object(object(resDetailAfter["data"])["meta"])
			if int(num(metaAfter, "cycle_number")) != initialCycle {
				t.Fatalf("persisted sample cycle_number mutated! expected %d, got %v", initialCycle, metaAfter["cycle_number"])
			}
			_, batchAfter := doJSON(t, "GET", srv.URL+"/api/v1/samples/"+batchSmpID, token, nil)
			if num(object(object(batchAfter["data"])["meta"]), "cycle_number") != float64(initialCycle) {
				t.Fatal("batch sample cycle mutated")
			}
		})
	}
}

func coreStores(t *testing.T) map[string]repository.WebStore {
	stores := map[string]repository.WebStore{"memory": repository.NewMemoryStore()}
	if url := os.Getenv("DATABASE_URL_TEST"); url != "" {
		s, err := repository.OpenPostgres(context.Background(), url, 6)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		stores["postgres"] = s
	}
	return stores
}

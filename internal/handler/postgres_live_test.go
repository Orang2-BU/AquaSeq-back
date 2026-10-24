package handler

import (
	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/repository"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestLivePostgresBatch(t *testing.T) {
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST tidak diatur")
	}
	store, e := repository.OpenPostgres(context.Background(), url, 4)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	a, e := auth.New("test-secret-for-integration-123456789")
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(NewLiveHandler(store, Config{Auth: a, DemoEnabled: true, FilesDir: t.TempDir()}).Routes())
	defer srv.Close()
	code, out := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", rec{"role": "pemilik"})
	if code != 200 {
		t.Fatal(code, out)
	}
	token := str(object(out["data"]), "access_token")
	code, out = doJSON(t, "GET", srv.URL+"/api/v1/farms", token, nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	items := object(out["data"])["items"].([]any)
	if len(items) != 2 {
		t.Fatal(out)
	}
	f := object(items[0])
	fid := str(f, "id")
	code, out = doJSON(t, "GET", srv.URL+"/api/v1/farms/"+fid+"/ponds", token, nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	ps := object(out["data"])["items"].([]any)
	pid := str(object(ps[0]), "id")
	feats := rec{"ct_ahpnd": 22, "ct_wssv": 40, "ct_ehp": 30, "ct_amoa": 28.22, "ct_16s": 18.44, "ct_ppc": 19.95, "do_mg_l": 2.65, "tom_mg_l": 128.4, "ph": 8.4, "salinity_ppt": 22.5, "temp_c": 30, "tan_mg_l": 3.2, "no2_mg_l": 4.85, "h2s_mg_l": .092, "alkalinity": 82}
	code, out = doJSON(t, "POST", srv.URL+"/api/v1/samples/batch", token, rec{"run": rec{"lab_name": "Data simulasi", "instrument": "Panel jejak penyakit", "tested_at": time.Now().Format(time.RFC3339), "file_name": "simulasi.csv"}, "samples": []rec{{"pond_id": pid, "sample_code": "SIMULASI-UJI", "sampling_date": "2026-10-24", "cycle_day": 35, "source_type": "GSI_LAB", "features": feats, "meta": rec{"wells": []any{}, "standards": []any{}, "water_log_id": nil}}}})
	if code != 200 {
		t.Fatal(code, out)
	}
	result := object(out["data"])["results"].([]any)
	sid := str(object(result[0]), "sample_id")
	code, out = doJSON(t, "GET", srv.URL+"/api/v1/samples/"+sid, token, nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	b, _ := json.Marshal(out)
	t.Log(string(b))
	d := object(out["data"])
	res := object(d["sdri_result"])
	if str(res, "disease_name") == "" || res["mitigation_actions"] == nil || d["nh3_mg_l"] == nil {
		t.Fatal(out)
	}
}

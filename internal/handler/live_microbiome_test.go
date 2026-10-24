package handler

import (
	"net/http"
	"testing"
)

func createTestSample(t *testing.T, base, token, pondID, code string, day int) string {
	t.Helper()
	status, res := doJSON(t, "POST", base+"/api/v1/samples", token, map[string]any{
		"pond_id":       pondID,
		"sample_code":   code,
		"sampling_date": "2026-10-24",
		"cycle_day":     day,
		"features": map[string]any{
			"ct_ahpnd": 30.0, "ct_wssv": 40.0, "ct_ehp": 40.0, "ct_amoa": 26.0, "ct_16s": 18.4, "ct_ppc": 19.9,
			"do_mg_l": 5.6, "tom_mg_l": 60.0, "ph": 7.9, "salinity_ppt": 20.0, "temp_c": 29.0, "tan_mg_l": 0.4,
			"no2_mg_l": 0.2, "h2s_mg_l": 0.002, "alkalinity": 140.0,
		},
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("buat sampel gagal: %d %v", status, res)
	}
	return res["data"].(map[string]any)["sample_id"].(string)
}

func TestMicrobiomeUploadBaselineAndPanel(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)
	_, demo := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "pemilik"})
	token := demo["data"].(map[string]any)["access_token"].(string)
	_, farms := doJSON(t, "GET", srv.URL+"/api/v1/farms", token, nil)
	farmID := farms["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"].(string)
	_, ponds := doJSON(t, "GET", srv.URL+"/api/v1/farms/"+farmID+"/ponds", token, nil)
	pondID := ponds["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"].(string)

	first := createTestSample(t, srv.URL, token, pondID, "MB-0101", 101)
	second := createTestSample(t, srv.URL, token, pondID, "MB-0115", 115)

	status, res := doJSON(t, "POST", srv.URL+"/api/v1/samples/"+first+"/microbiome", token, map[string]any{
		"csv": "taxon,genus,reads\nBacillus subtilis,Bacillus,5000\nNitrosomonas europaea,Nitrosomonas,3000\nVibrio harveyi,Vibrio,2000\n",
	})
	if status != http.StatusOK {
		t.Fatalf("unggah baseline gagal: %d %v", status, res)
	}
	baseMetrics := res["data"].(map[string]any)["microbiome"].(map[string]any)
	if baseMetrics["bray_curtis"] != nil || baseMetrics["status"] != "VALID" {
		t.Fatalf("sampel pertama tidak punya baseline: %v", baseMetrics)
	}

	status, res = doJSON(t, "POST", srv.URL+"/api/v1/samples/"+second+"/microbiome", token, map[string]any{
		"csv": "taxon;reads\nBacillus subtilis;2000\nNitrosomonas europaea;3000\nVibrio harveyi;5000\n",
	})
	if status != http.StatusOK {
		t.Fatalf("unggah kedua gagal: %d %v", status, res)
	}
	m := res["data"].(map[string]any)["microbiome"].(map[string]any)
	if bc, _ := m["bray_curtis"].(float64); bc < 0.299 || bc > 0.301 {
		t.Errorf("Bray-Curtis terhadap baseline = %v, ingin 0,3", m["bray_curtis"])
	}
	if m["baseline_sample_id"] != first || m["vibrio_pct"].(float64) != 50 {
		t.Errorf("baseline/porsi Vibrio salah: %v", m)
	}

	status, res = doJSON(t, "GET", srv.URL+"/api/v1/ponds/"+pondID+"/microbiome", token, nil)
	if status != http.StatusOK {
		t.Fatalf("ringkasan kolam gagal: %d %v", status, res)
	}
	data := res["data"].(map[string]any)
	if series := data["series"].([]any); len(series) != 2 {
		t.Fatalf("seri harus 2 sampel, dapat %d", len(series))
	}
	markers := data["markers"].([]any)
	if len(markers) < 3 || len(markers) > 5 || markers[2].(map[string]any)["target"] != "pirA" {
		t.Errorf("panel penanda salah: %v", markers)
	}

	status, res = doJSON(t, "GET", srv.URL+"/api/v1/samples/"+second, token, nil)
	if status != http.StatusOK {
		t.Fatalf("detail sampel gagal: %d", status)
	}
	detail := res["data"].(map[string]any)
	if f, _ := detail["engineered_features"].([]any); len(f) != 12 {
		t.Errorf("engineered_features harus 12 fitur, dapat %v", detail["engineered_features"])
	}
	if detail["microbiome"] == nil {
		t.Error("detail sampel harus membawa microbiome")
	}
}

func TestMicrobiomeValidationAndAccess(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)
	_, demo := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "pemilik"})
	token := demo["data"].(map[string]any)["access_token"].(string)
	_, farms := doJSON(t, "GET", srv.URL+"/api/v1/farms", token, nil)
	farmID := farms["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"].(string)
	_, ponds := doJSON(t, "GET", srv.URL+"/api/v1/farms/"+farmID+"/ponds", token, nil)
	pondID := ponds["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"].(string)
	sid := createTestSample(t, srv.URL, token, pondID, "MB-0201", 120)

	if status, res := doJSON(t, "POST", srv.URL+"/api/v1/samples/"+sid+"/microbiome", token, map[string]any{"csv": "nama,jumlah\nA,1\n"}); status != http.StatusBadRequest {
		t.Errorf("CSV tanpa kolom wajib harus 400, dapat %d %v", status, res)
	}

	_, tech := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "teknisi"})
	techToken := tech["data"].(map[string]any)["access_token"].(string)
	if status, _ := doJSON(t, "POST", srv.URL+"/api/v1/samples/"+sid+"/microbiome", techToken, map[string]any{"csv": "taxon,reads\nA,10000\n"}); status != http.StatusForbidden {
		t.Errorf("teknisi tidak boleh mengunggah kelimpahan, dapat %d", status)
	}
}

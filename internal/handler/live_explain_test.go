package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/repository"
	"aquaseq-back/internal/science"
)

type stubWriter struct{ out string }

func (s stubWriter) Name() string  { return "claude" }
func (s stubWriter) Model() string { return "claude-opus-5-5" }
func (s stubWriter) Write(context.Context, string, string) (string, error) {
	return s.out, nil
}

func newExplainServer(t *testing.T, gen *science.Generator) *httptest.Server {
	t.Helper()
	jwtAuth, err := auth.New("test-jwt-secret-minimum-16-bytes-ok")
	if err != nil {
		t.Fatal(err)
	}
	lh := NewLiveHandler(repository.NewMemoryStore(), Config{Auth: jwtAuth, DemoEnabled: true, FilesDir: t.TempDir(), Explainer: gen})
	srv := httptest.NewServer(lh.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func demoSample(t *testing.T, base string) (token, sampleID string) {
	t.Helper()
	_, demo := doJSON(t, "POST", base+"/api/v1/auth/demo", "", map[string]string{"role": "pemilik"})
	token = demo["data"].(map[string]any)["access_token"].(string)
	_, farms := doJSON(t, "GET", base+"/api/v1/farms", token, nil)
	farmID := farms["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"].(string)
	_, ponds := doJSON(t, "GET", base+"/api/v1/farms/"+farmID+"/ponds", token, nil)
	pondID := ponds["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"].(string)
	return token, createTestSample(t, base, token, pondID, "EX-0130", 130)
}

func TestExplanationTemplateStoredAndReadable(t *testing.T) {
	srv := newExplainServer(t, nil)
	token, sid := demoSample(t, srv.URL)

	status, res := doJSON(t, "GET", srv.URL+"/api/v1/samples/"+sid+"/explanation", token, nil)
	if status != http.StatusOK || res["data"].(map[string]any)["explanation"] != nil {
		t.Fatalf("sebelum dibuat, penjelasan harus kosong: %d %v", status, res)
	}
	if res["data"].(map[string]any)["ai"].(map[string]any)["writer"] != "template" {
		t.Errorf("tanpa kunci AI, penulis harus template: %v", res["data"])
	}

	status, res = doJSON(t, "POST", srv.URL+"/api/v1/samples/"+sid+"/explanation", token, nil)
	if status != http.StatusOK {
		t.Fatalf("buat penjelasan gagal: %d %v", status, res)
	}
	exp := res["data"].(map[string]any)["explanation"].(map[string]any)
	if exp["writer"] != "template" || exp["summary"] == "" || len(exp["reasons"].([]any)) == 0 {
		t.Errorf("penjelasan template tidak lengkap: %v", exp)
	}

	_, res = doJSON(t, "GET", srv.URL+"/api/v1/samples/"+sid+"/explanation", token, nil)
	if res["data"].(map[string]any)["explanation"] == nil {
		t.Error("penjelasan yang dibuat harus tersimpan")
	}

	_, tech := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", map[string]string{"role": "teknisi"})
	techToken := tech["data"].(map[string]any)["access_token"].(string)
	if status, _ := doJSON(t, "POST", srv.URL+"/api/v1/samples/"+sid+"/explanation", techToken, nil); status != http.StatusForbidden {
		t.Errorf("teknisi tidak boleh membuat penjelasan, dapat %d", status)
	}
}

func TestExplanationRejectsFabricatedWriterOutput(t *testing.T) {
	// Jawaban berangka karangan harus ditolak pemeriksa dan diganti template, dengan alasan tercatat.
	fabricated := `{"summary":"Peluang wabah kolam ini 99,9%.","reasons":[{"text":"Jejak AHPND 777777 kopi/mL.","sources":["FAKTA"]}],"actions":[{"text":"Panen sekarang.","source":"FAKTA"}],"limitations":["Tidak ada."]}`
	srv := newExplainServer(t, science.New(stubWriter{out: fabricated}, nil))
	token, sid := demoSample(t, srv.URL)
	_, res := doJSON(t, "POST", srv.URL+"/api/v1/samples/"+sid+"/explanation", token, nil)
	exp := res["data"].(map[string]any)["explanation"].(map[string]any)
	if exp["writer"] != "template" || exp["fallback_reason"] == nil || exp["fallback_reason"] == "" {
		t.Errorf("jawaban karangan harus diganti template: %v", exp)
	}
	if res["data"].(map[string]any)["ai"].(map[string]any)["writer"] != "claude" {
		t.Errorf("status AI harus melaporkan penulis claude: %v", res["data"])
	}
}

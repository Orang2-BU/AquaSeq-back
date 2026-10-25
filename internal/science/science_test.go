package science

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aquaseq-back/internal/microbiome"
	"aquaseq-back/internal/sdri"
)

func ptr(v float64) *float64 { return &v }

func sampleFacts() Facts {
	vib := microbiome.Assess([]microbiome.Taxon{{Taxon: "Vibrio harveyi", Genus: "Vibrio", Reads: 4000}, {Taxon: "Bacillus subtilis", Genus: "Bacillus", Reads: 6000}}, nil, "")
	return Facts{
		PondCode: "C4", SampleCode: "C4-1023", CycleDay: 52, Status: "VALID", RiskClass: 4, RiskLevel: "BAHAYA",
		ProbabilityPct: 71.6, ModelVersion: "V1-GSI", PredictedDisease: "AHPND_NH3_RISK",
		DiseaseName: "Risiko jejak AHPND disertai amonia tinggi",
		Features: []sdri.Feature{
			{Code: "X3", Name: "jejak_ahpnd", Value: ptr(51200), Status: "kritis", Threshold: "peringatan >= 10; kritis > 1000"},
			{Code: "X7", Name: "nh3_mg_l", Value: ptr(0.118), Status: "kritis", Threshold: "aman <= 0,05; kritis > 0,1 mg/L"},
			{Code: "X10", Name: "do_mg_l", Value: ptr(5.6), Status: "aman", Threshold: "aman >= 5; kritis < 4 mg/L"},
		},
		Microbiome:        &vib,
		MitigationActions: []string{"Ganti air 20-30% dan nyalakan kincir tambahan.", "Hentikan pakan 1 hari, lalu beri setengah porsi."},
	}
}

func goodJSON() string {
	return `{"summary":"Kolam C4 masuk kelas risiko 4 dengan peluang wabah 71,6% menurut model V1-GSI.",
"reasons":[{"text":"Jejak AHPND 51.200 kopi/mL melewati ambang kritis 1000.","sources":["FAKTA","WANG2022"]},
{"text":"Amonia bebas 0,118 mg/L di atas batas kritis 0,1 mg/L.","sources":["FAKTA","EMERSON1975"]}],
"actions":[{"text":"Ganti air 20-30% dan nyalakan kincir tambahan.","source":"FAKTA"}],
"limitations":["Model belum divalidasi; porsi 16S bersifat relatif."]}`
}

type fakeWriter struct {
	out string
	err error
}

func (f fakeWriter) Name() string  { return "claude" }
func (f fakeWriter) Model() string { return "claude-opus-5-5" }
func (f fakeWriter) Write(context.Context, string, string) (string, error) {
	return f.out, f.err
}

type fakePicker struct {
	choice string
	conf   float64
}

func (p fakePicker) SelectActionLabel(context.Context, string, []string) (string, float64, error) {
	return p.choice, p.conf, nil
}

func TestExplainUsesWriterWhenCheckPasses(t *testing.T) {
	f := sampleFacts()
	e := New(fakeWriter{out: goodJSON()}, fakePicker{choice: f.MitigationActions[1], conf: 0.82}).Explain(context.Background(), f)
	if e.Writer != "claude" || e.FallbackReason != "" {
		t.Fatalf("seharusnya memakai penulis Claude, dapat %q (%s)", e.Writer, e.FallbackReason)
	}
	if e.Priority == nil || e.Priority.Action != f.MitigationActions[1] || e.Priority.NeedsReview {
		t.Errorf("prioritas Jev salah: %+v", e.Priority)
	}
	ids := map[string]bool{}
	for _, s := range e.Sources {
		ids[s.ID] = true
	}
	if !ids["WANG2022"] || !ids["EMERSON1975"] || ids["LU2023"] {
		t.Errorf("sumber yang dirujuk salah: %v", ids)
	}
}

func TestCheckRejectsFabrication(t *testing.T) {
	f := sampleFacts()
	g := New(nil, nil)
	cases := map[string]string{
		"angka karangan": strings.Replace(goodJSON(), "71,6%", "88,4%", 1),
		"sumber asing":   strings.Replace(goodJSON(), `"WANG2022"`, `"JURNAL-KARANGAN"`, 1),
		"kata mutlak":    strings.Replace(goodJSON(), "Model belum divalidasi", "Kolam pasti terserang", 1),
		"tanpa sumber":   strings.Replace(goodJSON(), `"sources":["FAKTA","EMERSON1975"]`, `"sources":[]`, 1),
	}
	for name, raw := range cases {
		e, err := parse(raw)
		if err != nil {
			t.Fatalf("%s: JSON uji rusak: %v", name, err)
		}
		if g.Check(e, f) == nil {
			t.Errorf("%s: pemeriksa seharusnya menolak", name)
		}
	}
	ok, _ := parse(goodJSON())
	if err := g.Check(ok, f); err != nil {
		t.Errorf("jawaban sah ditolak: %v", err)
	}
}

func TestExplainFallsBackToTemplate(t *testing.T) {
	f := sampleFacts()
	for name, w := range map[string]Writer{
		"tanpa penulis":  nil,
		"penulis galat":  fakeWriter{err: errors.New("status 400")},
		"bukan JSON":     fakeWriter{out: "Berikut penjelasannya: kolam aman."},
		"angka karangan": fakeWriter{out: strings.Replace(goodJSON(), "0,118", "0,9", 1)},
	} {
		e := New(w, nil).Explain(context.Background(), f)
		if e.Writer != "template" || e.FallbackReason == "" {
			t.Errorf("%s: seharusnya template dengan alasan, dapat %q %q", name, e.Writer, e.FallbackReason)
		}
		if err := New(nil, nil).Check(e, f); err != nil {
			t.Errorf("%s: template harus lolos pemeriksa: %v", name, err)
		}
	}
}

func TestTemplateRejectedSample(t *testing.T) {
	f := Facts{PondCode: "A4", SampleCode: "A4-1030", Status: "REJECTED_INHIBITOR", MitigationActions: []string{}}
	e := Template(f)
	if strings.Contains(e.Summary, "%") || New(nil, nil).Check(e, f) != nil {
		t.Errorf("template sampel ditolak salah: %+v", e)
	}
}

func TestPickerLowConfidenceAndOutsideList(t *testing.T) {
	f := sampleFacts()
	if p := New(nil, fakePicker{choice: f.MitigationActions[0], conf: 0.41}).Explain(context.Background(), f).Priority; p == nil || !p.NeedsReview {
		t.Errorf("keyakinan rendah harus ditandai perlu dicek: %+v", p)
	}
	if p := New(nil, fakePicker{choice: "Beri antibiotik", conf: 0.99}).Explain(context.Background(), f).Priority; p != nil {
		t.Errorf("pilihan di luar daftar mitigasi harus diabaikan: %+v", p)
	}
}

func TestClaudeWriterRequestShape(t *testing.T) {
	var body map[string]any
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id": "msg_uji", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
			"stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": goodJSON()}},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 10},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	f := sampleFacts()
	e := New(NewClaudeWriter("kunci-uji", "", srv.URL), nil).Explain(context.Background(), f)
	if e.Writer != "claude" || e.WriterModel != "claude-opus-5-5" {
		t.Fatalf("penulis Claude tidak terpakai: %q %q", e.Writer, e.FallbackReason)
	}
	if headers.Get("X-Api-Key") != "kunci-uji" || !strings.Contains(headers.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("header salah: key=%q beta=%q", headers.Get("X-Api-Key"), headers.Get("Anthropic-Beta"))
	}
	if body["model"] != "claude-opus-5-5" || body["fallbacks"] != "default" || body["temperature"] != nil {
		t.Errorf("body salah: model=%v fallbacks=%v temperature=%v", body["model"], body["fallbacks"], body["temperature"])
	}
	oc, _ := body["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if oc["effort"] != "low" || format["type"] != "json_schema" {
		t.Errorf("output_config salah: %v", oc)
	}
	system, _ := body["system"].([]any)
	if len(system) != 1 || system[0].(map[string]any)["cache_control"] == nil {
		t.Errorf("system prompt harus di-cache: %v", body["system"])
	}
}

// Penyangkalan dari mesin ("bukan bukti bebas penyakit") boleh dikutip, tapi klaim penulis sendiri tetap ditolak.
func TestCheckAllowsMachineNegation(t *testing.T) {
	f := sampleFacts()
	f.DiseaseName = "Belum ada indikasi target di atas ambang; bukan bukti bebas penyakit"
	g := New(nil, nil)
	quoted, _ := parse(strings.Replace(goodJSON(), "Kolam C4 masuk", "Belum ada indikasi target di atas ambang; bukan bukti bebas penyakit. Kolam C4 masuk", 1))
	if err := g.Check(quoted, f); err != nil {
		t.Errorf("kutipan penyangkalan mesin ditolak: %v", err)
	}
	claim, _ := parse(strings.Replace(goodJSON(), "Kolam C4 masuk", "Kolam ini bebas penyakit. Kolam C4 masuk", 1))
	if g.Check(claim, f) == nil {
		t.Error("klaim bebas penyakit dari penulis harus ditolak")
	}
}

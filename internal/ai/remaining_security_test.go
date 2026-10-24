package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExplainerRequiresExplicitEvidence(t *testing.T) {
	base := MachineFacts{PondID: "POND-1", Disease: "AHPND", RiskLevel: "AMAN", SDRIClass: 1, ProbabilityPct: 2, QCStatus: "VALID", DataAvailable: true}
	for _, change := range []func(*MachineFacts){func(f *MachineFacts) { f.QCStatus = "" }, func(f *MachineFacts) { f.DataAvailable = false }, func(f *MachineFacts) { f.Disease = "" }, func(f *MachineFacts) { f.RiskLevel = "give antibiotics" }, func(f *MachineFacts) { f.ProbabilityPct = math.NaN() }} {
		f := base
		change(&f)
		if _, e := BuildExplanation(f); e == nil {
			t.Fatalf("insufficient evidence accepted: %+v", f)
		}
	}
	report, e := BuildExplanation(base)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(report.EvidenceReason, "0,00 mg/L") || strings.Contains(report.SevenDayOutlook, "0,0%") {
		t.Fatalf("missing evidence fabricated: %+v", report)
	}
	for _, invention := range []string{"\nAda penyakit udang galaksi.", "\nBuang seluruh air tambak.", "\nKadar amonia 2 mg/L."} {
		if Verify(report.FullNarrative+invention, base) == nil {
			t.Fatalf("invented claim accepted: %q", invention)
		}
	}
	for class := 1; class <= 4; class++ {
		f := base
		f.SDRIClass = class
		r, e := BuildExplanation(f)
		if e != nil {
			t.Fatal(e)
		}
		for _, a := range r.Actions {
			if !strings.Contains(a.Source, "internal/ai/") {
				t.Fatalf("untraceable source: %+v", a)
			}
		}
	}
}

func TestIngestRequiresCompleteEvidence(t *testing.T) {
	for _, data := range []string{
		"sample_code,ct_ppc\nPOND-1,20\n",
		"sample_code,ct_ppc,qc_status\nPOND-1,20,FAIL\n",
		"sample_code,ct_ppc,ntc_status\nPOND-1,20,DETECTED\n",
		"sample_code,ct_ppc,outlier\nPOND-1,20,true\n",
		"sample_code,ct_ppc,ct_ahpnd\nPOND-1,20,-1\n",
	} {
		r, e := Ingest(context.Background(), "evidence.csv", []byte(data), nil)
		if e == nil && (!r.BlockPrediction || len(r.Samples) == 0 || !r.Samples[0].BlockPrediction) {
			t.Fatalf("insufficient evidence passed: %+v", r)
		}
	}
	for _, data := range []string{"sample_code,tom (g/L)\nPOND-1,20\n", "sample_code,temperature (F)\nPOND-1,80\n", "sample_code,unknown_feature\nPOND-1,2\n", "sample_code,ct_ppc\n<script>,20\n"} {
		if _, e := Ingest(context.Background(), "unsafe.csv", []byte(data), nil); e == nil {
			t.Fatalf("untrusted schema accepted: %q", data)
		}
	}
	if _, e := Ingest(context.Background(), "unsafe.bin", []byte("sample_code,ct_ppc\nPOND-1,20\n"), nil); e == nil {
		t.Fatal("unknown file guessed as CSV")
	}
}

func TestJevRequiresFullDistribution(t *testing.T) {
	for _, answer := range []string{
		`{"type":"choice","choice":"lab_report","confidence":0.9}`,
		`{"type":"choice","choice":"lab_report","confidence":0.9,"probabilities":{"lab_report":1}}`,
		`{"type":"choice","choice":"lab_report","confidence":0.9,"probabilities":{"lab_report":0.7,"demo_file":0.7,"worksheet":0,"other":0}}`,
		`{"type":"choice","choice":"lab_report","confidence":0.9,"probabilities":{"lab_report":0,"demo_file":1,"worksheet":0,"other":0}}`,
		`{"type":"choice","choice":"lab_report","confidence":0.9,"probabilities":{"lab_report":null,"demo_file":0,"worksheet":0,"other":0}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"model":"jev-latest","answers":{"doc_type":` + answer + `}}`))
		}))
		_, _, e := NewJevClient(server.URL, "test-key", "", server.Client()).ClassifyDocument(context.Background(), "text")
		server.Close()
		if e == nil {
			t.Fatalf("malformed answer accepted: %s", answer)
		}
	}
}

func TestJevTimeoutDoesNotMutateCaller(t *testing.T) {
	h := &http.Client{}
	_ = NewJevClient("", "test-key", "", h)
	if h.Timeout != 0 {
		t.Fatal("constructor mutated caller HTTP client")
	}
}

func TestOfficeRejectsAmbiguousTables(t *testing.T) {
	entries := map[string]string{"word/document.xml": `<document><tbl><tr><tc><t>sample_code</t></tc><tc><t>ct_ppc</t></tc></tr><tr><tc><t>POND-1</t></tc><tc><t>20</t></tc></tr></tbl><tbl><tr><tc><t>POND-2</t></tc><tc><t>21</t></tc></tr></tbl></document>`}
	if _, e := Ingest(context.Background(), "ambiguous.docx", zipped(t, entries), nil); e == nil {
		t.Fatal("unrelated DOCX tables merged")
	}
	for _, sheet := range []string{
		`<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>sample_code</t></is></c><c r="B1" t="inlineStr"><is><t>ct_ppc</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>POND-1</t></is></c><c r="B2"><f>10+10</f><v>20</v></c></row></sheetData></worksheet>`,
		`<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>sample_code</t></is></c><c r="B1" t="inlineStr"><is><t>ct_ppc</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>POND-1</t></is></c><c r="B3"><v>20</v></c></row></sheetData></worksheet>`,
	} {
		if _, e := Ingest(context.Background(), "ambiguous.xlsx", zipped(t, map[string]string{"xl/worksheets/sheet1.xml": sheet}), nil); e == nil {
			t.Fatal("ambiguous XLSX accepted")
		}
	}
}

func TestIngestPreservesZeroAndUnits(t *testing.T) {
	r, e := Ingest(context.Background(), "units.csv", []byte("sample_code,do (mg/L)\nPOND-1,0\n"), nil)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(r.Samples[0].Features)
	if !strings.Contains(string(b), `"do_mg_l":0`) || r.Samples[0].Provenance["do_mg_l"].Unit != "mg/L" {
		t.Fatalf("lost measurement: %s %+v", b, r)
	}
}

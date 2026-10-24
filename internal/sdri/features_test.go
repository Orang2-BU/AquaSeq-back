package sdri

import (
	"aquaseq-back/internal/domain"
	"math"
	"testing"
)

func number(v float64) *float64 { return &v }
func rawFeatures() domain.SampleFeatures {
	return domain.SampleFeatures{CTAHPND: number(22), CTWSSV: number(40), CTEHP: number(30), CTamoA: number(28.22), CT16S: number(18.44), CTPPC: 19.95, TempC: number(30), PH: number(8.4), TANMgL: number(3.2), NO2MgL: number(4.85), H2SMgL: number(.092), TOMMgL: number(128.4), DOMgL: number(2.65), Alkalinity: number(82), SalinityPPT: number(22.5)}
}
func TestAssessRawFeatures(t *testing.T) {
	a := Assess(rawFeatures())
	if a.Result.Status != domain.StatusValid || a.Result.PredictedDisease != "AHPND_NH3_RISK" {
		t.Fatalf("hasil: %+v", a)
	}
	if len(a.Features) != 12 || len(a.Result.Evidence) == 0 || len(a.Result.MitigationActions) == 0 {
		t.Fatal("fitur/bukti/tindakan hilang")
	}
	if math.Abs(*a.FoldRegulation+math.Pow(2, 1.98)) > 1e-6 {
		t.Fatal("konvensi QIAGEN salah")
	}
	if math.Abs(*a.NH4MgL+*a.NH3MgL-3.2) > 1e-9 {
		t.Fatal("neraca TAN salah")
	}
	if a.Features[9].Status != "kritis" || a.Features[11].Status != "kritis" {
		t.Fatal("ambang DO/alkalinitas salah")
	}
}
func TestAssessFailsClosed(t *testing.T) {
	f := rawFeatures()
	f.CTPPC = 24.46
	a := Assess(f)
	if a.Result.Status != domain.StatusRejectedInhibitor || a.Result.PredictedDisease != "" || a.CopiesPerML["ahpnd"] != nil {
		t.Fatal("kontrol gagal mengeluarkan prediksi")
	}
	if math.Abs(a.DeltaCTPPC-4.51) > 1e-9 {
		t.Fatal("delta kontrol salah")
	}
	f = rawFeatures()
	f.TANMgL = nil
	a = Assess(f)
	if a.Result.Status != domain.StatusRejectedInvalidQC || len(a.Missing) == 0 {
		t.Fatal("kimia kosong dianggap nol")
	}
	f = rawFeatures()
	f.CTWSSV = nil
	a = Assess(f)
	if a.Result.Status != domain.StatusRejectedInvalidQC {
		t.Fatal("target tidak diuji dianggap negatif")
	}
}
func TestAssessThresholds(t *testing.T) {
	for _, tc := range []struct {
		v      float64
		status string
	}{{.05, "aman"}, {.0501, "peringatan"}, {.1, "peringatan"}, {.1001, "kritis"}} {
		if got := highStatus(tc.v, .05, .1); got != tc.status {
			t.Errorf("%g got %s", tc.v, got)
		}
	}
}

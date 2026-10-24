package microbiome

import (
	"math"
	"strings"
	"testing"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, ingin %v", name, got, want)
	}
}

func TestParseCSV(t *testing.T) {
	taxa, err := ParseCSV(strings.NewReader(string(rune(0xFEFF)) + "taxon;genus;reads\nVibrio harveyi;Vibrio;100\nBacillus subtilis;;50\nVibrio harveyi;Vibrio;20\nNol;;0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(taxa) != 2 || taxa[0].Reads != 120 || taxa[1].Genus != "Bacillus" {
		t.Fatalf("hasil parse salah: %+v", taxa)
	}
	if _, err := ParseCSV(strings.NewReader("nama,jumlah\nA,1\n")); err == nil {
		t.Error("kolom wajib yang hilang harus ditolak")
	}
	if _, err := ParseCSV(strings.NewReader("taxon,reads\nA,12.5\n")); err == nil {
		t.Error("reads pecahan harus ditolak")
	}
}

func TestAssessShannonChao1Vibrio(t *testing.T) {
	// Empat takson sama banyak: H = ln 4 (contoh Buku Belajar Bab 10), evenness 1.
	even := []Taxon{{"A", "Vibrio", 2500}, {"B", "Bacillus", 2500}, {"C", "Nitrosomonas", 2500}, {"D", "Rhodobacter", 2500}}
	m := Assess(even, nil, "")
	near(t, "shannon", m.Shannon, math.Log(4), 0.001)
	near(t, "evenness", m.Evenness, 1, 0.001)
	near(t, "vibrio", m.VibrioPct, 25, 0.001)
	if m.Status != "VALID" || m.BrayCurtis != nil {
		t.Fatalf("status/baseline salah: %+v", m)
	}

	// Chao1 = S_obs + F1(F1-1)/(2(F2+1)): 4 takson, F1=2, F2=1 -> 4 + 2/4 = 4,5.
	rare := []Taxon{{"A", "A", 1}, {"B", "B", 1}, {"C", "C", 2}, {"D", "D", 9000}}
	near(t, "chao1", Assess(rare, nil, "").Chao1, 4.5, 0.01)
}

func TestAssessBaselineAndSignals(t *testing.T) {
	base := []Taxon{{"X", "Bacillus", 5000}, {"Y", "Nitrosomonas", 3000}, {"Z", "Vibrio", 2000}}
	now := []Taxon{{"X", "Bacillus", 2000}, {"Y", "Nitrosomonas", 3000}, {"Z", "Vibrio", 5000}}
	m := Assess(now, base, "s-base")
	// Porsi 50/30/20 vs 20/30/50: jumlah minimum 0,2+0,3+0,2 = 0,7 -> BC 0,3 (contoh Buku Belajar Bab 10).
	near(t, "bray-curtis", *m.BrayCurtis, 0.3, 0.001)
	if m.BaselineID != "s-base" || m.ShannonDrop == nil {
		t.Fatalf("baseline tidak tercatat: %+v", m)
	}
	status := map[string]string{}
	for _, s := range m.Signals {
		status[s.Code] = s.Status
	}
	if status["porsi_vibrio"] != "kritis" || status["pergeseran_komunitas"] != "aman" {
		t.Errorf("status sinyal salah: %v", status)
	}
}

func TestAssessShallowSample(t *testing.T) {
	m := Assess([]Taxon{{"A", "Vibrio", 300}, {"B", "Bacillus", 200}}, nil, "")
	if m.Status != "BELUM_CUKUP" || len(m.Signals) != 1 || m.Signals[0].Status != "belum_cukup" {
		t.Fatalf("sampel dangkal harus BELUM_CUKUP: %+v", m)
	}
}

func TestSelectMarkers(t *testing.T) {
	base := SelectMarkers(PanelInput{})
	if len(base) != 3 || base[2].Target != "pirA" {
		t.Fatalf("panel dasar harus 16S, amoA, pirA: %+v", base)
	}
	full := SelectMarkers(PanelInput{AHPNDCopy: 500, WSSVCopy: 3, EHPCopy: 20})
	if len(full) != 5 || full[3].Target != "vp28" || full[4].Target != "swp1" {
		t.Fatalf("panel lengkap salah: %+v", full)
	}
	m := Assess([]Taxon{{"V", "Vibrio", 4000}, {"B", "Bacillus", 6000}}, nil, "")
	if r := SelectMarkers(PanelInput{Metrics: &m})[2].Reason; !strings.Contains(r, "Porsi Vibrio") {
		t.Errorf("alasan pirA harus menyebut porsi Vibrio: %s", r)
	}
}

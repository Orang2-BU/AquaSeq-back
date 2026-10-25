package sintetis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"aquaseq-back/internal/science"
)

const dir = "../../testdata/sintetis"

type scenario struct {
	Expect struct {
		Rejected          []string       `json:"rejected"`
		WorstClassAtLeast map[string]int `json:"worst_class_at_least"`
		MaxClassAtMost    map[string]int `json:"max_class_at_most"`
		VibrioRising      []string       `json:"vibrio_rising"`
	} `json:"expect"`
}

func load(t *testing.T) ([]Case, scenario) {
	t.Helper()
	samples, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "skenario.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sc scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatal(err)
	}
	return Cases(samples), sc
}

// Tabel kelas per kolam di log tes membantu menyetel ulang skenario bila mesin SDRI berubah.
func TestSyntheticScenarioMatchesStory(t *testing.T) {
	cases, sc := load(t)
	if len(cases) != 19 {
		t.Fatalf("harus 19 sampel (B3 berhenti DOC 35), dapat %d", len(cases))
	}
	worst, best := map[string]int{}, map[string]int{}
	rejected := map[string]bool{}
	for _, c := range cases {
		f := c.Facts
		vib := "-"
		if f.Microbiome != nil {
			vib = formatPct(f.Microbiome.VibrioPct)
		}
		t.Logf("%-7s DOC %2d  %-20s kelas %d  peluang %7.3f%%  Vibrio %s  penanda %d", f.SampleCode, f.CycleDay, f.Status, f.RiskClass, f.ProbabilityPct, vib, len(f.Markers))
		if f.Status != "VALID" {
			rejected[f.SampleCode] = true
			continue
		}
		if f.RiskClass > worst[f.PondCode] {
			worst[f.PondCode] = f.RiskClass
		}
		if _, ok := best[f.PondCode]; !ok || f.RiskClass < best[f.PondCode] {
			best[f.PondCode] = f.RiskClass
		}
	}
	for _, code := range sc.Expect.Rejected {
		if !rejected[code] {
			t.Errorf("%s seharusnya ditolak mesin", code)
		}
	}
	if len(rejected) != len(sc.Expect.Rejected) {
		t.Errorf("sampel ditolak = %v, ingin hanya %v", rejected, sc.Expect.Rejected)
	}
	for pond, min := range sc.Expect.WorstClassAtLeast {
		if worst[pond] < min {
			t.Errorf("kolam %s: kelas terburuk %d, ingin >= %d", pond, worst[pond], min)
		}
	}
	for pond, max := range sc.Expect.MaxClassAtMost {
		if worst[pond] > max {
			t.Errorf("kolam %s: kelas terburuk %d, ingin <= %d", pond, worst[pond], max)
		}
	}
}

func formatPct(v float64) string {
	b, _ := json.Marshal(v)
	return string(b) + "%"
}

func TestSyntheticMicrobiomeTrend(t *testing.T) {
	cases, sc := load(t)
	for _, pond := range sc.Expect.VibrioRising {
		var first, last float64
		var lastBC *float64
		n := 0
		for _, c := range cases {
			if c.Facts.PondCode != pond || c.Facts.Microbiome == nil {
				continue
			}
			if n == 0 {
				first = c.Facts.Microbiome.VibrioPct
			}
			last = c.Facts.Microbiome.VibrioPct
			lastBC = c.Facts.Microbiome.BrayCurtis
			n++
		}
		if last <= first*2 {
			t.Errorf("kolam %s: porsi Vibrio harus naik jelas (%.1f -> %.1f)", pond, first, last)
		}
		if lastBC == nil || *lastBC < 0.2 {
			t.Errorf("kolam %s: komunitas akhir harus bergeser dari baseline, Bray-Curtis %v", pond, lastBC)
		}
	}
	for _, c := range cases {
		if m := c.Facts.Microbiome; m != nil && m.Status != "VALID" {
			t.Errorf("%s: kelimpahan sintetis harus cukup dalam, dapat %d bacaan", c.Facts.SampleCode, m.TotalReads)
		}
	}
}

func TestTemplatePassesCheckForEverySyntheticSample(t *testing.T) {
	cases, _ := load(t)
	g := science.New(nil, nil)
	for _, c := range cases {
		if err := g.Check(science.Template(c.Facts), c.Facts); err != nil {
			t.Errorf("%s: template gagal diperiksa: %v", c.Facts.SampleCode, err)
		}
	}
}

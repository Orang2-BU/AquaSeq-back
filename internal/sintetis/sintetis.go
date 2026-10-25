// Package sintetis membaca paket data SINTETIS di testdata/sintetis (bukan data tambak nyata) dan menyusunnya
// menjadi fakta mesin per sampel, sama seperti yang diterima backend dari web. Dipakai tes alur dan alat uji
// penjelasan (cmd/uji-penjelasan). Data sintetis hanya membuktikan pipeline berjalan, bukan akurasi model.
package sintetis

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/microbiome"
	"aquaseq-back/internal/science"
	"aquaseq-back/internal/sdri"
)

// Sample adalah satu sampling sintetis: ringkasan Ct (cara ringkas sama dengan web), kimia air, dan kelimpahan 16S.
type Sample struct {
	Code     string
	Pond     string
	CycleDay int
	Date     string
	Features domain.SampleFeatures
	Taxa     []microbiome.Taxon
}

func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

func ptr(v float64) *float64 { return &v }

// summarizeCt meniru web (src/lib/csv/qpcr.ts): rata-rata ulangan yang teramplifikasi dibulatkan 2 desimal;
// target penyakit yang diuji tetapi tidak teramplifikasi dikirim sebagai 40 (negatif).
func summarizeCt(values []float64, tested bool) *float64 {
	if len(values) > 0 {
		s := 0.0
		for _, v := range values {
			s += v
		}
		return ptr(math.Round(s/float64(len(values))*100) / 100)
	}
	if tested {
		return ptr(40)
	}
	return nil
}

// Load membaca semua sampel di dir (testdata/sintetis), urut kolam lalu umur kultur.
func Load(dir string) ([]Sample, error) {
	type acc struct {
		values map[string][]float64
		tested map[string]bool
	}
	wells := map[string]*acc{}
	runs, _ := filepath.Glob(filepath.Join(dir, "qpcr", "run-*.csv"))
	if len(runs) == 0 {
		return nil, fmt.Errorf("tidak ada run qPCR di %s", dir)
	}
	for _, path := range runs {
		rows, err := readCSV(path)
		if err != nil {
			return nil, err
		}
		for _, row := range rows[1:] {
			if len(row) < 4 || row[0] == "" || row[0] == "std_level" || strings.HasPrefix(row[0], "STD") || row[1] == "NTC" {
				continue
			}
			a := wells[row[1]]
			if a == nil {
				a = &acc{values: map[string][]float64{}, tested: map[string]bool{}}
				wells[row[1]] = a
			}
			a.tested[row[2]] = true
			if v, err := strconv.ParseFloat(row[3], 64); err == nil {
				a.values[row[2]] = append(a.values[row[2]], v)
			}
		}
	}
	water, err := readCSV(filepath.Join(dir, "kimia-air.csv"))
	if err != nil {
		return nil, err
	}
	out := []Sample{}
	for _, row := range water[1:] {
		code := row[0]
		a := wells[code]
		if a == nil {
			return nil, fmt.Errorf("sampel %s tidak ada di run qPCR", code)
		}
		num := func(i int) *float64 { v, _ := strconv.ParseFloat(row[i], 64); return ptr(v) }
		day, _ := strconv.Atoi(row[2])
		f := domain.SampleFeatures{
			CTAHPND: summarizeCt(a.values["pirA"], a.tested["pirA"]),
			CTWSSV:  summarizeCt(a.values["vp28"], a.tested["vp28"]),
			CTEHP:   summarizeCt(a.values["swp1"], a.tested["swp1"]),
			CTamoA:  summarizeCt(a.values["amoA"], false),
			CT16S:   summarizeCt(a.values["16S"], false),
			TempC:   num(4), PH: num(5), DOMgL: num(6), TANMgL: num(7), NO2MgL: num(8), H2SMgL: num(9),
			TOMMgL: num(10), Alkalinity: num(11), SalinityPPT: num(12),
		}
		if ppc := summarizeCt(a.values["PPC"], false); ppc != nil {
			f.CTPPC = *ppc
		}
		s := Sample{Code: code, Pond: row[1], CycleDay: day, Date: row[3], Features: f}
		if tf, err := os.Open(filepath.Join(dir, "kelimpahan", code+".csv")); err == nil {
			s.Taxa, err = microbiome.ParseCSV(tf)
			tf.Close()
			if err != nil {
				return nil, fmt.Errorf("kelimpahan %s: %w", code, err)
			}
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pond == out[j].Pond {
			return out[i].CycleDay < out[j].CycleDay
		}
		return out[i].Pond < out[j].Pond
	})
	return out, nil
}

// Case adalah fakta mesin satu sampel beserta hasil SDRI-nya.
type Case struct {
	Sample Sample
	Result domain.SDRIResult
	Facts  science.Facts
}

// Cases menjalankan mesin SDRI dan mikrobioma atas semua sampel; baseline mikrobioma = sampel VALID pertama kolam.
func Cases(samples []Sample) []Case {
	baseline := map[string]Sample{}
	out := []Case{}
	for _, s := range samples {
		assess := sdri.Assess(s.Features)
		f := science.Facts{
			PondCode: s.Pond, SampleCode: s.Code, CycleDay: s.CycleDay, Status: string(assess.Result.Status),
			RiskClass: int(assess.Result.RiskClass), RiskLevel: assess.Result.RiskLevel, ProbabilityPct: assess.Result.ProbabilityPct,
			ModelVersion: assess.Result.ModelVersion, PredictedDisease: assess.Result.PredictedDisease,
			DiseaseName: assess.Result.DiseaseName, Features: assess.Features, MitigationActions: assess.Result.MitigationActions,
		}
		if f.MitigationActions == nil {
			f.MitigationActions = []string{}
		}
		if len(s.Taxa) > 0 && f.Status == "VALID" {
			base, ok := baseline[s.Pond]
			var m microbiome.Metrics
			if ok {
				m = microbiome.Assess(s.Taxa, base.Taxa, base.Code)
			} else {
				m = microbiome.Assess(s.Taxa, nil, "")
				if m.Status == "VALID" {
					baseline[s.Pond] = s
				}
			}
			f.Microbiome = &m
		}
		c := assess.CopiesPerML
		get := func(k string) float64 {
			if c[k] == nil {
				return 0
			}
			return *c[k]
		}
		f.Markers = microbiome.SelectMarkers(microbiome.PanelInput{Metrics: f.Microbiome, AHPNDCopy: get("ahpnd"), WSSVCopy: get("wssv"), EHPCopy: get("ehp")})
		out = append(out, Case{Sample: s, Result: assess.Result, Facts: f})
	}
	return out
}

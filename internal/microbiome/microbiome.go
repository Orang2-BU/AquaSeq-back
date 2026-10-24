// Package microbiome mengolah tabel kelimpahan taksa (hasil klasifikasi 16S dari lab mitra) menjadi metrik
// keragaman, porsi Vibrio, pergeseran komunitas terhadap baseline kolam, dan usulan 3-5 penanda qPCR mingguan.
//
// Semua metrik deterministik. Ambang status adalah heuristik awal (V0) untuk pendukung keputusan, belum
// divalidasi terhadap data wabah; versinya ikut dikirim agar web bisa menampilkannya.
package microbiome

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Version menandai aturan metrik dan ambang heuristik di paket ini.
const Version = "MB-V0-heuristik"

// MinReads adalah kedalaman minimum agar keragaman layak dibandingkan antar sampel.
const MinReads = 5000

// Taxon adalah satu baris tabel kelimpahan.
type Taxon struct {
	Taxon string `json:"taxon"`
	Genus string `json:"genus"`
	Reads int    `json:"reads"`
}

// Metrics adalah ringkasan komunitas mikroba satu sampel.
type Metrics struct {
	Version     string   `json:"version"`
	Status      string   `json:"status"` // VALID atau BELUM_CUKUP (kedalaman di bawah MinReads)
	TotalReads  int      `json:"total_reads"`
	Observed    int      `json:"observed_taxa"`
	Shannon     float64  `json:"shannon"`
	Chao1       float64  `json:"chao1"`
	Evenness    float64  `json:"evenness"`
	VibrioPct   float64  `json:"vibrio_pct"`
	BrayCurtis  *float64 `json:"bray_curtis"` // terhadap baseline kolam; null bila belum ada baseline
	BaselineID  string   `json:"baseline_sample_id,omitempty"`
	ShannonDrop *float64 `json:"shannon_drop_pct"` // penurunan Shannon terhadap baseline (persen); null tanpa baseline
	Signals     []Signal `json:"signals"`
	TopTaxa     []Share  `json:"top_taxa"`
}

// Share adalah porsi relatif satu takson (persen).
type Share struct {
	Taxon string  `json:"taxon"`
	Genus string  `json:"genus"`
	Pct   float64 `json:"pct"`
}

// Signal adalah satu tanda yang dibaca dari komunitas, dengan status dan ambang heuristiknya.
type Signal struct {
	Code      string   `json:"code"`
	Value     *float64 `json:"value"`
	Status    string   `json:"status"` // aman, peringatan, kritis, belum_cukup
	Threshold string   `json:"threshold"`
}

// Marker adalah satu penanda qPCR mingguan yang diusulkan beserta alasannya.
type Marker struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// ParseCSV membaca tabel kelimpahan. Kolom wajib: `taxon` dan `reads`; kolom `genus` opsional (bila kosong,
// diambil dari kata pertama `taxon`). Pemisah koma atau titik koma. Baris dengan nama takson sama dijumlahkan.
func ParseCSV(r io.Reader) ([]Taxon, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 2<<20))
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(raw), string(rune(0xFEFF))) // BOM dari CSV ekspor Excel
	sep := ','
	if first, _, _ := strings.Cut(text, "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
		sep = ';'
	}
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = sep
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV tidak bisa dibaca: %w", err)
	}
	if len(rows) < 2 {
		return nil, errors.New("tabel kelimpahan kosong")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	ti, okT := col["taxon"]
	ri, okR := col["reads"]
	gi, okG := col["genus"]
	if !okT || !okR {
		return nil, errors.New("kolom wajib: taxon dan reads")
	}
	byName := map[string]*Taxon{}
	order := []string{}
	for n, row := range rows[1:] {
		line := n + 2
		cell := func(i int) string {
			if i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		name := cell(ti)
		if name == "" {
			continue
		}
		reads, err := strconv.Atoi(cell(ri))
		if err != nil || reads < 0 {
			return nil, fmt.Errorf("baris %d: reads harus bilangan bulat >= 0", line)
		}
		genus := ""
		if okG {
			genus = cell(gi)
		}
		if genus == "" {
			genus, _, _ = strings.Cut(name, " ")
		}
		if t, ok := byName[name]; ok {
			t.Reads += reads
			continue
		}
		byName[name] = &Taxon{Taxon: name, Genus: genus, Reads: reads}
		order = append(order, name)
	}
	out := make([]Taxon, 0, len(order))
	for _, name := range order {
		if t := byName[name]; t.Reads > 0 {
			out = append(out, *t)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("tidak ada takson dengan reads > 0")
	}
	return out, nil
}

func round(v float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(v*p) / p
}

func ptr(v float64) *float64 { return &v }

// Assess menghitung metrik satu sampel. `baseline` adalah sampel acuan kolam (boleh nil).
func Assess(taxa []Taxon, baseline []Taxon, baselineID string) Metrics {
	total, f1, f2 := 0, 0, 0
	vibrio := 0
	for _, t := range taxa {
		total += t.Reads
		if t.Reads == 1 {
			f1++
		}
		if t.Reads == 2 {
			f2++
		}
		if strings.EqualFold(t.Genus, "Vibrio") {
			vibrio += t.Reads
		}
	}
	m := Metrics{Version: Version, Status: "VALID", TotalReads: total, Observed: len(taxa), Signals: []Signal{}, TopTaxa: []Share{}}
	if total == 0 {
		m.Status = "BELUM_CUKUP"
		return m
	}
	h := 0.0
	for _, t := range taxa {
		p := float64(t.Reads) / float64(total)
		h -= p * math.Log(p)
	}
	m.Shannon = round(h, 3)
	// Chao1 bias-corrected: S_obs + F1(F1-1) / (2(F2+1)).
	m.Chao1 = round(float64(len(taxa))+float64(f1*(f1-1))/float64(2*(f2+1)), 1)
	if len(taxa) > 1 {
		m.Evenness = round(h/math.Log(float64(len(taxa))), 3)
	}
	m.VibrioPct = round(100*float64(vibrio)/float64(total), 2)

	shares := make([]Share, 0, len(taxa))
	for _, t := range taxa {
		shares = append(shares, Share{t.Taxon, t.Genus, round(100*float64(t.Reads)/float64(total), 2)})
	}
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].Pct > shares[j].Pct })
	if len(shares) > 8 {
		shares = shares[:8]
	}
	m.TopTaxa = shares

	if total < MinReads {
		m.Status = "BELUM_CUKUP"
	}
	if len(baseline) > 0 {
		bc := brayCurtis(taxa, baseline)
		m.BrayCurtis = &bc
		m.BaselineID = baselineID
		if b := Assess(baseline, nil, ""); b.Shannon > 0 {
			m.ShannonDrop = ptr(round(100*(b.Shannon-m.Shannon)/b.Shannon, 1))
		}
	}
	m.Signals = signals(m)
	return m
}

// brayCurtis pada porsi relatif (bukan reads mentah) supaya kedalaman berbeda tidak mendominasi.
func brayCurtis(a, b []Taxon) float64 {
	pa, pb := proportions(a), proportions(b)
	shared := 0.0
	for k, v := range pa {
		shared += math.Min(v, pb[k])
	}
	return round(1-shared, 3) // jumlah porsi tiap sampel = 1, jadi BC = 1 - jumlah minimum
}

func proportions(taxa []Taxon) map[string]float64 {
	total := 0
	for _, t := range taxa {
		total += t.Reads
	}
	out := make(map[string]float64, len(taxa))
	for _, t := range taxa {
		out[t.Taxon] += float64(t.Reads) / float64(total)
	}
	return out
}

func high(v, warn, crit float64) string {
	switch {
	case v > crit:
		return "kritis"
	case v > warn:
		return "peringatan"
	}
	return "aman"
}

func signals(m Metrics) []Signal {
	if m.Status != "VALID" {
		return []Signal{{Code: "kedalaman_baca", Value: ptr(float64(m.TotalReads)), Status: "belum_cukup", Threshold: fmt.Sprintf("minimal %d bacaan", MinReads)}}
	}
	out := []Signal{
		{Code: "porsi_vibrio", Value: ptr(m.VibrioPct), Status: high(m.VibrioPct, 10, 30), Threshold: "aman <= 10%; kritis > 30%"},
	}
	if m.ShannonDrop != nil {
		out = append(out, Signal{Code: "penurunan_keragaman", Value: m.ShannonDrop, Status: high(*m.ShannonDrop, 20, 40), Threshold: "aman <= 20%; kritis > 40% dari baseline"})
	} else {
		out = append(out, Signal{Code: "penurunan_keragaman", Status: "belum_cukup", Threshold: "butuh sampel baseline kolam"})
	}
	if m.BrayCurtis != nil {
		out = append(out, Signal{Code: "pergeseran_komunitas", Value: m.BrayCurtis, Status: high(*m.BrayCurtis, 0.4, 0.6), Threshold: "aman <= 0,4; kritis > 0,6 (Bray-Curtis)"})
	} else {
		out = append(out, Signal{Code: "pergeseran_komunitas", Status: "belum_cukup", Threshold: "butuh sampel baseline kolam"})
	}
	return out
}

// PanelInput adalah ringkasan yang dipakai memilih penanda: metrik komunitas terakhir dan jejak qPCR terakhir.
type PanelInput struct {
	Metrics   *Metrics
	AHPNDCopy float64 // kopi/mL dari qPCR terakhir
	WSSVCopy  float64
	EHPCopy   float64
}

// SelectMarkers memilih 3-5 penanda qPCR mingguan dengan aturan yang bisa dijelaskan. Normalisasi (16S) dan
// indikator bakteri baik (amoA) selalu ada; penanda patogen ditambah bila ada bukti di komunitas atau qPCR.
func SelectMarkers(in PanelInput) []Marker {
	out := []Marker{
		{"16S", "Total bakteri untuk menormalkan porsi dari sekuensing ke jumlah mutlak"},
		{"amoA", "Bakteri nitrifikasi (indikator bakteri baik dan siklus nitrogen)"},
	}
	vibrioHigh := in.Metrics != nil && in.Metrics.Status == "VALID" && in.Metrics.VibrioPct > 10
	switch {
	case in.AHPNDCopy >= 10:
		out = append(out, Marker{"pirA", "Jejak AHPND terdeteksi di qPCR terakhir"})
	case vibrioHigh:
		out = append(out, Marker{"pirA", fmt.Sprintf("Porsi Vibrio %.1f%% di sekuensing: pantau gen racun AHPND", in.Metrics.VibrioPct)})
	default:
		out = append(out, Marker{"pirA", "Penyakit bakteri paling merugikan di tambak vaname; tetap dipantau"})
	}
	if in.WSSVCopy > 0 {
		out = append(out, Marker{"vp28", "Jejak WSSV terdeteksi di qPCR terakhir (virus tidak terlihat oleh 16S)"})
	}
	if in.EHPCopy >= 1 && len(out) < 5 {
		out = append(out, Marker{"swp1", "Jejak EHP terdeteksi di qPCR terakhir"})
	}
	return out
}

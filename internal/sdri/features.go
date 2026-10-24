package sdri

import (
	"aquaseq-back/internal/domain"
	"math"
	"strings"
)

type Feature struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Value     *float64 `json:"value"`
	Status    string   `json:"status"`
	Threshold string   `json:"threshold"`
}
type Assessment struct {
	Result         domain.SDRIResult   `json:"sdri_result"`
	Features       []Feature           `json:"engineered_features"`
	CopiesPerML    map[string]*float64 `json:"copies_per_ml"`
	NH3MgL         *float64            `json:"nh3_mg_l"`
	NH4MgL         *float64            `json:"nh4_mg_l"`
	DeltaCT        *float64            `json:"delta_ct"`
	DeltaDeltaCT   *float64            `json:"delta_delta_ct"`
	FoldChange     *float64            `json:"fold_change"`
	FoldRegulation *float64            `json:"fold_regulation"`
	DeltaCTPPC     float64             `json:"delta_ct_ppc"`
	Missing        []string            `json:"missing_features"`
}

func ptr(v float64) *float64 { return &v }
func highStatus(v, safe, critical float64) string {
	if v > critical {
		return "kritis"
	}
	if v > safe {
		return "peringatan"
	}
	return "aman"
}
func lowStatus(v, safe, critical float64) string {
	if v < critical {
		return "kritis"
	}
	if v < safe {
		return "peringatan"
	}
	return "aman"
}

// Assess tidak mengoreksi target saat kontrol gagal; selisih kontrol hanya bukti kebutuhan uji ulang.
func Assess(f domain.SampleFeatures) Assessment {
	a := Assessment{CopiesPerML: map[string]*float64{"ahpnd": nil, "wssv": nil, "ehp": nil}, DeltaCTPPC: f.CTPPC - 19.95, Features: make([]Feature, 0, 12), Missing: []string{}}
	if f.CTPPC < MinValidPPC || f.CTPPC > MaxValidPPC || math.IsNaN(f.CTPPC) || math.IsInf(f.CTPPC, 0) {
		a.Result = Calculate(domain.SDRIInput{CTPPC: f.CTPPC})
		return a
	}
	targets := []struct {
		name string
		v    *float64
	}{{"ct_ahpnd", f.CTAHPND}, {"ct_wssv", f.CTWSSV}, {"ct_ehp", f.CTEHP}, {"ct_amoa", f.CTamoA}, {"ct_16s", f.CT16S}, {"temp_c", f.TempC}, {"ph", f.PH}, {"tan_mg_l", f.TANMgL}, {"no2_mg_l", f.NO2MgL}, {"h2s_mg_l", f.H2SMgL}, {"tom_mg_l", f.TOMMgL}, {"do_mg_l", f.DOMgL}, {"alkalinity", f.Alkalinity}}
	for _, x := range targets {
		if x.v == nil || math.IsNaN(*x.v) || math.IsInf(*x.v, 0) {
			a.Missing = append(a.Missing, x.name)
		}
	}
	if len(a.Missing) > 0 {
		a.Result = domain.SDRIResult{Status: domain.StatusRejectedInvalidQC, RiskClass: domain.ClassInvalid, RiskLevel: "DATA_BELUM_CUKUP", ModelVersion: ModelVersion, Recommendation: "Data belum cukup. Lengkapi parameter yang belum diuji: " + strings.Join(a.Missing, ", ") + ". Nilai tidak terdeteksi perlu dinyatakan eksplisit oleh laboratorium."}
		return a
	}
	for _, x := range []*float64{f.CTAHPND, f.CTWSSV, f.CTEHP, f.CTamoA, f.CT16S} {
		if *x <= 0 || *x > 40 {
			a.Result = domain.SDRIResult{Status: domain.StatusRejectedInvalidQC, RiskLevel: "DATA_TIDAK_VALID", ModelVersion: ModelVersion, Recommendation: "Nilai panel jejak penyakit tidak valid. Periksa berkas laboratorium."}
			return a
		}
	}
	_, _, nh3 := CalculateFreeAmmonia(*f.TANMgL, *f.TempC, *f.PH)
	a.NH3MgL = ptr(nh3)
	a.NH4MgL = ptr(*f.TANMgL - nh3)
	ahpnd, wssv, ehp, amoa := CTToCopiesPerML(*f.CTAHPND), CTToCopiesPerML(*f.CTWSSV), CTToCopiesPerML(*f.CTEHP), CTToCopiesPerML(*f.CTamoA)
	a.CopiesPerML = map[string]*float64{"ahpnd": ptr(ahpnd), "wssv": ptr(wssv), "ehp": ptr(ehp)}
	// ponytail: kontrol referensi CTRL laporan 26,2/18,4 dibekukan V1; referensi per kolam membutuhkan versi baru.
	delta := *f.CTamoA - *f.CT16S
	dd := delta - 7.8
	fc := math.Pow(2, -dd)
	fr := fc
	if fc < 1 {
		fr = -1 / fc
	}
	a.DeltaCT = ptr(delta)
	a.DeltaDeltaCT = ptr(dd)
	a.FoldChange = ptr(fc)
	a.FoldRegulation = ptr(fr)
	add := func(code, name string, v *float64, status, threshold string) {
		a.Features = append(a.Features, Feature{code, name, v, status, threshold})
		if v != nil && status != "aman" {
			a.Result.Evidence = append(a.Result.Evidence, domain.EvidenceItem{Feature: name, Value: *v, Threshold: threshold, Status: status})
		}
	}
	sq, sb, sc := CalculateSubScores(ahpnd+wssv+ehp, amoa, nh3, *f.NO2MgL, *f.H2SMgL, *f.TOMMgL)
	a.Result = Calculate(domain.SDRIInput{CTPPC: f.CTPPC, SQPCR: sq, SBio: sb, SChem: sc})
	add("X1", "kontrol_kualitas", ptr(f.CTPPC), "aman", "18 sampai 22")
	add("X2", "fold_regulation_amoa", ptr(fr), lowStatus(fr, 1, -2), "aman >= 1; kritis < -2")
	ast := "aman"
	if ahpnd >= 10 {
		ast = "peringatan"
	}
	if ahpnd > 1000 {
		ast = "kritis"
	}
	add("X3", "jejak_ahpnd", ptr(ahpnd), ast, "peringatan >= 10; kritis > 1000")
	wst := "aman"
	if wssv > 0 {
		wst = "peringatan"
	}
	if wssv > 100 {
		wst = "kritis"
	}
	add("X4", "jejak_wssv", ptr(wssv), wst, "peringatan > 0; kritis > 100")
	est := "aman"
	if ehp >= 1 {
		est = "peringatan"
	}
	if ehp > 100 {
		est = "kritis"
	}
	add("X5", "jejak_ehp", ptr(ehp), est, "peringatan >= 1; kritis > 100")
	var dys *float64
	ds := "belum_cukup"
	if ahpnd > 0 && amoa > 0 {
		dys = ptr(math.Log10(ahpnd / amoa))
		ds = highStatus(*dys, -2, 0)
	}
	add("X6", "indeks_disbiosis", dys, ds, "aman < -2; kritis > 0; rasio nol tidak terdefinisi")
	add("X7", "nh3_mg_l", ptr(nh3), highStatus(nh3, .05, .1), "aman <= 0,05; kritis > 0,1 mg/L")
	add("X8", "no2_mg_l", f.NO2MgL, highStatus(*f.NO2MgL, .5, 1), "aman <= 0,5; kritis > 1 mg/L")
	add("X9", "h2s_mg_l", f.H2SMgL, highStatus(*f.H2SMgL, .005, .01), "aman <= 0,005; kritis > 0,01 mg/L")
	add("X10", "do_mg_l", f.DOMgL, lowStatus(*f.DOMgL, 5, 4), "aman >= 5; kritis < 4 mg/L")
	add("X11", "tom_mg_l", f.TOMMgL, highStatus(*f.TOMMgL, 70, 90), "aman <= 70; kritis > 90 mg/L")
	alk := lowStatus(*f.Alkalinity, 120, 100)
	if *f.Alkalinity > 180 {
		alk = "di_luar_acuan"
	}
	add("X12", "alkalinity", f.Alkalinity, alk, "aman 120 sampai 180; kritis < 100 mg/L")
	code, name := "NO_TARGET_WARNING", "Belum ada indikasi target di atas ambang; bukan bukti bebas penyakit"
	switch {
	case ahpnd >= 1000 && nh3 > .1:
		code, name = "AHPND_NH3_RISK", "Risiko jejak AHPND disertai amonia tinggi"
	case ahpnd >= 10:
		code, name = "AHPND_RISK", "Indikasi risiko jejak AHPND"
	case nh3 > .1:
		code, name = "AMMONIA_RISK", "Risiko toksisitas amonia"
	case wssv > 0:
		code, name = "WSSV_RISK", "Indikasi risiko jejak WSSV"
	case ehp >= 1:
		code, name = "EHP_RISK", "Indikasi risiko jejak EHP; perlu pemeriksaan pertumbuhan"
	case *f.NO2MgL > 1:
		code, name = "NITRITE_RISK", "Risiko toksisitas nitrit"
	case *f.H2SMgL > .01:
		code, name = "H2S_RISK", "Risiko toksisitas hidrogen sulfida"
	case *f.DOMgL < 4:
		code, name = "HYPOXIA_RISK", "Risiko kekurangan oksigen"
	case *f.Alkalinity < 100:
		code, name = "ALKALINITY_RISK", "Risiko alkalinitas rendah"
	}
	a.Result.PredictedDisease = code
	a.Result.DiseaseName = name
	actions := []string{"Lanjutkan pengukuran air dan pengambilan sampel berkala. Hasil eDNA bukan diagnosis klinis."}
	if len(a.Result.Evidence) > 0 {
		actions = append(actions, "Periksa kondisi udang dan catatan perubahan air bersama pendamping budidaya.")
	}
	if *f.DOMgL < 5 {
		actions = append(actions, "Periksa aerasi segera dan ukur ulang oksigen terlarut.")
	}
	if nh3 > .05 || *f.NO2MgL > .5 || *f.H2SMgL > .005 || *f.TOMMgL > 70 {
		actions = append(actions, "Tinjau sisa pakan dan akumulasi bahan organik; diskusikan pengelolaan air dengan pendamping sebelum tindakan.")
	}
	if ahpnd >= 10 || wssv > 0 || ehp >= 1 {
		actions = append(actions, "Batasi perpindahan air dan peralatan antar kolam. Konfirmasi jejak target melalui laboratorium dan pemeriksaan udang.")
	}
	if a.Result.RiskClass >= domain.ClassSiaga {
		actions = append(actions, "Hubungi pendamping segera untuk penilaian lapangan dan rencana tindak lanjut.")
	}
	a.Result.MitigationActions = actions
	return a
}

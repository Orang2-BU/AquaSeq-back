package ai

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var sampleIDPattern = regexp.MustCompile(`^[A-Za-z0-9_\-\.:]{1,128}$`)

func formatID(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	return strings.ReplaceAll(s, ".", ",")
}

func finiteRange(v, lo, hi float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= lo && v <= hi
}

func BuildExplanation(facts MachineFacts) (ExplanationReport, error) {
	if !facts.DataAvailable || facts.QCStatus != "VALID" {
		return ExplanationReport{}, errors.New("kontrol kualitas atau data mesin belum valid; perlu tinjauan, penjelasan risiko diblokir")
	}
	if !sampleIDPattern.MatchString(facts.PondID) || facts.SDRIClass <= 0 || facts.SDRIClass > 4 || !finiteRange(facts.ProbabilityPct, 0, 100) {
		return ExplanationReport{}, errors.New("fakta mesin tidak valid")
	}

	for _, forbidden := range []string{"antibiotik", "antibiotics", "kaporit", "obat", "sembuh", "dosis"} {
		if strings.Contains(strings.ToLower(facts.RiskLevel), forbidden) {
			return ExplanationReport{}, fmt.Errorf("tingkat risiko memuat instruksi medis terlarang: %s", facts.RiskLevel)
		}
	}

	// Supported core disease codes and aliases
	validDiseases := map[string]string{
		"AHPND":             "AHPND",
		"WSSV":              "WSSV",
		"EHP":               "EHP",
		"AHPND_NH3_RISK":    "AHPND (kombinasi amonia)",
		"AHPND_RISK":        "AHPND",
		"WSSV_RISK":         "WSSV",
		"EHP_RISK":          "EHP",
		"AMMONIA_RISK":      "Amonia Bebas (NH3)",
		"NITRITE_RISK":      "Nitrit (NO2)",
		"H2S_RISK":          "Hidrogen Sulfida (H2S)",
		"HYPOXIA_RISK":      "Hipoksia (Oksigen Rendah)",
		"ALKALINITY_RISK":   "Alkalinitas",
		"NO_TARGET_WARNING": "Peringatan: Tanpa Target Terdeteksi",
	}

	rawDisease := strings.TrimSpace(facts.Disease)
	if rawDisease == "" {
		return ExplanationReport{}, errors.New("panel jejak penyakit tidak didukung atau belum tersedia")
	}
	diseaseLabel, ok := validDiseases[strings.ToUpper(rawDisease)]
	if !ok {
		return ExplanationReport{}, errors.New("panel jejak penyakit tidak didukung atau belum tersedia")
	}

	validRiskLevels := map[string]bool{
		"AMAN":           true,
		"WASPADA_RINGAN": true,
		"WASPADA_TINGGI": true,
		"BAHAYA":         true,
	}
	if !validRiskLevels[strings.ToUpper(strings.TrimSpace(facts.RiskLevel))] {
		return ExplanationReport{}, errors.New("tingkat risiko tidak valid")
	}

	if facts.ChemistryAvailable && (!finiteRange(facts.NH3MgL, 0, 1000) || !finiteRange(facts.TOMMgL, 0, 10000) || !finiteRange(facts.DOMgL, 0, 25)) {
		return ExplanationReport{}, errors.New("kimia air tidak valid")
	}
	if facts.ForecastAvailable && (facts.ForecastHorizonDays < 1 || facts.ForecastHorizonDays > 14 || !finiteRange(facts.ForecastProbabilityPct, 0, 100)) {
		return ExplanationReport{}, errors.New("prakiraan tidak valid")
	}

	foreDays := facts.ForecastHorizonDays
	if foreDays <= 0 {
		foreDays = 7
	}

	disease := fmt.Sprintf("Penyakit yang berpotensi muncul:\nPanel jejak penyakit: %s, tingkat risiko %s (kelas %d). Peringatan dari data eDNA air tambak dan kadar jejak patogen; bukan diagnosis klinis atau konfirmasi penyakit udang.", diseaseLabel, facts.RiskLevel, facts.SDRIClass)

	evidence := fmt.Sprintf("Alasan (angka dari mesin):\nProbabilitas risiko model %s%%; kontrol kualitas valid. Angka ini bukan kepastian wabah atau confidence keputusan dokumen. ", formatID(facts.ProbabilityPct, 1))
	if facts.ChemistryAvailable {
		evidence += fmt.Sprintf("Amonia bebas %s mg/L, TOM %s mg/L, DO %s mg/L. ", formatID(facts.NH3MgL, 2), formatID(facts.TOMMgL, 1), formatID(facts.DOMgL, 1))
	} else {
		evidence += "Parameter kimia air belum tersedia. "
	}

	if facts.AnomalyFlagged {
		evidence += "Mesin menandai anomali trajektori; waktu peringatan dini belum dibuktikan."
	} else {
		evidence += "Tidak ada penanda anomali pada fakta ini; kestabilan atau kondisi petak tidak dapat disimpulkan."
	}

	// Traceable action items with internal repository citations, no "sehat" or medical prescriptions
	actions := []ActionItem{
		{Action: "Tinjau hasil bersama laboratorium dan ahli patologi akuakultur sebelum keputusan lapangan.", Source: "internal/ai/SAFETY.md#review"},
		{Action: "Verifikasi identitas sampel, kontrol kualitas, dan kelengkapan kimia air pada dokumen sumber.", Source: "internal/ai/SAFETY.md#verification"},
	}

	actionText := "Tindakan secepatnya hari ini:\n"
	for i, a := range actions {
		actionText += fmt.Sprintf("%d. %s (Sumber: %s)\n", i+1, a.Action, a.Source)
	}

	var outlook string
	if facts.ForecastAvailable {
		outlook = fmt.Sprintf("Pantauan %d hari:\nProyeksi risiko model %s%%; bukan konfirmasi penyakit atau kepastian kejadian. Pantau konsistensi aerasi dan kadar jejak patogen dari data eDNA air tambak.", foreDays, formatID(facts.ForecastProbabilityPct, 1))
	} else {
		outlook = fmt.Sprintf("Pantauan %d hari:\nPrakiraan risiko belum tersedia; riwayat dan keluaran model yang valid diperlukan. Pantau konsistensi aerasi dan kadar jejak patogen dari data eDNA air tambak.", foreDays)
	}

	return ExplanationReport{
		DiseasePotential: disease,
		EvidenceReason:   evidence,
		Actions:          actions,
		SevenDayOutlook:  outlook,
		FullNarrative:    strings.TrimSpace(disease + "\n\n" + evidence + "\n\n" + actionText + "\n" + outlook),
	}, nil
}

func Verify(narrative string, facts MachineFacts) error {
	report, err := BuildExplanation(facts)
	if err != nil {
		return err
	}
	if strings.TrimSpace(narrative) != strings.TrimSpace(report.FullNarrative) {
		return errors.New("narasi berbeda dari fakta dan tindakan terotorisasi")
	}
	return nil
}

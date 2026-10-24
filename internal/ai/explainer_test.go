package ai

import (
	"strings"
	"testing"
)

func TestExplainerVerification(t *testing.T) {
	facts := MachineFacts{QCStatus: "VALID", DataAvailable: true, ChemistryAvailable: true, ForecastAvailable: true,
		PondID:                 "KOLAM-B2",
		Disease:                "AHPND",
		SDRIClass:              2,
		RiskLevel:              "WASPADA_TINGGI",
		ProbabilityPct:         34.8,
		NH3MgL:                 0.08,
		TOMMgL:                 78.5,
		DOMgL:                  4.2,
		AnomalyFlagged:         true,
		ForecastHorizonDays:    7,
		ForecastProbabilityPct: 48.2,
		TopSimilarCase:         "Kasus Siklus 2025-C1 (remediasi berhasil dengan siphon dan probiotik)",
	}
	report, err := BuildExplanation(facts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(report.FullNarrative, facts); err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	for _, section := range []string{"Penyakit yang berpotensi muncul:", "Alasan (angka dari mesin):", "Tindakan secepatnya hari ini:", "Pantauan 7 hari:"} {
		if !strings.Contains(report.FullNarrative, section) {
			t.Fatalf("missing section %s in %s", section, report.FullNarrative)
		}
	}
	for _, act := range report.Actions {
		if act.Action == "" || act.Source == "" {
			t.Fatalf("action missing source: %+v", act)
		}
	}
	if strings.Contains(report.FullNarrative, "qPCR") || strings.Contains(report.FullNarrative, "Ct") || strings.Contains(report.FullNarrative, "PPC") {
		t.Fatalf("violates terminology dictionary: %s", report.FullNarrative)
	}
	hallucinated := report.FullNarrative + "\nTerdapat kenaikan 99.9 persen bakteri asing."
	if err := Verify(hallucinated, facts); err == nil {
		t.Fatal("hallucinated number accepted")
	}
}

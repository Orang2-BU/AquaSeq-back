package ai

import (
	"strings"
	"testing"
)

func TestExplainerFailsClosedOnMissingOrFailedQC(t *testing.T) {
	// Empty pond ID
	_, err := BuildExplanation(MachineFacts{
		PondID:         "",
		Disease:        "AHPND",
		SDRIClass:      1,
		RiskLevel:      "WASPADA_RINGAN",
		ProbabilityPct: 20.0,
	})
	if err == nil {
		t.Fatal("expected error on empty pond ID")
	}

	// Invalid SDRI class
	_, err = BuildExplanation(MachineFacts{
		PondID:         "POND-1",
		Disease:        "AHPND",
		SDRIClass:      99,
		RiskLevel:      "UNKNOWN",
		ProbabilityPct: 20.0,
	})
	if err == nil {
		t.Fatal("expected error on invalid SDRI class")
	}

	// Invalid probability
	_, err = BuildExplanation(MachineFacts{
		PondID:         "POND-1",
		Disease:        "AHPND",
		SDRIClass:      2,
		RiskLevel:      "WASPADA_RINGAN",
		ProbabilityPct: -5.0,
	})
	if err == nil {
		t.Fatal("expected error on negative probability")
	}

	// Explicit QC failure in facts
	_, err = BuildExplanation(MachineFacts{
		PondID:         "POND-1",
		Disease:        "AHPND",
		SDRIClass:      2,
		RiskLevel:      "WASPADA_RINGAN",
		ProbabilityPct: 30.0,
		QCStatus:       "FAIL",
	})
	if err == nil {
		t.Fatal("expected error when QC status is FAIL")
	}
}

func TestExplainerTerminologyAndNoPrescriptions(t *testing.T) {
	facts := MachineFacts{QCStatus: "VALID", DataAvailable: true,
		PondID:              "POND-A1",
		Disease:             "AHPND",
		SDRIClass:           1,
		RiskLevel:           "AMAN",
		ProbabilityPct:      5.2,
		NH3MgL:              0.01,
		TOMMgL:              45.0,
		DOMgL:               6.2,
		ForecastHorizonDays: 7,
		TopSimilarCase:      "Kasus Siklus 2025-A1",
	}

	report, err := BuildExplanation(facts)
	if err != nil {
		t.Fatal(err)
	}

	narrative := report.FullNarrative

	// Must NOT claim safe/healthy/bebas penyakit
	for _, forbidden := range []string{"sehat", "bebas penyakit", "pasti aman", "bebas patogen"} {
		if strings.Contains(strings.ToLower(narrative), forbidden) {
			t.Fatalf("narrative contains forbidden safe/healthy claim: %q", forbidden)
		}
	}

	// Must NOT prescribe cure/antibiotics/disinfectant dosage
	for _, forbidden := range []string{"antibiotik", "obat", "kaporit", "dosis", "desinfektan", "sembuh"} {
		if strings.Contains(strings.ToLower(narrative), forbidden) {
			t.Fatalf("narrative contains forbidden prescription: %q", forbidden)
		}
	}

	// Must state eDNA risk != confirmed disease
	if !strings.Contains(narrative, "eDNA") || !strings.Contains(narrative, "bukan diagnosis") {
		t.Fatalf("narrative must distinguish eDNA risk from confirmed clinical disease")
	}

	// Must use public terminology
	for _, term := range []string{"data eDNA air tambak", "kadar jejak patogen", "kontrol kualitas"} {
		if !strings.Contains(narrative, term) {
			t.Fatalf("narrative missing required terminology: %q", term)
		}
	}

	// All actions must have traceable source
	for _, act := range report.Actions {
		if act.Action == "" || act.Source == "" {
			t.Fatalf("action missing action or source: %+v", act)
		}
	}
}

func TestVerifyRejectsInventedDiseaseAndActions(t *testing.T) {
	facts := MachineFacts{QCStatus: "VALID", DataAvailable: true,
		PondID:              "POND-A1",
		Disease:             "AHPND",
		SDRIClass:           1,
		RiskLevel:           "WASPADA_RINGAN",
		ProbabilityPct:      15.0,
		NH3MgL:              0.02,
		TOMMgL:              50.0,
		DOMgL:               5.5,
		ForecastHorizonDays: 7,
	}

	report, err := BuildExplanation(facts)
	if err != nil {
		t.Fatal(err)
	}

	// Valid narrative passes
	if err := Verify(report.FullNarrative, facts); err != nil {
		t.Fatalf("valid narrative failed verification: %v", err)
	}

	// Invented disease rejected
	inventedDisease := strings.Replace(report.FullNarrative, "AHPND", "COVID-19", -1)
	if err := Verify(inventedDisease, facts); err == nil {
		t.Fatal("verification should reject invented disease")
	}

	// Invented unauthorized action / antibiotic rejected
	inventedAction := report.FullNarrative + "\nBeri antibiotik enrofloxacin 5 gram per kg pakan."
	if err := Verify(inventedAction, facts); err == nil {
		t.Fatal("verification should reject unauthorized antibiotic/prescription action")
	}
}

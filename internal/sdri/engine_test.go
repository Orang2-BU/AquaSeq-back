package sdri

import (
	"math"
	"testing"

	"aquaseq-back/internal/domain"
)

func TestGoldenArchetypesExactLabValues(t *testing.T) {
	tests := []struct {
		name        string
		input       domain.SDRIInput
		wantStatus  domain.RiskStatus
		wantProbMin float64
		wantProbMax float64
		wantClass   domain.RiskClass
	}{
		{
			name: "Archetype 1 - CTRL Inlet DOC 15 (0.04% Normal)",
			input: domain.SDRIInput{
				CTPPC: 19.95,
				SQPCR: 0.0,
				SBio:  -2.10588,
				SChem: 0.65714,
			},
			wantStatus:  domain.StatusValid,
			wantProbMin: 0.035,
			wantProbMax: 0.045,
			wantClass:   domain.ClassAman,
		},
		{
			name: "Archetype 2 - POND-BIO Bioflok DOC 60 (0.02% Protektif)",
			input: domain.SDRIInput{
				CTPPC: 19.95,
				SQPCR: 0.15,
				SBio:  -3.87059,
				SChem: 1.77143,
			},
			wantStatus:  domain.StatusValid,
			wantProbMin: 0.015,
			wantProbMax: 0.028,
			wantClass:   domain.ClassAman,
		},
		{
			name: "Archetype 3 - POND-TOX Limbah DOC 85 (99.998% Kritis/Wabah)",
			input: domain.SDRIInput{
				CTPPC: 19.95,
				SQPCR: 4.20,
				SBio:  2.80,
				SChem: 14.11428,
			},
			wantStatus:  domain.StatusValid,
			wantProbMin: 99.995,
			wantProbMax: 99.999,
			wantClass:   domain.ClassBahaya,
		},
		{
			name: "Archetype 4 - POND-REM Remediasi DOC 90 (34.80% Early Warning)",
			input: domain.SDRIInput{
				CTPPC: 19.95,
				SQPCR: 1.51667,
				SBio:  -1.69412,
				SChem: 7.82857,
			},
			wantStatus:  domain.StatusValid,
			wantProbMin: 34.5,
			wantProbMax: 35.0,
			wantClass:   domain.ClassSiaga,
		},
		{
			name: "Archetype 5 - UNK-HUMIC Asam Humat (REJECT PPC > 22.0)",
			input: domain.SDRIInput{
				CTPPC: 24.46, // QIAGEN PPC > 22.0 -> Humic Acid Inhibition
				SQPCR: 0.625,
				SBio:  -2.1176,
				SChem: 2.30,
			},
			wantStatus: domain.StatusRejectedInhibitor,
			wantClass:  domain.ClassInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Calculate(tc.input)

			if res.Status != tc.wantStatus {
				t.Fatalf("expected status %v, got %v", tc.wantStatus, res.Status)
			}

			if tc.wantStatus == domain.StatusValid {
				if res.ProbabilityPct < tc.wantProbMin || res.ProbabilityPct > tc.wantProbMax {
					t.Errorf("probability %v outside expected window [%v, %v]", res.ProbabilityPct, tc.wantProbMin, tc.wantProbMax)
				}
				if res.RiskClass != tc.wantClass {
					t.Errorf("expected class %v, got %v", tc.wantClass, res.RiskClass)
				}
			} else {
				if res.ProbabilityPct != 0 {
					t.Errorf("inhibited sample must have 0 probability, got %v", res.ProbabilityPct)
				}
			}
		})
	}
}

func TestScientificFormulas(t *testing.T) {
	// 1. Emerson et al. Free Ammonia calculation at 30 degC, pH 7.80, TAN 0.25 mg/L (CTRL)
	pKa, fNH3, nh3 := CalculateFreeAmmonia(0.250, 30.0, 7.80)
	if math.Abs(pKa-9.095) > 0.01 {
		t.Errorf("expected pKa ~9.095, got %f", pKa)
	}
	if math.Abs(fNH3-0.0477) > 0.005 {
		t.Errorf("expected fNH3 ~0.0477 (4.77%%), got %f", fNH3)
	}
	if math.Abs(nh3-0.0119) > 0.002 {
		t.Errorf("expected NH3 ~0.0119 mg/L, got %f", nh3)
	}

	// 2. Standard curve conversion: CT 16.50 should yield ~1.00E+06 copies/rxn -> 80,000 copies/mL
	copiesML := CTToCopiesPerML(16.50)
	if math.Abs(copiesML-80000.0)/80000.0 > 0.01 {
		t.Errorf("expected ~80,000 copies/mL at CT 16.50, got %f", copiesML)
	}

	// Undetermined (CT >= 40) must return 0
	if CTToCopiesPerML(40.0) != 0 {
		t.Errorf("CT 40.0 must return 0 copies/mL")
	}

	// 3. Sub-scores derivation
	sQPCR, sBio, sChem := CalculateSubScores(0.40, 101.76, 0.0356, 0.350, 0.002, 78.0)
	if sQPCR <= 0 {
		t.Errorf("expected positive sQPCR, got %f", sQPCR)
	}
	if sBio >= 0 {
		t.Errorf("expected negative sBio when amoA > pathogen, got %f", sBio)
	}
	if sChem <= 0 {
		t.Errorf("expected positive sChem, got %f", sChem)
	}
}

func TestPPCBoundaries(t *testing.T) {
	// Below QIAGEN 18.0
	resLow := Calculate(domain.SDRIInput{CTPPC: 17.5})
	if resLow.Status != domain.StatusRejectedInhibitor {
		t.Errorf("CTPPC 17.5 should be rejected, got %v", resLow.Status)
	}

	// Above QIAGEN 22.0
	resHigh := Calculate(domain.SDRIInput{CTPPC: 22.5})
	if resHigh.Status != domain.StatusRejectedInhibitor {
		t.Errorf("CTPPC 22.5 should be rejected, got %v", resHigh.Status)
	}

	// Exactly valid QIAGEN PPC
	resOk := Calculate(domain.SDRIInput{CTPPC: 19.95})
	if resOk.Status != domain.StatusValid {
		t.Errorf("CTPPC 19.95 should be valid, got %v", resOk.Status)
	}
}

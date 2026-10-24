package sdri

import (
	"math"
	"testing"

	"aquaseq-back/internal/domain"
)

// Nama historis dipertahankan; fixture subskor ini sintetis, bukan data mentah atau ground truth klinis.
// Oracle logit/sigmoid dihitung independen dengan Python math; rentang lama tetap diperiksa.
func TestGoldenArchetypesExactLabValues(t *testing.T) {
	tests := []struct {
		name        string
		input       domain.SDRIInput
		wantStatus  domain.RiskStatus
		wantZScore  float64
		wantProb    float64
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
			wantZScore:  -7.8300,
			wantProb:    0.0397,
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
			wantZScore:  -8.3700,
			wantProb:    0.0232,
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
			wantZScore:  10.8000,
			wantProb:    99.9980,
			wantProbMin: 99.995,
			wantProbMax: 99.999,
			wantClass:   domain.ClassBahaya,
		},
		{
			name: "Archetype 4 - POND-REM synthetic (34.5247% model risk)",
			input: domain.SDRIInput{
				CTPPC: 19.95,
				SQPCR: 1.51667,
				SBio:  -1.69412,
				SChem: 7.82857,
			},
			wantStatus:  domain.StatusValid,
			wantZScore:  -0.6400,
			wantProb:    34.5247,
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
				assertGoldenNear(t, "z", res.ZScore, tc.wantZScore, 1e-12)
				assertGoldenNear(t, "percentage", res.ProbabilityPct, tc.wantProb, 1e-12)
				if res.RiskClass != tc.wantClass {
					t.Errorf("expected class %v, got %v", tc.wantClass, res.RiskClass)
				}
			} else {
				if res.ProbabilityPct != 0 {
					t.Errorf("rejected result must retain the unscored numeric sentinel, got %v", res.ProbabilityPct)
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

// Fixture data mentah dari laporan, bukan ground truth klinis/laboratorium lapangan terverifikasi.
// Tabel 1.2/4.3 dan benchmark 7.3 tidak konsisten; koefisien V1 tidak disesuaikan.
// Oracle dihitung independen dengan Python math stdlib dari rumus 7.3, bukan keluaran engine:
// copies = 0.08 * 10**((36.6-Ct)/3.35); pathogen = AHPND + WSSV + EHP;
// NH3 = TAN / (10**(0.09018+2729.92/(temp+273.15)-pH)+1);
// SQPCR = log10(pathogen+1); SBio = log10((pathogen+1)/(amoA+1));
// SChem = NH3/.10 + NO2/1 + H2S/.01 + TOM/90;
// z = -6.5 + 1.2*SQPCR + .85*SBio + .7*SChem; pct = 100/(1+exp(-z)).
func TestGoldenIngestRawReportConflict(t *testing.T) {
	num := func(v float64) *float64 { return &v }

	// 1. CTRL DOC 15
	ctrl := domain.SampleFeatures{
		CTAHPND: num(40.0), CTWSSV: num(40.0), CTEHP: num(40.0), CTamoA: num(26.2), CT16S: num(18.4),
		CTPPC: 19.95, DOMgL: num(6.4), TOMMgL: num(34.5), PH: num(7.8), SalinityPPT: num(20.0),
		TempC: num(30.0), TANMgL: num(0.25), NO2MgL: num(0.08), H2SMgL: num(0.001), Alkalinity: num(140.0),
	}
	resCTRL := Assess(ctrl).Result
	if resCTRL.Status != domain.StatusValid || resCTRL.RiskClass != domain.ClassAman {
		t.Errorf("CTRL: expected Class 1 (Aman), got %v", resCTRL.RiskClass)
	}

	// 2. POND-BIO DOC 60
	bio := domain.SampleFeatures{
		CTAHPND: num(34.25), CTWSSV: num(40.0), CTEHP: num(40.0), CTamoA: num(19.55), CT16S: num(18.38),
		CTPPC: 19.95, DOMgL: num(5.8), TOMMgL: num(78.0), PH: num(7.9), SalinityPPT: num(21.0),
		TempC: num(30.0), TANMgL: num(0.60), NO2MgL: num(0.35), H2SMgL: num(0.002), Alkalinity: num(165.0),
	}
	resBIO := Assess(bio).Result
	if resBIO.Status != domain.StatusValid || resBIO.RiskClass != domain.ClassAman {
		t.Errorf("POND-BIO: expected Class 1 (Aman), got %v", resBIO.RiskClass)
	}

	// 3. POND-TOX DOC 85
	tox := domain.SampleFeatures{
		CTAHPND: num(18.84), CTWSSV: num(40.0), CTEHP: num(25.16), CTamoA: num(28.22), CT16S: num(18.44),
		CTPPC: 19.95, DOMgL: num(2.65), TOMMgL: num(128.4), PH: num(8.4), SalinityPPT: num(22.5),
		TempC: num(30.0), TANMgL: num(3.20), NO2MgL: num(4.85), H2SMgL: num(0.092), Alkalinity: num(82.0),
	}
	resTOX := Assess(tox).Result
	if resTOX.Status != domain.StatusValid || resTOX.RiskClass != domain.ClassBahaya {
		t.Errorf("POND-TOX: expected Class 4 (Bahaya), got %v", resTOX.RiskClass)
	}

	// 4. POND-REM DOC 90 (Benchmark dispute test)
	rem := domain.SampleFeatures{
		CTAHPND: num(27.89), CTWSSV: num(40.0), CTEHP: num(28.56), CTamoA: num(22.87), CT16S: num(18.45),
		CTPPC: 19.95, DOMgL: num(5.5), TOMMgL: num(52.0), PH: num(7.85), SalinityPPT: num(20.5),
		TempC: num(30.0), TANMgL: num(0.45), NO2MgL: num(0.42), H2SMgL: num(0.004), Alkalinity: num(148.0),
	}
	resREM := Assess(rem).Result
	if resREM.Status != domain.StatusValid {
		t.Fatalf("POND-REM: expected VALID status, got %v", resREM.Status)
	}
	// Z -5,24 yang tertulis di tabel memberi 0,5272%; ingest data mentah memberi 1,2493%.
	if resREM.ProbabilityPct != 1.2493 || resREM.ZScore != -4.3701 {
		t.Fatalf("POND-REM formula changed: %+v", resREM)
	}
	t.Logf("POND-REM raw stoikiometri: prob=%.4f%%, Z=%.4f (catatan benchmark dispute terhadap klaim 34.80%%)", resREM.ProbabilityPct, resREM.ZScore)

	// 5. UNK-HUMIC PPC FAIL
	humic := domain.SampleFeatures{
		CTAHPND: num(31.24), CTWSSV: num(40.0), CTEHP: num(40.0), CTamoA: num(24.13), CT16S: num(18.42),
		CTPPC: 24.46, DOMgL: num(4.9), TOMMgL: num(112.0), PH: num(7.6), SalinityPPT: num(18.0),
		TempC: num(30.0), TANMgL: num(0.85), NO2MgL: num(0.60), H2SMgL: num(0.008), Alkalinity: num(115.0),
	}
	resHumic := Assess(humic).Result
	if resHumic.Status != domain.StatusRejectedInhibitor || resHumic.RiskClass != domain.ClassInvalid {
		t.Errorf("UNK-HUMIC: expected REJECTED_INHIBITOR (Class 0), got status=%v, class=%v", resHumic.Status, resHumic.RiskClass)
	}
	if resHumic.ZScore != 0 || resHumic.ProbabilityPct != 0 || resHumic.SQPCR != 0 || resHumic.SBio != 0 || resHumic.SChem != 0 {
		t.Errorf("UNK-HUMIC: rejected raw sample must not be scored: %+v", resHumic)
	}

	for _, tc := range []struct {
		name                 string
		got                  domain.SDRIResult
		sq, sb, sc, z, pct   float64
		roundedZ, roundedPct float64
		class                domain.RiskClass
	}{
		{"CTRL", resCTRL, 0, -2.0118146886655266, 0.6838702338592011, -7.731333321664256, 0.043866607480306394, -7.7313, 0.0439, domain.ClassAman},
		{"POND-BIO", resBIO, 0.1468502557018499, -3.8458361393435974, 1.7763688560652318, -8.349282212354176, 0.023651031148138494, -8.3493, 0.0237, domain.ClassAman},
		{"POND-TOX", resTOX, 4.2102121880465155, 2.7888499359829453, 20.847056937129846, 15.515716927232214, 99.99998173542058, 15.5157, 100, domain.ClassBahaya},
		{"POND-REM", resREM, 1.7238140026250786, -1.278215931607395, 1.6397943009317297, -4.370050728063981, 1.2492560441075522, -4.3701, 1.2493, domain.ClassAman},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.Status != domain.StatusValid || tc.got.RiskClass != tc.class || tc.got.ModelVersion != "V1-GSI" {
				t.Fatalf("golden identity changed: %+v", tc.got)
			}
			for _, score := range []struct {
				name                 string
				got, want, tolerance float64
			}{
				{"s_qpcr", tc.got.SQPCR, tc.sq, 1e-10},
				{"s_bio", tc.got.SBio, tc.sb, 1e-10},
				{"s_chem", tc.got.SChem, tc.sc, 1e-10},
				// Z and percentage are rounded to four decimals in the public engine result.
				{"z", tc.got.ZScore, tc.z, 0.00005},
				{"percentage", tc.got.ProbabilityPct, tc.pct, 0.00005},
			} {
				assertGoldenNear(t, score.name, score.got, score.want, score.tolerance)
			}
			if tc.got.ZScore != tc.roundedZ || tc.got.ProbabilityPct != tc.roundedPct {
				t.Errorf("four-decimal contract: z=%.4f pct=%.4f; want z=%.4f pct=%.4f", tc.got.ZScore, tc.got.ProbabilityPct, tc.roundedZ, tc.roundedPct)
			}
			t.Logf("SQPCR=%.15g SBio=%.15g SChem=%.15g Z=%.4f P=%.4f%% class=%d", tc.got.SQPCR, tc.got.SBio, tc.got.SChem, tc.got.ZScore, tc.got.ProbabilityPct, tc.got.RiskClass)
		})
	}
}

func assertGoldenNear(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-want) > tolerance {
		t.Errorf("%s=%.17g; want %.17g +/- %g", name, got, want, tolerance)
	}
}

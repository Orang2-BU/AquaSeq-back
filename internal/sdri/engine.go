package sdri

import (
	"math"

	"aquaseq-back/internal/domain"
)

// Model coefficients - V1-GSI Prior
const (
	WeightIntercept = -6.50
	WeightQPCR      = 1.20
	WeightBio       = 0.85
	WeightChem      = 0.70
	ModelVersion    = "V1-GSI"

	// QIAGEN RT2 PPC Standard (20 +/- 2)
	MinValidPPC = 18.0
	MaxValidPPC = 22.0
)

// Calculate computes the deterministic SDRI score.
// ponytail: using logistic regression with frozen V1 weights; calibrate weights per cycle outcome in V2.
func Calculate(input domain.SDRIInput) domain.SDRIResult {
	// Gatekeeper: Positive Process Control (PPC) validation
	if input.CTPPC <= 0 || input.CTPPC > MaxValidPPC || input.CTPPC < MinValidPPC {
		return domain.SDRIResult{
			Status:         domain.StatusRejectedInhibitor,
			ZScore:         0,
			ProbabilityPct: 0,
			RiskClass:      domain.ClassInvalid,
			RiskLevel:      "INVALID_PPC",
			SQPCR:          input.SQPCR,
			SBio:           input.SBio,
			SChem:          input.SChem,
			ModelVersion:   ModelVersion,
			Recommendation: "Uji PCR terhambat (PPC abnormal). Sampel wajib diulang/ekstraksi ulang sebelum dievaluasi.",
		}
	}

	// Deterministic logit: Z = -6.50 + 1.20*S_qPCR + 0.85*S_Bio + 0.70*S_Chem
	z := WeightIntercept + (WeightQPCR * input.SQPCR) + (WeightBio * input.SBio) + (WeightChem * input.SChem)

	// Sigmoid: P = 1 / (1 + exp(-Z))
	prob := 1.0 / (1.0 + math.Exp(-z))
	probPct := prob * 100.0

	// Determine risk class
	riskClass, riskLevel, rec := classifyRisk(probPct)

	return domain.SDRIResult{
		Status:         domain.StatusValid,
		ZScore:         math.Round(z*10000) / 10000,
		ProbabilityPct: math.Round(probPct*10000) / 10000,
		RiskClass:      riskClass,
		RiskLevel:      riskLevel,
		SQPCR:          input.SQPCR,
		SBio:           input.SBio,
		SChem:          input.SChem,
		ModelVersion:   ModelVersion,
		Recommendation: rec,
	}
}

// CalculateFreeAmmonia calculates un-ionized toxic NH3-N from TAN, Temp, and pH via Emerson et al. (1975)
func CalculateFreeAmmonia(tan, tempC, ph float64) (pKa, fNH3, nh3 float64) {
	tKelvin := tempC + 273.15
	pKa = 0.09018 + (2729.92 / tKelvin)
	fNH3 = 1.0 / (math.Pow(10, pKa-ph) + 1.0)
	nh3 = tan * fNH3
	return pKa, fNH3, nh3
}

// CTToCopiesPerML converts qPCR Ct to copies/mL based on standard curve regression
// CT = -3.3500 * log10(N) + 36.6000 -> N_rxn = 10^((36.6000 - CT)/3.3500)
// Copies/mL = N_rxn * (100uL / 5uL) / 250mL = N_rxn * 0.08
func CTToCopiesPerML(ct float64) float64 {
	if ct >= 40.0 || ct <= 0 {
		return 0.0
	}
	nRxn := math.Pow(10, (36.6000-ct)/3.3500)
	return nRxn * 0.08
}

// CalculateSubScores computes S_qPCR, S_Bio, and S_Chem from raw scientific parameters
func CalculateSubScores(pathogenCopyML, amoaCopyML, nh3MgL, no2MgL, h2sMgL, tomMgL float64) (sQPCR, sBio, sChem float64) {
	sQPCR = math.Log10(pathogenCopyML + 1.0)
	sBio = math.Log10((pathogenCopyML + 1.0) / (amoaCopyML + 1.0))
	sChem = (nh3MgL / 0.10) + (no2MgL / 1.00) + (h2sMgL / 0.01) + (tomMgL / 90.0)
	return sQPCR, sBio, sChem
}

func classifyRisk(probPct float64) (domain.RiskClass, string, string) {
	switch {
	case probPct < 5.0:
		return domain.ClassAman, "AMAN", "Kondisi optimal. Lanjutkan sampling rutin."
	case probPct < 25.0:
		return domain.ClassWaspada, "WASPADA_RINGAN", "Pola awal fluktuasi air. Pantau kestabilan DO dan aerasi."
	case probPct < 60.0:
		return domain.ClassSiaga, "WASPADA_TINGGI", "Deviasi patogen/bioindikator signifikan. Kurangi pakan 20% dan siphon dasar petak."
	default:
		return domain.ClassBahaya, "BAHAYA", "Beban patogen kritis / wabah imminent. Isolasi saluran buang, desinfeksi terkontrol, dan mitigasi darurat."
	}
}

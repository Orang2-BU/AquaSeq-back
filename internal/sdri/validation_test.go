package sdri

import (
	"encoding/json"
	"math"
	"testing"

	"aquaseq-back/internal/domain"
)

func TestPPCExactInclusiveBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ppc    float64
		status domain.RiskStatus
	}{
		{"minimum_inclusive", 18, domain.StatusValid},
		{"maximum_inclusive", 22, domain.StatusValid},
		{"one_float_below_minimum", math.Nextafter(18, math.Inf(-1)), domain.StatusRejectedInhibitor},
		{"one_float_above_maximum", math.Nextafter(22, math.Inf(1)), domain.StatusRejectedInhibitor},
		{"NaN", math.NaN(), domain.StatusRejectedInhibitor},
		{"positive_infinity", math.Inf(1), domain.StatusRejectedInhibitor},
		{"negative_infinity", math.Inf(-1), domain.StatusRejectedInhibitor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := domain.SDRIInput{CTPPC: tc.ppc}
			f := rawFeatures()
			f.CTPPC = tc.ppc
			for name, got := range map[string]domain.SDRIResult{"Calculate": Calculate(input), "Assess": Assess(f).Result} {
				if got.Status != tc.status || got.RiskClass == domain.ClassInvalid && tc.status == domain.StatusValid || got.RiskClass != domain.ClassInvalid && tc.status != domain.StatusValid {
					t.Errorf("%s at PPC %.17g: %+v", name, tc.ppc, got)
				}
			}
		})
	}
}

func TestCalculateRejectsNonFiniteSubscores(t *testing.T) {
	for _, field := range []string{"s_qpcr", "s_bio", "s_chem"} {
		for _, bad := range []struct {
			name  string
			value float64
		}{{"NaN", math.NaN()}, {"positive_infinity", math.Inf(1)}, {"negative_infinity", math.Inf(-1)}} {
			t.Run(field+"/"+bad.name, func(t *testing.T) {
				input := domain.SDRIInput{CTPPC: 19.95, SQPCR: 1, SBio: -2, SChem: 1}
				switch field {
				case "s_qpcr":
					input.SQPCR = bad.value
				case "s_bio":
					input.SBio = bad.value
				case "s_chem":
					input.SChem = bad.value
				}
				got := Calculate(input)
				if got.Status != domain.StatusRejectedInvalidQC || got.RiskClass != domain.ClassInvalid {
					t.Fatalf("non-finite subscore must fail closed: %+v", got)
				}
				if got.ModelVersion != "V1-GSI" || got.Recommendation == "" {
					t.Errorf("rejection needs model identity and explanation: %+v", got)
				}
				for name, value := range map[string]float64{"z": got.ZScore, "probability": got.ProbabilityPct, "s_qpcr": got.SQPCR, "s_bio": got.SBio, "s_chem": got.SChem} {
					if math.IsNaN(value) || math.IsInf(value, 0) {
						t.Errorf("rejection leaked non-finite %s: %g", name, value)
					}
				}
				if _, err := json.Marshal(got); err != nil {
					t.Errorf("rejection is not JSON encodable: %v", err)
				}
			})
		}
	}
}

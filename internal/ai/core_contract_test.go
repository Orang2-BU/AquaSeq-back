package ai

import (
	"strings"
	"testing"
)

func TestCoreMachineFactsContract(t *testing.T) {
	for _, code := range []string{"AHPND_NH3_RISK", "AHPND_RISK", "WSSV_RISK", "EHP_RISK", "AMMONIA_RISK", "NITRITE_RISK", "H2S_RISK", "HYPOXIA_RISK", "ALKALINITY_RISK", "NO_TARGET_WARNING", "AHPND", "WSSV", "EHP"} {
		for _, level := range []string{"AMAN", "WASPADA_RINGAN", "WASPADA_TINGGI", "BAHAYA"} {
			f := MachineFacts{PondID: "01234567-89ab-cdef-0123-456789abcdef", Disease: code, SDRIClass: 1, RiskLevel: level, QCStatus: "VALID", DataAvailable: true}
			r, e := BuildExplanation(f)
			if e != nil {
				t.Fatalf("%s/%s: %v", code, level, e)
			}
			if code == "NO_TARGET_WARNING" && strings.Contains(r.FullNarrative, "AHPND") {
				t.Fatal("default disease fabricated")
			}
		}
	}
	f := MachineFacts{PondID: "POND-1", Disease: "AHPND_RISK", SDRIClass: 0, RiskLevel: "AMAN", QCStatus: "VALID", DataAvailable: true}
	if _, e := BuildExplanation(f); e == nil {
		t.Fatal("class zero (rejected) explained as valid")
	}
	f.SDRIClass = 1
	f.RiskLevel = "RENDAH"
	if _, e := BuildExplanation(f); e == nil {
		t.Fatal("unsupported core risk enum accepted")
	}
}

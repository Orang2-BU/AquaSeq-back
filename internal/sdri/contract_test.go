package sdri

import (
	"aquaseq-back/internal/domain"
	"testing"
)

func TestRiskClassContract(t *testing.T) {
	for _, tc := range []struct {
		pct   float64
		class int
	}{{0, 1}, {4.999, 1}, {5, 2}, {25, 3}, {60, 4}} {
		got, _, _ := classifyRisk(tc.pct)
		if int(got) != tc.class {
			t.Errorf("%.3f: got %d want %d", tc.pct, got, tc.class)
		}
	}
	if int(Calculate(domain.SDRIInput{CTPPC: 24.46}).RiskClass) != 0 {
		t.Fatal("penolakan harus kelas 0")
	}
}

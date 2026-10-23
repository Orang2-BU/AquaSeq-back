package report

import (
	"fmt"
	"strings"

	"aquaseq-back/internal/domain"
)

type WhatsAppPayload struct {
	PondCode       string            `json:"pond_code"`
	ProbabilityPct float64           `json:"probability_pct"`
	RiskClass      domain.RiskClass  `json:"risk_class"`
	PrimaryDriver  string            `json:"primary_driver"`
	Action         string            `json:"action"`
}

// GenerateWhatsAppMessage formats calculated SDRI results into concise, clear Indonesian text.
// ponytail: deterministic message template; wire LLM service when dynamic multilingual prompts are required.
func GenerateWhatsAppMessage(p WhatsAppPayload) string {
	var statusLabel string
	var icon string

	switch p.RiskClass {
	case domain.ClassAman:
		statusLabel = "Aman (Risiko Minimal)"
		icon = "✅"
	case domain.ClassWaspada:
		statusLabel = "Waspada Ringan"
		icon = "⚠️"
	case domain.ClassSiaga:
		statusLabel = "Waspada Tinggi"
		icon = "🚨"
	case domain.ClassBahaya:
		statusLabel = "Bahaya Kritis"
		icon = "🛑"
	default:
		statusLabel = "Uji Ditolak / Perlu Pengulangan"
		icon = "❓"
	}

	driver := strings.TrimSpace(p.PrimaryDriver)
	if driver == "" {
		driver = "Parameter dalam rentang baseline normal"
	}

	action := strings.TrimSpace(p.Action)
	if action == "" {
		action = "Pertahankan manajemen air dan aerasi stabil."
	}

	return fmt.Sprintf(
		"%s *LAPORAN MONITORING AQUASEQ*\n"+
			"Petak: *%s*\n"+
			"Status: *%s (%.2f%%)*\n"+
			"Pemicu Utama: %s\n"+
			"Rekomendasi Tindakan: %s\n"+
			"Catatan: Hasil interpretasi eDNA air. Konsultasikan dengan tim pendamping bila ragu.",
		icon, p.PondCode, statusLabel, p.ProbabilityPct, driver, action,
	)
}

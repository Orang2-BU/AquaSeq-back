package trajectory

import (
	"fmt"
	"math"

	"aquaseq-back/internal/domain"
)

// EvaluateTrajectory assesses the trend and triggers early trajectory alerts.
// ponytail: linear slope over cycle days; upgrade to spline/Kalman filter when real field noise profiles arrive.
func EvaluateTrajectory(pondID string, points []domain.TrajectoryPoint) domain.TrajectoryReport {
	n := len(points)
	if n < 2 {
		return domain.TrajectoryReport{
			PondID:          pondID,
			DataPoints:      n,
			Slope:           0,
			Trend:           "INSUFFICIENT_DATA",
			TrajectoryAlert: false,
			AlertMessage:    "Data sampling belum cukup untuk evaluasi trajektori (minimal 2 titik).",
			History:         points,
		}
	}

	// Calculate linear regression slope: (dP / dDay)
	var sumX, sumY, sumXY, sumXX float64
	for _, p := range points {
		x := float64(p.CycleDay)
		y := p.ProbabilityPct
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}

	denom := float64(n)*sumXX - sumX*sumX
	var slope float64
	if math.Abs(denom) > 1e-9 {
		slope = (float64(n)*sumXY - sumX*sumY) / denom
	}

	slopeRounded := math.Round(slope*1000) / 1000

	// Determine trend and anomaly alert
	trend := "STABLE"
	alert := false
	var alertMsg string

	latest := points[n-1]
	initial := points[0]

	switch {
	case slopeRounded >= 0.5:
		trend = "RAPID_INCREASE"
		alert = true
		alertMsg = fmt.Sprintf("Waspada Dini: Trajektori risiko melonjak +%.2f%%/hari. Pola mengindikasikan deviasi kurva sebelum kelas berbalik ke bahaya.", slopeRounded)
	case slopeRounded >= 0.15:
		trend = "MODERATE_INCREASE"
		// If rising steadily and crossed 15% from low baseline, trigger early alert
		if latest.ProbabilityPct > 15.0 && initial.ProbabilityPct < 5.0 {
			alert = true
			alertMsg = "Peringatan Deviasi: Kurva risiko meningkat konsisten dari baseline kolam. Rekomendasi cek bioindikator dan siphon."
		} else {
			alertMsg = "Tren risiko meningkat perlahan. Pantau sampling berikutnya."
		}
	case slopeRounded <= -0.15:
		trend = "DECREASING"
		alertMsg = "Tren risiko membaik/menurun stabil."
	default:
		trend = "STABLE"
		alertMsg = "Kondisi trajektori stabil dalam batas normal."
	}

	return domain.TrajectoryReport{
		PondID:          pondID,
		DataPoints:      n,
		Slope:           slopeRounded,
		Trend:           trend,
		TrajectoryAlert: alert,
		AlertMessage:    alertMsg,
		History:         points,
	}
}

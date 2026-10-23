package trajectory

import (
	"testing"

	"aquaseq-back/internal/domain"
)

func TestTrajectoryPondCSpike(t *testing.T) {
	// Pond C outbreak arc synthetic simulation: day 30 to day 42
	history := []domain.TrajectoryPoint{
		{SamplingDate: "2026-10-01", CycleDay: 30, ProbabilityPct: 0.05, RiskClass: 1},
		{SamplingDate: "2026-10-04", CycleDay: 33, ProbabilityPct: 1.20, RiskClass: 1},
		{SamplingDate: "2026-10-07", CycleDay: 36, ProbabilityPct: 6.50, RiskClass: 2},
		{SamplingDate: "2026-10-10", CycleDay: 39, ProbabilityPct: 18.40, RiskClass: 2},
		{SamplingDate: "2026-10-13", CycleDay: 42, ProbabilityPct: 34.80, RiskClass: 3},
	}

	report := EvaluateTrajectory("pond-c-test", history)

	if report.DataPoints != 5 {
		t.Fatalf("expected 5 data points, got %d", report.DataPoints)
	}

	if report.Slope <= 0 {
		t.Fatalf("expected positive rising slope, got %f", report.Slope)
	}

	if !report.TrajectoryAlert {
		t.Errorf("expected trajectory alert to trigger before Class 4, but was false")
	}

	if report.Trend != "RAPID_INCREASE" {
		t.Errorf("expected RAPID_INCREASE trend, got %s", report.Trend)
	}
}

func TestTrajectoryStable(t *testing.T) {
	history := []domain.TrajectoryPoint{
		{SamplingDate: "2026-10-01", CycleDay: 30, ProbabilityPct: 0.04, RiskClass: 1},
		{SamplingDate: "2026-10-04", CycleDay: 33, ProbabilityPct: 0.04, RiskClass: 1},
		{SamplingDate: "2026-10-07", CycleDay: 36, ProbabilityPct: 0.05, RiskClass: 1},
	}

	report := EvaluateTrajectory("pond-stable-test", history)

	if report.TrajectoryAlert {
		t.Errorf("stable pond should not trigger trajectory alert")
	}
	if report.Trend != "STABLE" {
		t.Errorf("expected STABLE trend, got %s", report.Trend)
	}
}

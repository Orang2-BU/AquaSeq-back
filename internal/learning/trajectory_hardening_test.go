package learning

import (
	"testing"
)

func TestForecastOnLogitZBoundaries(t *testing.T) {
	// Trajectory approaching 100% risk:
	// A linear fit on percentages would overshoot > 100%.
	// A logit Z fit must asymptotically approach but NEVER exceed 100%.
	points := []Sample{
		{Day: 0, ProbabilityPct: 70.0, Synthetic: true},
		{Day: 7, ProbabilityPct: 80.0, Synthetic: true},
		{Day: 14, ProbabilityPct: 90.0, Synthetic: true},
		{Day: 21, ProbabilityPct: 96.0, Synthetic: true},
	}

	f, err := ForecastRisk(points, 2) // 14 days ahead
	if err != nil {
		t.Fatalf("forecast failed: %v", err)
	}
	if f.PredictedProbabilityPct == nil {
		t.Fatal("expected predicted probability, got nil")
	}
	pred := *f.PredictedProbabilityPct
	if pred <= 96.0 || pred >= 100.0 {
		t.Fatalf("expected asymptotic prediction between 96%% and 100%%, got %.2f%%", pred)
	}
	if f.Upper == nil || *f.Upper > 100.0 || *f.Lower < 0.0 {
		t.Fatalf("CI violated probability bounds [0, 100]: lower=%v, upper=%v", f.Lower, f.Upper)
	}
	// Verify no arbitrary 1-MAD/35 confidence claimed
	// Uncalibrated forecast must not claim calibrated confidence
	if f.Confidence != 0.0 {
		t.Fatalf("uncalibrated forecast must not claim arbitrary confidence, got %v", f.Confidence)
	}
	// Verify disclaimer present
	if f.Disclaimer == "" {
		t.Fatal("forecast must contain non-clinical experimental disclaimer")
	}
}

func TestForecastPureLogitDistinctFromLabPredictions(t *testing.T) {
	// When LogitZ is explicitly supplied (e.g. pure logit calculation from SDRI),
	// ForecastRisk must fit on pure logit Z and expose PredictedLogitZ.
	z0 := -1.0
	z1 := 0.5
	z2 := 1.8
	z3 := 3.0
	points := []Sample{
		{Day: 0, LogitZ: &z0, ProbabilityPct: 99.0 /* raw lab outlier override */, Synthetic: true},
		{Day: 7, LogitZ: &z1, ProbabilityPct: 99.0, Synthetic: true},
		{Day: 14, LogitZ: &z2, ProbabilityPct: 99.0, Synthetic: true},
		{Day: 21, LogitZ: &z3, ProbabilityPct: 99.0, Synthetic: true},
	}

	f, err := ForecastRisk(points, 1)
	if err != nil {
		t.Fatalf("forecast failed: %v", err)
	}
	if f.PredictedLogitZ == nil {
		t.Fatal("pure-logit forecast must expose PredictedLogitZ")
	}
	if *f.PredictedLogitZ <= 3.0 {
		t.Fatalf("expected PredictedLogitZ > 3.0, got %v", *f.PredictedLogitZ)
	}
}

func TestForecastRobustElapsedDatesAndInsufficientDistinct(t *testing.T) {
	// Case 1: 4 samples but only 2 distinct dates -> insufficient data
	duplicateDates := []Sample{
		{Date: "2026-03-01", Day: 1, ProbabilityPct: 10.0, Synthetic: true},
		{Date: "2026-03-01", Day: 1, ProbabilityPct: 12.0, Synthetic: true},
		{Date: "2026-03-08", Day: 8, ProbabilityPct: 15.0, Synthetic: true},
		{Date: "2026-03-08", Day: 8, ProbabilityPct: 18.0, Synthetic: true},
	}
	f, err := ForecastRisk(duplicateDates, 1)
	if err != nil {
		t.Fatalf("expected nil error on insufficient data, got %v", err)
	}
	if f.Status != "INSUFFICIENT_DATA" || f.PredictedProbabilityPct != nil {
		t.Fatalf("expected INSUFFICIENT_DATA with nil predicted pct for <4 distinct dates: %+v", f)
	}

	// Case 2: 4 distinct ISO dates -> successful elapsed date calculation
	validDates := []Sample{
		{Date: "2026-03-01", ProbabilityPct: 10.0, Synthetic: true},
		{Date: "2026-03-08", ProbabilityPct: 15.0, Synthetic: true},
		{Date: "2026-03-15", ProbabilityPct: 22.0, Synthetic: true},
		{Date: "2026-03-22", ProbabilityPct: 30.0, Synthetic: true},
	}
	f2, err := ForecastRisk(validDates, 1)
	if err != nil {
		t.Fatalf("forecast with valid dates failed: %v", err)
	}
	if f2.Status != "OK" || f2.PredictedProbabilityPct == nil {
		t.Fatalf("expected OK forecast with dates, got: %+v", f2)
	}
	if *f2.PredictedProbabilityPct <= 30.0 || *f2.PredictedProbabilityPct >= 100.0 {
		t.Fatalf("expected prediction > 30%% and < 100%%, got %v", *f2.PredictedProbabilityPct)
	}
}

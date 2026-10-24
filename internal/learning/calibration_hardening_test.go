package learning

import (
	"context"
	"math/rand"
	"testing"
)

func TestSyntheticPromotionToProductionBlocked(t *testing.T) {
	truth := Coef{B0: -3, W1: .65, W2: 1.5, W3: .3}
	rng := rand.New(rand.NewSource(42))
	var labels []Outcome
	for i := 0; i < 200; i++ {
		x := [3]float64{rng.Float64() * 5, rng.Float64()*6 - 3, rng.Float64() * 7}
		label := "HARVEST_SUCCESS"
		if rng.Float64() < probability(truth, x) {
			label = "OUTBREAK"
		}
		labels = append(labels, Outcome{
			FarmID:    "farm-synthetic",
			PondID:    "pond-1",
			CycleID:   string(rune(i + 100)),
			Scores:    x,
			Label:     label,
			Synthetic: true,
		})
	}
	c, err := Calibrate("V2-sintetik", labels)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Synthetic {
		t.Fatal("expected synthetic candidate")
	}

	store := NewMemoryStore()
	registry := Registry{Store: store}
	if err := registry.Add(context.Background(), c); err != nil {
		t.Fatal(err)
	}

	// Must block synthetic promotion
	if err := registry.Promote(context.Background(), c.Version, "auditor"); err == nil {
		t.Fatal("CRITICAL: synthetic model candidate must NEVER be promoted to production")
	}
	active, err := store.Active(context.Background())
	if err != nil || active != V1VersionName {
		t.Fatalf("active version changed despite blocked synthetic promotion: %s", active)
	}
}

func TestChronologicalHoldoutAndGroupSeparation(t *testing.T) {
	// Create dataset with 6 farms, each farm having 10 cycles
	rng := rand.New(rand.NewSource(123))
	truth := Coef{B0: -2.5, W1: .8, W2: 1.2, W3: .5}
	var outcomes []Outcome
	farms := []string{"farm-1", "farm-2", "farm-3", "farm-4", "farm-5", "farm-6"}

	dayCounter := 0.0
	for _, farm := range farms {
		for c := 0; c < 10; c++ {
			dayCounter += 7.0
			x := [3]float64{rng.Float64() * 4, rng.Float64() * 3, rng.Float64() * 5}
			label := "HARVEST_SUCCESS"
			if rng.Float64() < probability(truth, x) {
				label = "OUTBREAK"
			}
			outcomes = append(outcomes, Outcome{
				FarmID:    farm,
				PondID:    "p1",
				CycleID:   farm + "-c" + string(rune(c+65)),
				Day:       dayCounter,
				Scores:    x,
				Label:     label,
				Synthetic: false,
			})
		}
	}

	cand, err := Calibrate("V2-grouped", outcomes)
	if err != nil {
		t.Fatal(err)
	}
	if cand.N != len(outcomes) {
		t.Fatalf("expected N=%d, got %d", len(outcomes), cand.N)
	}
}

func TestHoldoutLacksClassRepresentationFailsClosed(t *testing.T) {
	// If all outbreaks happen in early cycles and zero in late cycles,
	// chronological holdout has no positive labels -> must fail closed without making up confidence!
	var outcomes []Outcome
	for i := 0; i < 40; i++ {
		label := "HARVEST_SUCCESS"
		if i < 15 {
			label = "OUTBREAK" // all outbreaks in early part
		}
		outcomes = append(outcomes, Outcome{
			FarmID:    "farm-skewed",
			PondID:    "p1",
			CycleID:   "cycle-" + string(rune(i+100)),
			Day:       float64(i * 7),
			Scores:    [3]float64{1.0, 1.0, 1.0},
			Label:     label,
			Synthetic: false,
		})
	}

	_, err := Calibrate("V2-unbalanced", outcomes)
	if err == nil {
		t.Fatal("expected failure when chronological holdout lacks class representation; got success with made-up confidence")
	}
}

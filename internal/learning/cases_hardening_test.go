package learning

import (
	"testing"
)

func TestLegacyCaseRetrievalFailsClosed(t *testing.T) {
	_, err := new(CaseBase).Retrieve(map[string]float64{"tom": 40}, 1)
	if err == nil {
		t.Fatal("unscoped retrieval must fail closed")
	}
}

func TestCaseRequiresExplicitOwnershipAndProvenance(t *testing.T) {
	b := new(CaseBase)
	// 1. Missing provenance rejected
	err := b.Add(Case{
		ID:        "c-no-prov",
		FarmID:    "farm-A",
		PondID:    "pond-1",
		CycleID:   "cycle-1",
		Scope:     CaseScopePrivate,
		Features:  map[string]float64{"tom": 42.0},
		SDRIClass: 1,
		Action:    "Monitor",
		Outcome:   "HARVEST_SUCCESS",
	})
	if err == nil {
		t.Fatal("case without provenance must be rejected")
	}

	// 2. Unowned case without explicit public scope is rejected
	err = b.Add(Case{
		ID:         "c-anon",
		Provenance: "ANONYMIZED_SUBMISSION",
		Features:   map[string]float64{"tom": 42.0},
		SDRIClass:  1,
		Action:     "Monitor",
		Outcome:    "HARVEST_SUCCESS",
	})
	if err == nil {
		t.Fatal("unowned case accepted; absence of PII is not public authorization")
	}
}

func TestCaseTenantIsolationAndScopeSeparation(t *testing.T) {
	b := new(CaseBase)

	// Farm A private case
	if err := b.Add(Case{
		ID:         "case-farm-A",
		FarmID:     "farm-A",
		PondID:     "pond-1",
		CycleID:    "cycle-1",
		Scope:      CaseScopePrivate,
		Provenance: "FARM_A_CYCLE_2026",
		Features:   map[string]float64{"tom": 41.0, "do": 5.9},
		SDRIClass:  1,
		Action:     "Reduce Feed 10%",
		Outcome:    "HARVEST_SUCCESS",
		Synthetic:  false,
	}); err != nil {
		t.Fatalf("valid farm-A case rejected: %v", err)
	}

	// Farm B private case
	if err := b.Add(Case{
		ID:         "case-farm-B",
		FarmID:     "farm-B",
		PondID:     "pond-2",
		CycleID:    "cycle-1",
		Scope:      CaseScopePrivate,
		Provenance: "FARM_B_OUTBREAK_REPORT",
		Features:   map[string]float64{"tom": 41.0, "do": 5.9},
		SDRIClass:  4,
		Action:     "Emergency Harvest",
		Outcome:    "OUTBREAK",
		Synthetic:  false,
	}); err != nil {
		t.Fatalf("valid farm-B case rejected: %v", err)
	}

	// Curated public knowledge
	if err := b.Add(Case{
		ID:         "pub-gsi",
		Scope:      CaseScopePublic,
		Provenance: "GSI_BENCHMARK_SOP_2026",
		Features:   map[string]float64{"tom": 41.0, "do": 5.9},
		SDRIClass:  0,
		Action:     "Standard Protocol",
		Outcome:    "HARVEST_SUCCESS",
		Synthetic:  false,
	}); err != nil {
		t.Fatalf("valid public case rejected: %v", err)
	}

	// Farm A query CANNOT retrieve Farm B private case
	hits, err := b.RetrieveScoped("farm-A", false, map[string]float64{"tom": 41.0, "do": 6.0}, 10)
	if err != nil {
		t.Fatalf("scoped query failed: %v", err)
	}
	for _, h := range hits {
		if h.Case.FarmID == "farm-B" {
			t.Fatalf("CROSS-TENANT LEAKAGE: Farm A retrieved Farm B case: %+v", h.Case)
		}
	}
	if len(hits) != 1 || hits[0].Case.ID != "case-farm-A" {
		t.Fatalf("expected only case-farm-A in private scope, got: %+v", hits)
	}

	// Public query retrieves public knowledge
	pubHits, err := b.RetrievePublic("farm-A", false, map[string]float64{"tom": 41.0, "do": 6.0}, 10)
	if err != nil {
		t.Fatalf("public retrieval failed: %v", err)
	}
	if len(pubHits) != 1 || pubHits[0].Case.ID != "pub-gsi" {
		t.Fatalf("expected only pub-gsi in public retrieval, got: %+v", pubHits)
	}

	// Synthetic filtering: real query ignores synthetic cases
	if err := b.Add(Case{
		ID:         "case-farm-A-synth",
		FarmID:     "farm-A",
		PondID:     "pond-1",
		CycleID:    "cycle-synth",
		Scope:      CaseScopePrivate,
		Provenance: "SYNTHETIC_SIM",
		Features:   map[string]float64{"tom": 41.0, "do": 5.9},
		SDRIClass:  1,
		Action:     "Simulated Action",
		Outcome:    "HARVEST_SUCCESS",
		Synthetic:  true,
	}); err != nil {
		t.Fatalf("valid synthetic case rejected: %v", err)
	}
	realHits, err := b.RetrieveScoped("farm-A", false, map[string]float64{"tom": 41.0, "do": 6.0}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range realHits {
		if h.Synthetic {
			t.Fatalf("synthetic case leaked into real query: %+v", h)
		}
	}
}


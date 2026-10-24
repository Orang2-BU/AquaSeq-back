package learning

import (
 "context"
 "math"
 "math/rand"
 "testing"
)

func TestCalibrationSyntheticGovernance(t *testing.T) {
 truth:=Coef{B0:-3,W1:.65,W2:1.5,W3:.3}
 rng:=rand.New(rand.NewSource(42))
 var labels []Outcome
 for i:=0;i<8000;i++ {
  x:= [3]float64{rng.Float64()*5,rng.Float64()*6-3,rng.Float64()*7}
  label:="HARVEST_SUCCESS"; if rng.Float64()<probability(truth,x) {label="OUTBREAK"}
  labels=append(labels,Outcome{CycleID: string(rune(i+100)),Scores:x,Label:label,Synthetic:true})
 }
 c,err:=Calibrate("V2-sintetik",labels)
 if err!=nil {t.Fatal(err)}
 if c.Status!="kandidat" || !c.Synthetic || !c.Eligible {t.Fatalf("%+v",c)}
 for i,v:=range c.Coef.array() {if math.Abs(v-truth.array()[i])>.35 {t.Fatalf("fit %+v truth %+v",c.Coef,truth)}}
 t.Logf("sintetik only: coefficients=%+v holdout=%+v V1=%+v",c.Coef,c.Metrics.Holdout,c.Metrics.V1)
 store:=NewMemoryStore(); registry:=Registry{Store:store}
 if err=registry.Add(context.Background(),c);err!=nil {t.Fatal(err)}
 active,err:=store.Active(context.Background());if err!=nil || active!="V1-GSI" {t.Fatal("automatic activation",active,err)}
 if err = registry.Promote(context.Background(), c.Version, ""); err == nil {
 	t.Fatal("anonymous approval")
 }
 // Synthetic model candidates can NEVER be promoted to production
 if err = registry.Promote(context.Background(), c.Version, "auditor"); err == nil {
 	t.Fatal("synthetic candidate promoted to active production")
 }
 if active, _ = store.Active(context.Background()); active != "V1-GSI" {
 	t.Fatalf("active version changed on synthetic candidate: %s", active)
 }

 if _,err=Calibrate("small",labels[:10]);err==nil {t.Fatal("small N accepted")}
 if _,err=Calibrate("V1-GSI",labels);err==nil {t.Fatal("V1 overwritten")}
 rejected:=c;rejected.Version="V3";rejected.Eligible=false
 if err=registry.Add(context.Background(),rejected);err!=nil {t.Fatal(err)}
 if err=registry.Promote(context.Background(),"V3","auditor");err==nil {t.Fatal("failed gate promoted")}
}

func TestCaseBaseRetrieval(t *testing.T) {
	b := new(CaseBase)
	for _, c := range []Case{
		{ID: "near", FarmID: "farm-1", PondID: "p-1", CycleID: "c-1", Scope: CaseScopePrivate, Provenance: "TEST_REPRO", Features: map[string]float64{"tom": 42, "do": 6}, SDRIClass: 0, Action: "Pantau", Outcome: "HARVEST_SUCCESS", Synthetic: true},
		{ID: "far", FarmID: "farm-1", PondID: "p-1", CycleID: "c-1", Scope: CaseScopePrivate, Provenance: "TEST_REPRO", Features: map[string]float64{"tom": 120, "do": 3}, SDRIClass: 4, Action: "Isolasi", Outcome: "OUTBREAK", Synthetic: true},
	} {
		if err := b.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	// Legacy Retrieve fails closed
	if _, err := b.Retrieve(map[string]float64{"tom": 40, "do": 6}, 1); err == nil {
		t.Fatal("legacy unscoped retrieve must fail closed")
	}
	// Secure scoped retrieval
	hits, err := b.RetrieveScoped("farm-1", true, map[string]float64{"tom": 40, "do": 6}, 1)
	if err != nil || len(hits) != 1 || hits[0].Case.ID != "near" || !hits[0].Synthetic {
		t.Fatalf("%+v %v", hits, err)
	}
	if _, err = b.RetrieveScoped("farm-1", true, map[string]float64{"unknown": 1}, 1); err == nil {
		t.Fatal("unmatched features accepted")
	}
}

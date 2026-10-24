package learning

import (
 "math"
 "testing"
)

func syntheticKolamC() []Sample {
 var points []Sample
 for i:=0;i<12;i++ {
  drift:=math.Max(0,float64(i-4))
  z:=-6.5+1.2*(1+drift*.45)+.85*(-2+drift*.4)+.7*(1+drift*.8)
  p:=100/(1+math.Exp(-z)); class:=0
  if p>=60 {class=4} else if p>=25 {class=2} else if p>=5 {class=1}
  points=append(points,Sample{Day:float64(i*7),ProbabilityPct:p,RiskClass:class,Synthetic:true,Features:map[string]float64{"amoa":100-drift*8+float64(i%2),"tom_mg_l":40+drift*5+float64(i%2),"do_mg_l":6+float64(i%2)*.1+math.Pow(-1,float64(i))*drift*.15}})
 }
 return points
}

func TestSyntheticKolamCAnomalyBeforeClassFour(t *testing.T) {
 points:=syntheticKolamC()
 report,err:=Analyze("C","Kolam C sintetik",points,4)
 if err!=nil {t.Fatal(err)}
 if !report.Synthetic || report.EarlierThanClassSteps==nil || *report.EarlierThanClassSteps<1 {t.Fatalf("lead time missing: %+v",report)}
 t.Logf("Kolam C sintetik: anomaly step=%d; class 4 step=%d; lead=%d sampling steps",*report.FirstAnomalyStep,*report.ClassFourStep,*report.EarlierThanClassSteps)
 prefix,err:=Analyze("C","Kolam C sintetik",points[:*report.FirstAnomalyStep+1],4)
 if err!=nil || !prefix.Flagged || prefix.EarlierThanClassSteps!=nil {t.Fatalf("online report: %+v %v",prefix,err)}
 policy:=Escalate(2,prefix)
 if policy.SDRIClass!=2 || policy.Handling!="waspada tinggi" || !policy.Synthetic {t.Fatalf("class overwritten: %+v",policy)}
}

func TestForecastInsufficientAndRobust(t *testing.T) {
 points:=syntheticKolamC()
 f,err:=ForecastRisk(points[:3],1)
 if err!=nil || f.Status!="INSUFFICIENT_DATA" || f.PredictedProbabilityPct!=nil || !f.Synthetic {t.Fatalf("fabricated forecast: %+v %v",f,err)}
 f,err=ForecastRisk(points[:8],2)
 if err!=nil || f.Horizon!=14 || f.PredictedProbabilityPct==nil || *f.Lower>*f.PredictedProbabilityPct || *f.Upper<*f.PredictedProbabilityPct {t.Fatalf("forecast: %+v %v",f,err)}
 if _,err=ForecastRisk(points,3);err==nil {t.Fatal("unsupported horizon")}
 bad:=append([]Sample(nil),points...); bad[2].Day=bad[1].Day
 if _,err=Analyze("C","sintetik",bad,4);err==nil {t.Fatal("duplicate day accepted")}
 bad=append([]Sample(nil),points...);bad[2].ProbabilityPct=math.NaN()
 if _,err=ForecastRisk(bad,1);err==nil {t.Fatal("NaN accepted")}
}

func TestClusterPropagation(t *testing.T) {
 own:=ClusterRisk{PondID:"a",Cluster:"x",ProbabilityPct:10}
 r,err:=Propagate(own,[]ClusterRisk{{PondID:"b",Cluster:"x",ProbabilityPct:80,Synthetic:true},{PondID:"c",Cluster:"y",ProbabilityPct:100}})
 if err!=nil || r.OriginalProbabilityPct!=10 || r.ContextualProbabilityPct<=10 || r.Neighbors!=1 || !r.Synthetic {t.Fatalf("%+v %v",r,err)}
}

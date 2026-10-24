package handler

import (
 "context"
 "net/http/httptest"
 "os"
 "testing"
 "time"

 "aquaseq-back/internal/auth"
 "aquaseq-back/internal/repository"
)

type mobileFixture struct {
 store repository.WebStore
 server *httptest.Server
 owner, tech, outsider, farm, otherFarm, pond, unassigned, otherPond string
 ownerToken, techToken, outsiderToken string
}

func mobileSetup(t *testing.T, store repository.WebStore) mobileFixture {
 t.Helper()
 f := mobileFixture{store:store, owner:generateUUID(), tech:generateUUID(), outsider:generateUUID(), farm:generateUUID(), otherFarm:generateUUID(), pond:generateUUID(), unassigned:generateUUID(), otherPond:generateUUID()}
 a, err := auth.New("mobile-test-signing-secret-at-least-16")
 if err != nil {t.Fatal(err)}
 f.ownerToken,_=a.Access(f.owner); f.techToken,_=a.Access(f.tech); f.outsiderToken,_=a.Access(f.outsider)
 err = store.Write(context.Background(), func(tx tx) error {
  for _, id := range []string{f.owner,f.tech,f.outsider} {
   if e:=tx.Put("users",rec{"id":id,"email":id+"@mobile.test","full_name":"Mobile "+id,"password_hash":"test"}); e!=nil {return e}
  }
  for _, r:=range []rec{{"id":f.farm,"owner_id":f.owner,"name":"Tambak Satu","regency":"Banyuwangi"},{"id":f.otherFarm,"owner_id":f.outsider,"name":"Tambak Lain","regency":"Banyuwangi"}} {if e:=tx.Put("farms",r);e!=nil{return e}}
  if e:=tx.Put("memberships",rec{"id":generateUUID(),"farm_id":f.farm,"user_id":f.tech,"active":true,"role":"teknisi"});e!=nil{return e}
  for _, r:=range []rec{{"id":f.pond,"farm_id":f.farm,"pond_code":"A3","assignee_id":f.tech},{"id":f.unassigned,"farm_id":f.farm,"pond_code":"B1"},{"id":f.otherPond,"farm_id":f.otherFarm,"pond_code":"C1","assignee_id":f.outsider}} {
   r["area_sqm"]=2500; r["stocked_at"]=time.Now().AddDate(0,0,-42).Format("2006-01-02");r["current_cycle"]=1
   if e:=tx.Put("ponds",r);e!=nil{return e}
  }
  return nil
 })
 if err!=nil {t.Fatal(err)}
 f.server=httptest.NewServer(NewLiveHandler(store,Config{Auth:a,FilesDir:t.TempDir()}).Routes())
 t.Cleanup(f.server.Close)
 return f
}


func TestMobileFarmTenantScope(t *testing.T) {
 f := mobileSetup(t,repository.NewMemoryStore())
 code, body := doJSON(t,"GET",f.server.URL+"/api/v1/mobile/farm?farm_id="+f.farm,f.techToken,nil)
 if code != 200 { t.Fatalf("farm: %d %v",code,body) }
 data := object(body["data"])
 ponds := data["ponds"].([]any)
 if len(ponds)!=1 || str(object(ponds[0]),"id")!=f.pond { t.Fatalf("pond access leaked: %v",ponds) }
 p := object(ponds[0])
 for _, key := range []string{"srPercent","stockedCount","abwGram"} { if p[key]!=nil {t.Fatalf("fabricated %s: %v",key,p[key])} }
 if num(p,"areaM2")!=2500 || num(p,"docDays")<10 || data["feedTodayKg"]!=nil {t.Fatal(data)}
 for _, key := range []string{"readings","riskScores","recommendations","tasks"} { if len(data[key].([]any))!=0 {t.Fatalf("expected empty %s: %v",key,data[key])} }
 code,_=doJSON(t,"GET",f.server.URL+"/api/v1/mobile/farm?farm_id="+f.otherFarm,f.techToken,nil)
 if code!=403 {t.Fatalf("cross farm %d",code)}
 code,_=doJSON(t,"GET",f.server.URL+"/api/v1/mobile/farm","",nil)
 if code!=401 {t.Fatalf("auth %d",code)}
}

func TestMobileReadingTaskSchedule(t *testing.T) {
 f:=mobileSetup(t,repository.NewMemoryStore())
 mobileExercise(t,f)
}

func TestMobilePostgres(t *testing.T) {
 url := os.Getenv("DATABASE_URL_TEST")
 if url == "" {
  t.Skip("DATABASE_URL_TEST tidak diatur")
 }
 store, e := repository.OpenPostgres(context.Background(), url, 4)
 if e != nil {
  t.Fatal(e)
 }
 defer store.Close()
 f := mobileSetup(t, store)
 mobileExercise(t, f)
}


func mobileExercise(t *testing.T,f mobileFixture) {
 t.Helper()
 base:=f.server.URL+"/api/v1/mobile"
 reading:=rec{"id":"local-1","pondId":f.pond,"at":time.Now().Format(time.RFC3339),"by":"Spoof","values":rec{"WarnaAir":5,"UdangMenyala":2}}
 for i:=0;i<2;i++ {code,out:=doJSON(t,"POST",base+"/readings",f.techToken,reading);if code!=200 {t.Fatal(code,out)}}
 code,_:=doJSON(t,"POST",base+"/readings",f.outsiderToken,reading);if code!=403 {t.Fatal("cross reading",code)}
 for _,v:=range []any{rec{"Bogus":0},rec{"WarnaAir":6},rec{"InsangHitam":"1"},rec{"EkorMerah":true},rec{"LapisanAlga":2.5}} {
  b:=clone(reading);b["values"]=v;code,_=doJSON(t,"POST",base+"/readings",f.techToken,b);if code!=400 {t.Fatal("values",v,code)}
 }
 sid:=generateUUID()
 err:=f.store.Write(context.Background(),func(tx tx)error{
  if e:=tx.Put("samples",rec{"id":sid,"pond_id":f.pond,"sample_code":"MOBILE-QA","sampling_date":"2026-10-24","cycle_day":42,"meta":map[string]any{"cycle_number":1}});e!=nil{return e}
  return tx.Put("sdri_results",rec{"id":generateUUID(),"sample_id":sid,"status":"VALID","probability_pct":82,"risk_class":3,"risk_level":"Waspada Tinggi","recommendation":"Pantau kolam sesuai hasil laboratorium"})
 });if err!=nil {t.Fatal(err)}
 code,_=doJSON(t,"POST",base+"/recommendations/"+sid+"/schedule",f.outsiderToken,nil);if code!=403 {t.Fatal("cross schedule",code)}
 taskID:=""
 for i:=0;i<2;i++ {code,out:=doJSON(t,"POST",base+"/recommendations/"+sid+"/schedule",f.ownerToken,nil);if code!=200 {t.Fatal(code,out)};id:=str(object(out["data"]),"task_id");if taskID!=""&&taskID!=id {t.Fatal("duplicate schedule")};taskID=id}
 code,_=doJSON(t,"PATCH",base+"/tasks/"+taskID,f.outsiderToken,rec{"done":true});if code!=403 {t.Fatal("cross task",code)}
 code,_=doJSON(t,"PATCH",base+"/tasks/"+taskID,f.ownerToken,rec{"done":true});if code!=403 {t.Fatal("owner must not report",code)}
 for i:=0;i<2;i++ {code,out:=doJSON(t,"PATCH",base+"/tasks/"+taskID,f.techToken,rec{"done":true});if code!=200 || str(object(out["data"]),"status")!="dilaporkan" {t.Fatal(code,out)}}
 code,_=doJSON(t,"PATCH",base+"/tasks/"+taskID,f.techToken,rec{"done":false});if code!=409 {t.Fatal("undo",code)}
 err=f.store.Read(context.Background(),func(tx tx)error{
  for table,want:=range map[string]int{"mobile_readings":1,"mobile_schedules":1,"tasks":1,"task_reports":1,"water_logs":0,"cycle_outcomes":0} {
   rows,e:=tx.List(table);if e!=nil{return e};if len(rows)!=want {t.Fatalf("%s: %d != %d",table,len(rows),want)}
  };return nil
 });if err!=nil {t.Fatal(err)}
 code,_=doJSON(t,"POST",base+"/ai/mitigasi",f.outsiderToken,rec{"pond_id":f.pond});if code!=403 {t.Fatal("cross mitigation",code)}
 code,_=doJSON(t,"POST",base+"/ai/mitigasi",f.techToken,rec{"pond_id":f.unassigned});if code!=403 {t.Fatal("unassigned mitigation",code)}
 code,_=doJSON(t,"POST",base+"/ai/mitigasi",f.ownerToken,rec{"pond_id":f.unassigned});if code!=422 {t.Fatal("missing mitigation",code)}
 code,_=doJSON(t,"POST",base+"/ai/mitigasi",f.techToken,rec{"pond_id":f.pond,"severity":"KRITIS"});if code!=400 {t.Fatal("untrusted mitigation fields",code)}
}

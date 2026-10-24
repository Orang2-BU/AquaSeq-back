package handler

import("testing";"time")
func TestRejectMalformedInputs(t *testing.T){
 srv,_,_:=newTestLiveServer(t);_,out:=doJSON(t,"POST",srv.URL+"/api/v1/auth/demo","",rec{"role":"pemilik"});token:=str(object(out["data"]),"access_token")
 for _,tc:=range []struct{path string;body rec}{
 {"/farms/"+farmJember+"/ponds",rec{"pond_code":"BAD","system":"liar"}},
 {"/farms/"+farmJember+"/ponds",rec{"pond_code":"BAD","system":"intensif","assignee_id":"00000000-0000-0000-0000-000000000999"}},
 {"/farms/"+farmJember+"/invitations",rec{"email":"valid@example.test","pond_ids":[]string{"00000000-0000-0000-0000-000000000201"}}},
 {"/samples",rec{"pond_id":"00000000-0000-0000-0000-000000000101","sample_code":"S","sampling_date":"bad-date","cycle_day":1,"features":rec{"ct_ppc":20}}},
 {"/samples",rec{"pond_id":"00000000-0000-0000-0000-000000000101","sample_code":"S","sampling_date":"2026-10-24","cycle_day":1,"features":rec{"ct_ppc":20,"tan_mg_l":"20"}}},
 }{code,out:=doJSON(t,"POST",srv.URL+"/api/v1"+tc.path,token,tc.body);if code!=400{t.Errorf("%s: %d %v",tc.path,code,out)}}
 code,_:=doJSON(t,"PATCH",srv.URL+"/api/v1/farms/"+farmJember+"/members/"+demoTechnician,token,rec{"active":"true"});if code!=400{t.Error("active wrong type",code)}
 _,tech:=doJSON(t,"POST",srv.URL+"/api/v1/auth/demo","",rec{"role":"teknisi"});tt:=str(object(tech["data"]),"access_token");code,_=doJSON(t,"POST",srv.URL+"/api/v1/farms",tt,rec{"name":"Kolam","regency":"Jember"});if code!=403{t.Error("technician created farm",code)}
 _ = time.Now()
}
func TestFarmMutationContract(t *testing.T){
 srv,_,_:=newTestLiveServer(t);_,out:=doJSON(t,"POST",srv.URL+"/api/v1/auth/demo","",rec{"role":"pemilik"});token:=str(object(out["data"]),"access_token")
 for _,method:=range []string{"PATCH","POST"}{path:="/farms/"+farmJember;var body any=rec{"address":"Puger"};if method=="POST"{path+="/archive";body=nil};code,out:=doJSON(t,method,srv.URL+"/api/v1"+path,token,body);if code!=200{t.Fatal(code,out)};d:=object(out["data"]);for _,key:=range []string{"pond_count","technician_count","at_risk_count"}{if _,ok:=d[key].(float64);!ok{t.Errorf("%s missing %s",method,key)}}}
}

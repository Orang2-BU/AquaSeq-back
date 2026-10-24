package handler

import (
	"aquaseq-back/internal/repository"
	"context"
	"net/http"
	"testing"
	"time"
)

func TestTechnicianScopeAndRefreshReplay(t *testing.T) {
	srv, store, _ := newTestLiveServer(t)
	_, owner := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", rec{"role": "pemilik"})
	ot := str(object(owner["data"]), "access_token")
	_, tech := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", rec{"role": "teknisi"})
	td := object(tech["data"])
	tt := str(td, "access_token")
	old := str(td, "refresh_token")
	code, out := doJSON(t, "POST", srv.URL+"/api/v1/auth/refresh", "", rec{"refresh_token": old})
	if code != 200 {
		t.Fatal(code, out)
	}
	code, _ = doJSON(t, "POST", srv.URL+"/api/v1/auth/refresh", "", rec{"refresh_token": old})
	if code != 401 {
		t.Fatal("refresh replay", code)
	}
	if e := store.Write(context.Background(), func(tx repository.WebTx) error {
		p, e := tx.Get("ponds", "00000000-0000-0000-0000-000000000104")
		if e != nil {
			return e
		}
		p["assignee_id"] = nil
		return tx.Put("ponds", p)
	}); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/ponds/00000000-0000-0000-0000-000000000104", "/ponds/00000000-0000-0000-0000-000000000104/samples", "/batches?farm_id=" + farmJember, "/farms/" + farmJember + "/members"} {
		code, _ = doJSON(t, "GET", srv.URL+"/api/v1"+path, tt, nil)
		if code != 403 {
			t.Errorf("%s: %d", path, code)
		}
	}
	code, _ = doJSON(t, "POST", srv.URL+"/api/v1/sdri/calculate", "", rec{"ct_ppc": 20, "s_qpcr": 1, "s_bio": 1, "s_chem": 1})
	if code != 401 {
		t.Fatal("calculate must require auth", code)
	}
	_, logs := doJSON(t, "GET", srv.URL+"/api/v1/ponds/00000000-0000-0000-0000-000000000101/water-logs", ot, nil)
	items := object(logs["data"])["items"].([]any)
	id := str(object(items[0]), "id")
	code, _ = doJSON(t, "PATCH", srv.URL+"/api/v1/water-logs/"+id, tt, rec{"temp_c": 55})
	if code != http.StatusBadRequest {
		t.Fatal("patch bypasses ranges", code)
	}
	code, _ = doJSON(t, "POST", srv.URL+"/api/v1/ponds/00000000-0000-0000-0000-000000000101/water-logs", ot, rec{"measured_at": time.Now().Format(time.RFC3339), "temp_c": 30})
	if code != 400 {
		t.Fatal("missing water chemistry treated as zero", code)
	}
}
func TestBatchAtomicValidation(t *testing.T) {
	srv, store, _ := newTestLiveServer(t)
	_, owner := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", rec{"role": "pemilik"})
	token := str(object(owner["data"]), "access_token")
	before := 0
	_ = store.Read(context.Background(), func(t repository.WebTx) error { s, e := t.List("samples"); before = len(s); return e })
	code, _ := doJSON(t, "POST", srv.URL+"/api/v1/samples/batch", token, rec{"run": rec{"lab_name": "Data simulasi", "instrument": "Panel jejak penyakit", "tested_at": time.Now().Format(time.RFC3339), "file_name": "simulasi.csv"}, "samples": []rec{{"pond_id": "00000000-0000-0000-0000-000000000101", "sample_code": "UJI", "sampling_date": "2026-10-24", "cycle_day": 35, "features": rec{"ct_ppc": 20}}, {"pond_id": "tidak-ada"}}})
	if code != 400 {
		t.Fatal("batch should fail", code)
	}
	_ = store.Read(context.Background(), func(t repository.WebTx) error {
		s, e := t.List("samples")
		if len(s) != before {
			panic("batch partial persisted")
		}
		return e
	})
}

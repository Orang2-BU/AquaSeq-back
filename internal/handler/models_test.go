package handler

import (
	"net/http"
	"testing"

	"aquaseq-back/internal/sdri"
)

func TestModelsReflectFrozenRuntime(t *testing.T) {
	srv, _, _ := newTestLiveServer(t)
	code, _ := doJSON(t, "GET", srv.URL+"/api/v1/models", "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous model read: %d", code)
	}
	_, login := doJSON(t, "POST", srv.URL+"/api/v1/auth/demo", "", rec{"role": "pemilik"})
	token := str(object(login["data"]), "access_token")
	code, response := doJSON(t, "GET", srv.URL+"/api/v1/models", token, nil)
	if code != http.StatusOK {
		t.Fatalf("models: %d %v", code, response)
	}
	items, ok := object(response["data"])["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected actual frozen runtime only, got %v", response)
	}
	model := object(items[0])
	if model["version"] != sdri.ModelVersion || model["status"] != "aktif" || num(model, "n_labels") != 0 {
		t.Fatalf("runtime model mismatch: %v", model)
	}
	metrics := object(model["metrics"])
	if metrics["validation"] != "BELUM_PILOT" || metrics["auc"] != nil || metrics["accuracy"] != nil {
		t.Fatalf("unvalidated model cannot claim field metrics: %v", metrics)
	}
}

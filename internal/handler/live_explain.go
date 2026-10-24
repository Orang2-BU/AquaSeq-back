package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"aquaseq-back/internal/microbiome"
	"aquaseq-back/internal/science"
	"aquaseq-back/internal/sdri"
)

// Penjelasan ilmiah per sampel (internal/science), disimpan supaya LLM tidak dipanggil ulang tiap halaman dibuka.
const explanationTable = "sample_explanations"

func mitigationTexts(v any) []string {
	b, _ := json.Marshal(v)
	var raw []any
	_ = json.Unmarshal(b, &raw)
	out := []string{}
	for _, x := range raw {
		switch m := x.(type) {
		case string:
			out = append(out, m)
		case map[string]any:
			if a, ok := m["action"].(string); ok {
				out = append(out, a)
			}
		}
	}
	return out
}

// sampleFacts menyusun fakta mesin untuk satu sampel; hanya angka di sini yang boleh dipakai penulis.
func sampleFacts(t tx, s rec) science.Facts {
	sid := str(s, "id")
	res, _ := first(t, "sdri_results", "sample_id", sid)
	featRec, _ := first(t, "sample_features", "sample_id", sid)
	assess := sdri.Assess(parseFeatures(featRec))
	f := science.Facts{
		PondCode:          str(pondRef(t, str(s, "pond_id")), "pond_code"),
		SampleCode:        str(s, "sample_code"),
		CycleDay:          int(num(s, "cycle_day")),
		Status:            str(res, "status"),
		RiskClass:         int(num(res, "risk_class")),
		RiskLevel:         str(res, "risk_level"),
		ProbabilityPct:    num(res, "probability_pct"),
		ModelVersion:      str(res, "model_version"),
		PredictedDisease:  str(res, "predicted_disease"),
		DiseaseName:       str(res, "disease_name"),
		Features:          assess.Features,
		MitigationActions: mitigationTexts(res["mitigation_actions"]),
	}
	if mb, e := first(t, microbiomeTable, "sample_id", sid); e == nil {
		f.Microbiome = metricsOf(mb["metrics"])
	}
	c := assess.CopiesPerML
	f.Markers = microbiome.SelectMarkers(microbiome.PanelInput{Metrics: f.Microbiome, AHPNDCopy: deref(c["ahpnd"]), WSSVCopy: deref(c["wssv"]), EHPCopy: deref(c["ehp"])})
	return f
}

func (h *LiveHandler) handleGetExplanation(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		sid := r.PathValue("id")
		if _, _, e := h.sampleAccess(t, getUserID(r), sid, false); e != nil {
			return nil, e
		}
		var payload any
		if row, e := first(t, explanationTable, "sample_id", sid); e == nil {
			payload = row["payload"]
		}
		return rec{"sample_id": sid, "explanation": payload, "ai": h.cfg.Explainer.Status()}, nil
	})
}

func (h *LiveHandler) handleCreateExplanation(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	user := getUserID(r)
	var facts science.Facts
	// Fakta dibaca dalam transaksi singkat; panggilan LLM dilakukan di luar transaksi.
	if e := h.store.Read(r.Context(), func(t tx) error {
		s, _, e := h.sampleAccess(t, user, sid, true)
		if e != nil {
			return e
		}
		facts = sampleFacts(t, s)
		return nil
	}); e != nil {
		h.fail(w, e)
		return
	}
	exp := h.cfg.Explainer.Explain(context.WithoutCancel(r.Context()), facts)
	var out any
	if e := h.store.Write(r.Context(), func(t tx) error {
		id := generateUUID()
		if old, e := first(t, explanationTable, "sample_id", sid); e == nil {
			id = str(old, "id")
		}
		payload := map[string]any(object(exp))
		out = rec{"sample_id": sid, "explanation": payload, "ai": h.cfg.Explainer.Status()}
		return t.Put(explanationTable, rec{"id": id, "sample_id": sid, "payload": payload, "created_at": now()})
	}); e != nil {
		h.fail(w, e)
		return
	}
	writeSuccess(w, 200, out)
}

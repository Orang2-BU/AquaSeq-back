package handler

import (
	"net/http"

	"aquaseq-back/internal/sdri"
)

func (h *LiveHandler) handleGetModels(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		items := []rec{
			{
				"version":     sdri.ModelVersion,
				"status":      "aktif",
				"metrics":     rec{"validation": "BELUM_PILOT"},
				"n_labels":    0,
				"synthetic":   false,
				"trained_at":  "2026-10-24T00:00:00Z",
				"promoted_at": nil,
				"promoted_by": nil,
				"note":        "Model deterministik V1-GSI beku berdasarkan literatur acuan laboratorium. Belum divalidasi pilot lapangan.",
			},
		}
		return rec{"items": items}, nil
	})
}

func (h *LiveHandler) handlePromoteModel(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		ver := r.PathValue("version")
		if ver == sdri.ModelVersion {
			return nil, invalid("version", "Model V1-GSI adalah model bawaan yang dibekukan.")
		}
		note := str(b, "note")
		if note == "" {
			return nil, invalid("note", "Catatan promosi model wajib diisi.")
		}
		return nil, invalid("version", "Kandidat model belum memenuhi kualifikasi data hasil panen/wabah nyata.")
	})
}

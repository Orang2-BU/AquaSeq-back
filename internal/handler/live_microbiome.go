package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"aquaseq-back/internal/microbiome"
	"aquaseq-back/internal/sdri"
)

// Tabel kelimpahan taksa per sampel (hasil klasifikasi 16S lab mitra) dan metrik komunitasnya.
const microbiomeTable = "sample_microbiome"

// sampleAccess memeriksa hak atas sampel lewat kolamnya; ownerOnly untuk aksi tulis pemilik.
func (h *LiveHandler) sampleAccess(t tx, user, sid string, ownerOnly bool) (sample, pond rec, err error) {
	s, e := t.Get("samples", sid)
	if e != nil {
		return nil, nil, e
	}
	p, e := h.pondAccess(t, user, str(s, "pond_id"), ownerOnly)
	if e != nil {
		return nil, nil, e
	}
	return s, p, nil
}

func taxaOf(v any) []microbiome.Taxon {
	b, _ := json.Marshal(v)
	out := []microbiome.Taxon{}
	_ = json.Unmarshal(b, &out)
	return out
}

func metricsOf(v any) *microbiome.Metrics {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	var m microbiome.Metrics
	if json.Unmarshal(b, &m) != nil || m.Version == "" {
		return nil
	}
	return &m
}

// cycleSamples mengembalikan sampel kolam pada siklus yang sama, urut umur kultur lalu waktu dibuat.
func cycleSamples(t tx, pondID string, cycle int) ([]rec, error) {
	all, e := find(t, "samples", "pond_id", pondID)
	if e != nil {
		return nil, e
	}
	out := []rec{}
	for _, s := range all {
		if extractCycleNumber(s) == cycle {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if num(out[i], "cycle_day") == num(out[j], "cycle_day") {
			return str(out[i], "created_at") < str(out[j], "created_at")
		}
		return num(out[i], "cycle_day") < num(out[j], "cycle_day")
	})
	return out, nil
}

// baselineFor memilih sampel acuan: sampel kelimpahan VALID paling awal pada siklus yang sama, selain sampel ini.
func baselineFor(t tx, sample rec) ([]microbiome.Taxon, string) {
	smps, e := cycleSamples(t, str(sample, "pond_id"), extractCycleNumber(sample))
	if e != nil {
		return nil, ""
	}
	for _, s := range smps {
		if str(s, "id") == str(sample, "id") || num(s, "cycle_day") > num(sample, "cycle_day") {
			continue
		}
		mb, e := first(t, microbiomeTable, "sample_id", str(s, "id"))
		if e != nil {
			continue
		}
		if m := metricsOf(mb["metrics"]); m != nil && m.Status == "VALID" {
			return taxaOf(mb["taxa"]), str(s, "id")
		}
	}
	return nil, ""
}

func (h *LiveHandler) handleCreateMicrobiome(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	taxa, pe := microbiome.ParseCSV(strings.NewReader(str(b, "csv")))
	if pe != nil {
		h.fail(w, invalid("csv", pe.Error()))
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		sid := r.PathValue("id")
		s, _, e := h.sampleAccess(t, getUserID(r), sid, true)
		if e != nil {
			return nil, e
		}
		base, baseID := baselineFor(t, s)
		m := microbiome.Assess(taxa, base, baseID)
		id := generateUUID()
		if old, e := first(t, microbiomeTable, "sample_id", sid); e == nil {
			id = str(old, "id")
		}
		taxaRows := make([]any, 0, len(taxa))
		for _, x := range taxa {
			taxaRows = append(taxaRows, map[string]any{"taxon": x.Taxon, "genus": x.Genus, "reads": x.Reads})
		}
		source := strings.TrimSpace(str(b, "source"))
		if source == "" {
			source = "Lab mitra (16S)"
		}
		if e := t.Put(microbiomeTable, rec{
			"id":         id,
			"sample_id":  sid,
			"source":     source,
			"taxa":       taxaRows,
			"metrics":    map[string]any(object(m)),
			"created_at": now(),
		}); e != nil {
			return nil, e
		}
		return rec{"sample_id": sid, "microbiome": m}, nil
	})
}

func (h *LiveHandler) handleGetPondMicrobiome(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		pid := r.PathValue("id")
		p, e := h.pondAccess(t, getUserID(r), pid, false)
		if e != nil {
			return nil, e
		}
		smps, e := cycleSamples(t, pid, int(num(p, "current_cycle")))
		if e != nil {
			return nil, e
		}
		series := []rec{}
		var latest *microbiome.Metrics
		in := microbiome.PanelInput{}
		for _, s := range smps {
			if res, e := first(t, "sdri_results", "sample_id", str(s, "id")); e == nil && str(res, "status") == "VALID" {
				if f, e := first(t, "sample_features", "sample_id", str(s, "id")); e == nil {
					c := sdri.Assess(parseFeatures(f)).CopiesPerML
					in.AHPNDCopy, in.WSSVCopy, in.EHPCopy = deref(c["ahpnd"]), deref(c["wssv"]), deref(c["ehp"])
				}
			}
			mb, e := first(t, microbiomeTable, "sample_id", str(s, "id"))
			if e != nil {
				continue
			}
			m := metricsOf(mb["metrics"])
			if m == nil {
				continue
			}
			latest = m
			series = append(series, rec{
				"sample_id":     s["id"],
				"sample_code":   s["sample_code"],
				"sampling_date": s["sampling_date"],
				"cycle_day":     int(num(s, "cycle_day")),
				"source":        mb["source"],
				"uploaded_at":   mb["created_at"],
				"status":        m.Status,
				"shannon":       m.Shannon,
				"chao1":         m.Chao1,
				"vibrio_pct":    m.VibrioPct,
				"bray_curtis":   m.BrayCurtis,
			})
		}
		in.Metrics = latest
		return rec{
			"pond":    pondRef(t, pid),
			"version": microbiome.Version,
			"series":  series,
			"latest":  latest,
			"markers": microbiome.SelectMarkers(in),
		}, nil
	})
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

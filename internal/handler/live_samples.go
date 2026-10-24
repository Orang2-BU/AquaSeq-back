package handler

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/sdri"
	"aquaseq-back/internal/trajectory"
)

func parseFeatures(raw any) domain.SampleFeatures {
	fMap := object(raw)
	numPtr := func(k string) *float64 {
		if v, ok := fMap[k]; ok && v != nil {
			n := num(fMap, k)
			return &n
		}
		return nil
	}
	return domain.SampleFeatures{
		CTAHPND:     numPtr("ct_ahpnd"),
		CTWSSV:      numPtr("ct_wssv"),
		CTEHP:       numPtr("ct_ehp"),
		CTamoA:      numPtr("ct_amoa"),
		CT16S:       numPtr("ct_16s"),
		CTPPC:       num(fMap, "ct_ppc"),
		DOMgL:       numPtr("do_mg_l"),
		TOMMgL:      numPtr("tom_mg_l"),
		PH:          numPtr("ph"),
		SalinityPPT: numPtr("salinity_ppt"),
		TempC:       numPtr("temp_c"),
		TANMgL:      numPtr("tan_mg_l"),
		NO2MgL:      numPtr("no2_mg_l"),
		H2SMgL:      numPtr("h2s_mg_l"),
		Alkalinity:  numPtr("alkalinity"),
	}
}

func (h *LiveHandler) handleSDRIPreview(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	if e := validateFeatureInput(object(b["features"])); e != nil {
		h.fail(w, e)
		return
	}
	feats := parseFeatures(b["features"])
	assess := sdri.Assess(feats)
	res := assess.Result

	out := rec{
		"status":             res.Status,
		"z_score":            res.ZScore,
		"probability_pct":    res.ProbabilityPct,
		"risk_class":         int(res.RiskClass),
		"risk_level":         res.RiskLevel,
		"s_qpcr":             res.SQPCR,
		"s_bio":              res.SBio,
		"s_chem":             res.SChem,
		"model_version":      res.ModelVersion,
		"recommendation":     res.Recommendation,
		"predicted_disease":  res.PredictedDisease,
		"disease_name":       res.DiseaseName,
		"evidence":           res.Evidence,
		"mitigation_actions": res.MitigationActions,
		"nh3_mg_l":           assess.NH3MgL,
		"copies_per_ml":      assess.CopiesPerML,
	}
	writeSuccess(w, 200, out)
}

func (h *LiveHandler) handleCreateSample(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		if e := validateSample(b); e != nil {
			return nil, e
		}
		pid := str(b, "pond_id")
		p, e := h.pondAccess(t, getUserID(r), pid, true)
		if e != nil {
			return nil, e
		}
		fid := str(p, "farm_id")

		feats := parseFeatures(b["features"])
		assess := sdri.Assess(feats)
		res := assess.Result

		smpID := generateUUID()
		code := str(b, "sample_code")
		if code == "" {
			code = "SMP-" + smpID[:8]
		}

		sDate := str(b, "sampling_date")
		if sDate == "" {
			sDate = time.Now().Format("2006-01-02")
		}
		cDay := int(num(b, "cycle_day"))
		if cDay <= 0 {
			cDay = 1
		}

		smpMeta := object(b["meta"])
		if smpMeta == nil {
			smpMeta = rec{}
		}
		smpMeta["cycle_number"] = int(num(p, "current_cycle"))

		sampleRec := rec{
			"id":            smpID,
			"pond_id":       pid,
			"batch_id":      b["batch_id"],
			"sample_code":   code,
			"sampling_date": sDate,
			"cycle_day":     cDay,
			"source_type":   str(b, "source_type"),
			"meta":          smpMeta,
			"created_at":    now(),
		}
		if sampleRec["source_type"] == "" {
			sampleRec["source_type"] = "GSI_LAB"
		}
		if e := t.Put("samples", sampleRec); e != nil {
			return nil, e
		}

		featRec := cleanFeatures(b["features"])
		featRec["id"] = generateUUID()
		featRec["sample_id"] = smpID
		featRec["created_at"] = now()
		if e := t.Put("sample_features", featRec); e != nil {
			return nil, e
		}

		resRec := rec{
			"id":                 generateUUID(),
			"sample_id":          smpID,
			"status":             string(res.Status),
			"z_score":            res.ZScore,
			"probability_pct":    res.ProbabilityPct,
			"risk_class":         int(res.RiskClass),
			"risk_level":         res.RiskLevel,
			"s_qpcr":             res.SQPCR,
			"s_bio":              res.SBio,
			"s_chem":             res.SChem,
			"model_version":      res.ModelVersion,
			"recommendation":     res.Recommendation,
			"predicted_disease":  res.PredictedDisease,
			"disease_name":       res.DiseaseName,
			"evidence":           res.Evidence,
			"mitigation_actions": res.MitigationActions,
			"created_at":         now(),
		}
		if e := t.Put("sdri_results", resRec); e != nil {
			return nil, e
		}

		if res.RiskClass >= domain.ClassSiaga || res.Status != domain.StatusValid {
			kind := "RISK_CLASS"
			if res.Status != domain.StatusValid {
				kind = "REJECTED"
			}
			msg := fmt.Sprintf("Petak %s terdeteksi status %s (%.2f%%). Rekomendasi: %s", str(p, "pond_code"), res.RiskLevel, res.ProbabilityPct, res.Recommendation)
			if err := t.Put("alerts", rec{
				"id":              generateUUID(),
				"farm_id":         fid,
				"pond_id":         pid,
				"kind":            kind,
				"risk_class":      int(res.RiskClass),
				"probability_pct": res.ProbabilityPct,
				"message":         msg,
				"sample_id":       smpID,
				"open_task_id":    nil,
				"acked_at":        nil,
				"created_at":      now(),
			}); err != nil {
				return nil, err
			}
		}

		return rec{
			"sample_id":   smpID,
			"sdri_result": resRec,
		}, nil
	})
}

func (h *LiveHandler) handleCreateSampleBatch(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		runMap := object(b["run"])
		rawSamples, _ := b["samples"].([]any)
		if len(rawSamples) == 0 {
			return nil, invalid("samples", "Daftar sampel tidak boleh kosong.")
		}

		batchID := generateUUID()
		sampleCount := len(rawSamples)
		rejectedCount := 0

		firstSample := object(rawSamples[0])
		firstPondID := str(firstSample, "pond_id")
		p, e := t.Get("ponds", firstPondID)
		if e != nil {
			return nil, invalid("samples.0.pond_id", "Kolam tidak valid.")
		}
		fid := str(p, "farm_id")
		_, e = h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}

		tTested := str(runMap, "tested_at")
		if tTested == "" {
			tTested = now()
		}
		batchRec := rec{
			"id":             batchID,
			"farm_id":        fid,
			"lab_name":       str(runMap, "lab_name"),
			"instrument":     str(runMap, "instrument"),
			"tested_at":      tTested,
			"file_name":      str(runMap, "file_name"),
			"sample_count":   sampleCount,
			"rejected_count": 0,
			"meta":           b["meta"],
			"created_by":     getUserID(r),
			"created_at":     now(),
		}
		if e := t.Put("batches", batchRec); e != nil {
			return nil, e
		}

		results := []rec{}
		for idx, rawSmp := range rawSamples {
			smp := object(rawSmp)
			if e := validateSample(smp); e != nil {
				return nil, e
			}
			pid := str(smp, "pond_id")
			curPond, e := t.Get("ponds", pid)
			if e != nil || str(curPond, "farm_id") != fid {
				return nil, invalid(fmt.Sprintf("samples.%d.pond_id", idx), "Kolam tidak berada di lokasi yang sama.")
			}

			feats := parseFeatures(smp["features"])
			assess := sdri.Assess(feats)
			res := assess.Result
			if res.Status != domain.StatusValid {
				rejectedCount++
			}

			smpID := generateUUID()
			code := str(smp, "sample_code")
			if code == "" {
				code = fmt.Sprintf("SMP-%s-%d", batchID[:6], idx+1)
			}

			sDate := str(smp, "sampling_date")
			if sDate == "" {
				sDate = time.Now().Format("2006-01-02")
			}
			cDay := int(num(smp, "cycle_day"))
			if cDay <= 0 {
				cDay = 1
			}

			smpMeta := object(smp["meta"])
			if smpMeta == nil {
				smpMeta = rec{}
			}
			smpMeta["cycle_number"] = int(num(curPond, "current_cycle"))
			if err := t.Put("samples", rec{
				"id":            smpID,
				"pond_id":       pid,
				"batch_id":      batchID,
				"sample_code":   code,
				"sampling_date": sDate,
				"cycle_day":     cDay,
				"source_type":   str(smp, "source_type"),
				"meta":          smpMeta,
				"created_at":    now(),
			}); err != nil {
				return nil, err
			}

			featRec := cleanFeatures(smp["features"])
			featRec["id"] = generateUUID()
			featRec["sample_id"] = smpID
			featRec["created_at"] = now()
			if err := t.Put("sample_features", featRec); err != nil {
				return nil, err
			}

			resRec := rec{
				"id":                 generateUUID(),
				"sample_id":          smpID,
				"status":             string(res.Status),
				"z_score":            res.ZScore,
				"probability_pct":    res.ProbabilityPct,
				"risk_class":         int(res.RiskClass),
				"risk_level":         res.RiskLevel,
				"s_qpcr":             res.SQPCR,
				"s_bio":              res.SBio,
				"s_chem":             res.SChem,
				"model_version":      res.ModelVersion,
				"recommendation":     res.Recommendation,
				"predicted_disease":  res.PredictedDisease,
				"disease_name":       res.DiseaseName,
				"evidence":           res.Evidence,
				"mitigation_actions": res.MitigationActions,
				"created_at":         now(),
			}
			if err := t.Put("sdri_results", resRec); err != nil {
				return nil, err
			}

			if res.RiskClass >= domain.ClassSiaga || res.Status != domain.StatusValid {
				kind := "RISK_CLASS"
				if res.Status != domain.StatusValid {
					kind = "REJECTED"
				}
				msg := fmt.Sprintf("Petak %s terdeteksi status %s (%.2f%%). Rekomendasi: %s", str(curPond, "pond_code"), res.RiskLevel, res.ProbabilityPct, res.Recommendation)
				if err := t.Put("alerts", rec{
					"id":              generateUUID(),
					"farm_id":         fid,
					"pond_id":         pid,
					"kind":            kind,
					"risk_class":      int(res.RiskClass),
					"probability_pct": res.ProbabilityPct,
					"message":         msg,
					"sample_id":       smpID,
					"open_task_id":    nil,
					"acked_at":        nil,
					"created_at":      now(),
				}); err != nil {
					return nil, err
				}
			}

			results = append(results, rec{
				"sample_id":   smpID,
				"sample_code": code,
				"sdri_result": resRec,
			})
		}

		batchRec["rejected_count"] = rejectedCount
		if e := t.Put("batches", batchRec); e != nil {
			return nil, e
		}
		if err := h.activity(t, getUserID(r), fid, "UNGGAH_BATCH", "batch", batchID, fmt.Sprintf("Unggah %d sampel dari berkas %s", sampleCount, str(runMap, "file_name"))); err != nil {
			return nil, err
		}

		return rec{
			"batch_id": batchID,
			"results":  results,
		}, nil
	})
}

func (h *LiveHandler) handleGetBatches(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		fid := r.URL.Query().Get("farm_id")
		if fid == "" {
			return nil, invalid("farm_id", "Parameter farm_id wajib disertakan.")
		}
		_, e := h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}

		batches, e := find(t, "batches", "farm_id", fid)
		if e != nil {
			return nil, e
		}

		items := []rec{}
		for _, b := range batches {
			items = append(items, rec{
				"batch_id":       b["id"],
				"lab_name":       b["lab_name"],
				"instrument":     b["instrument"],
				"tested_at":      b["tested_at"],
				"file_name":      b["file_name"],
				"sample_count":   int(num(b, "sample_count")),
				"rejected_count": int(num(b, "rejected_count")),
				"created_at":     b["created_at"],
			})
		}
		return rec{"items": items}, nil
	})
}

func (h *LiveHandler) handleGetBatchDetail(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		bid := r.PathValue("id")
		b, e := t.Get("batches", bid)
		if e != nil {
			return nil, e
		}
		_, e = h.farmAccess(t, getUserID(r), str(b, "farm_id"), true)
		if e != nil {
			return nil, e
		}

		smps, e := find(t, "samples", "batch_id", bid)
		if e != nil {
			return nil, e
		}

		sampleItems := []rec{}
		for _, s := range smps {
			res, _ := first(t, "sdri_results", "sample_id", str(s, "id"))
			sampleItems = append(sampleItems, rec{
				"sample_id":   s["id"],
				"sample_code": s["sample_code"],
				"pond":        pondRef(t, str(s, "pond_id")),
				"sdri_result": res,
			})
		}

		metaMap := object(b["meta"])
		if metaMap == nil {
			metaMap = make(rec)
		}
		if metaMap["wells"] == nil {
			metaMap["wells"] = []any{}
		}
		if metaMap["standards"] == nil {
			metaMap["standards"] = []any{}
		}

		return rec{
			"batch_id":       b["id"],
			"lab_name":       b["lab_name"],
			"instrument":     b["instrument"],
			"tested_at":      b["tested_at"],
			"file_name":      b["file_name"],
			"sample_count":   int(num(b, "sample_count")),
			"rejected_count": int(num(b, "rejected_count")),
			"created_at":     b["created_at"],
			"samples":        sampleItems,
			"meta":           metaMap,
		}, nil
	})
}

func (h *LiveHandler) handleGetPondSamples(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		pid := r.PathValue("id")
		_, e := h.pondAccess(t, getUserID(r), pid, false)
		if e != nil {
			return nil, e
		}

		smps, e := find(t, "samples", "pond_id", pid)
		if e != nil {
			return nil, e
		}

		items := []rec{}
		for _, s := range smps {
			res, _ := first(t, "sdri_results", "sample_id", str(s, "id"))
			items = append(items, rec{
				"sample_id":     s["id"],
				"sample_code":   s["sample_code"],
				"sampling_date": s["sampling_date"],
				"cycle_day":     int(num(s, "cycle_day")),
				"sdri_result":   res,
			})
		}
		return page(r, items), nil
	})
}

func (h *LiveHandler) handleGetSampleDetail(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		sid := r.PathValue("id")
		s, e := t.Get("samples", sid)
		if e != nil {
			return nil, e
		}
		pid := str(s, "pond_id")
		p, e := t.Get("ponds", pid)
		if e != nil {
			return nil, e
		}
		f, e := h.farmAccess(t, getUserID(r), str(p, "farm_id"), false)
		if e != nil {
			return nil, e
		}
		owner := str(f, "owner_id") == getUserID(r)
		if !owner && str(p, "assignee_id") != getUserID(r) {
			return nil, forbidden()
		}

		featRec, e := first(t, "sample_features", "sample_id", sid)
		if e != nil {
			return nil, e
		}
		resRec, _ := first(t, "sdri_results", "sample_id", sid)
		feats := parseFeatures(featRec)
		assess := sdri.Assess(feats)

		var metaVal any = sampleMeta(s["meta"])
		if !owner {
			// Technician cannot view raw well meta
			metaVal = nil
		}

		return rec{
			"id":            s["id"],
			"pond":          pondRef(t, pid),
			"batch_id":      s["batch_id"],
			"sample_code":   s["sample_code"],
			"sampling_date": s["sampling_date"],
			"cycle_day":     int(num(s, "cycle_day")),
			"features":      cleanFeatures(featRec),
			"meta":          metaVal,
			"sdri_result":   resRec,
			"copies_per_ml": assess.CopiesPerML,
			"nh3_mg_l":      assess.NH3MgL,
			// K9: semua fitur X1-X12 (termasuk yang aman), bukan hanya evidence.
			"engineered_features": assess.Features,
			"microbiome":          microbiomeOf(t, sid),
		}, nil
	})
}

// microbiomeOf mengembalikan metrik komunitas sampel, atau nil bila tabel kelimpahan belum diunggah.
func microbiomeOf(t tx, sid string) any {
	mb, e := first(t, microbiomeTable, "sample_id", sid)
	if e != nil {
		return nil
	}
	if m := metricsOf(mb["metrics"]); m != nil {
		return m
	}
	return nil
}

func (h *LiveHandler) handleGetTrajectoryLive(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		pid := r.PathValue("id")
		_, e := h.pondAccess(t, getUserID(r), pid, false)
		if e != nil {
			return nil, e
		}

		smps, e := find(t, "samples", "pond_id", pid)
		if e != nil {
			return nil, e
		}

		var history []domain.TrajectoryPoint
		for _, s := range smps {
			res, e := first(t, "sdri_results", "sample_id", str(s, "id"))
			if e != nil {
				continue
			}
			history = append(history, domain.TrajectoryPoint{
				SamplingDate:   str(s, "sampling_date"),
				CycleDay:       int(num(s, "cycle_day")),
				ProbabilityPct: num(res, "probability_pct"),
				RiskClass:      int(num(res, "risk_class")),
			})
		}

		rep := trajectory.EvaluateTrajectory(pid, history)
		return rec{
			"pond_id":          rep.PondID,
			"data_points":      rep.DataPoints,
			"slope":            rep.Slope,
			"trend":            rep.Trend,
			"trajectory_alert": rep.TrajectoryAlert,
			"alert_message":    rep.AlertMessage,
			"history":          rep.History,
		}, nil
	})
}

func (h *LiveHandler) handleGetPondCycles(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		pid := r.PathValue("id")
		_, e := h.pondAccess(t, getUserID(r), pid, true)
		if e != nil {
			return nil, e
		}
		rows, e := find(t, "cycle_outcomes", "pond_id", pid)
		if e != nil {
			return nil, e
		}
		items := []rec{}
		for _, c := range rows {
			items = append(items, rec{
				"cycle_number":             int(num(c, "cycle_number")),
				"outcome":                  c["outcome"],
				"actual_harvest_kg":        num(c, "actual_harvest_kg"),
				"outbreak_date":            c["outbreak_date"],
				"primary_suspect_pathogen": c["primary_suspect_pathogen"],
				"notes":                    c["notes"],
			})
		}
		return rec{"items": items}, nil
	})
}

func (h *LiveHandler) handleCreateWaterLog(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		pid := r.PathValue("id")
		p, e := h.pondAccess(t, getUserID(r), pid, false)
		if e != nil {
			return nil, e
		}

		if e := validateWater(b); e != nil {
			return nil, e
		}

		temp := num(b, "temp_c")
		ph := num(b, "ph")
		tan := num(b, "tan_mg_l")
		_, _, nh3 := sdri.CalculateFreeAmmonia(tan, temp, ph)

		mDate := str(b, "measured_at")
		if mDate == "" {
			mDate = now()
		}

		id := generateUUID()
		wlog := rec{
			"id":              id,
			"pond_id":         pid,
			"measured_at":     mDate,
			"temp_c":          temp,
			"ph":              ph,
			"do_mg_l":         num(b, "do_mg_l"),
			"tan_mg_l":        tan,
			"no2_mg_l":        b["no2_mg_l"],
			"h2s_mg_l":        b["h2s_mg_l"],
			"tom_mg_l":        b["tom_mg_l"],
			"alkalinity":      b["alkalinity"],
			"salinity_ppt":    b["salinity_ppt"],
			"transparency_cm": b["transparency_cm"],
			"nh3_mg_l":        nh3,
			"recorded_by":     getUserID(r),
			"created_at":      now(),
		}
		if e := t.Put("water_logs", wlog); e != nil {
			return nil, e
		}

		out := clone(wlog)
		out["pond"] = pondRef(t, pid)
		out["recorded_by"] = ref(t, getUserID(r))
		_ = p
		return out, nil
	})
}

func (h *LiveHandler) handleGetPondWaterLogs(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		pid := r.PathValue("id")
		_, e := h.pondAccess(t, getUserID(r), pid, false)
		if e != nil {
			return nil, e
		}

		logs, e := find(t, "water_logs", "pond_id", pid)
		if e != nil {
			return nil, e
		}

		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		items := []rec{}
		for _, l := range logs {
			m := str(l, "measured_at")
			if from != "" && m < from {
				continue
			}
			if to != "" && m > to {
				continue
			}
			out := clone(l)
			out["pond"] = pondRef(t, pid)
			out["recorded_by"] = ref(t, str(l, "recorded_by"))
			items = append(items, out)
		}
		return page(r, items), nil
	})
}

func (h *LiveHandler) handleGetFarmWaterLogs(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		fid := r.PathValue("id")
		_, e := h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}

		ponds, e := find(t, "ponds", "farm_id", fid)
		if e != nil {
			return nil, e
		}

		filterPondID := r.URL.Query().Get("pond_id")
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")

		items := []rec{}
		for _, p := range ponds {
			pid := str(p, "id")
			if filterPondID != "" && pid != filterPondID {
				continue
			}
			logs, e := find(t, "water_logs", "pond_id", pid)
			if e != nil {
				continue
			}
			for _, l := range logs {
				m := str(l, "measured_at")
				if from != "" && m < from {
					continue
				}
				if to != "" && m > to {
					continue
				}
				out := clone(l)
				out["pond"] = pondRef(t, pid)
				out["recorded_by"] = ref(t, str(l, "recorded_by"))
				items = append(items, out)
			}
		}
		return page(r, items), nil
	})
}

func (h *LiveHandler) handlePatchWaterLog(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		id := r.PathValue("id")
		l, e := t.Get("water_logs", id)
		if e != nil {
			return nil, e
		}

		if str(l, "recorded_by") != getUserID(r) {
			return nil, forbidden()
		}

		// Max 1 hour edit window
		cTime := timestamp(l["created_at"])
		if time.Since(cTime) > time.Hour {
			return nil, invalid("id", "Catatan kimia air hanya dapat diubah maksimal 1 jam setelah dibuat.")
		}

		mergeAllowed(l, b, "temp_c", "ph", "do_mg_l", "tan_mg_l", "no2_mg_l", "h2s_mg_l", "tom_mg_l", "alkalinity", "salinity_ppt", "transparency_cm")
		if _, e := h.pondAccess(t, getUserID(r), str(l, "pond_id"), false); e != nil {
			return nil, e
		}
		if e := validateWater(l); e != nil {
			return nil, e
		}
		_, _, nh3 := sdri.CalculateFreeAmmonia(num(l, "tan_mg_l"), num(l, "temp_c"), num(l, "ph"))
		l["nh3_mg_l"] = nh3

		if e := t.Put("water_logs", l); e != nil {
			return nil, e
		}
		out := clone(l)
		out["pond"] = pondRef(t, str(l, "pond_id"))
		out["recorded_by"] = ref(t, str(l, "recorded_by"))
		return out, nil
	})
}

func validateWater(b rec) error {
	ranges := []struct {
		k        string
		min, max float64
		required bool
	}{{"temp_c", 15, 40, true}, {"ph", 5, 10, true}, {"do_mg_l", 0, 20, true}, {"tan_mg_l", 0, 20, true}, {"no2_mg_l", 0, 20, false}, {"h2s_mg_l", 0, 2, false}, {"tom_mg_l", 0, 500, false}, {"alkalinity", 0, 400, false}, {"salinity_ppt", 0, 60, false}, {"transparency_cm", 0, 200, false}}
	for _, x := range ranges {
		v, ok := b[x.k]
		if v == nil {
			if x.required {
				return invalid(x.k, "Nilai pengukuran wajib diisi.")
			}
			continue
		}
		n, isNumber := v.(float64)
		if !ok || !isNumber || n < x.min || n > x.max {
			return invalid(x.k, fmt.Sprintf("Masukkan angka antara %g dan %g.", x.min, x.max))
		}
	}
	if timestamp(b["measured_at"]).IsZero() {
		return invalid("measured_at", "Masukkan waktu dengan zona yang valid.")
	}
	return nil
}

var featureKeys = []string{"ct_ahpnd", "ct_wssv", "ct_ehp", "ct_amoa", "ct_16s", "ct_ppc", "do_mg_l", "tom_mg_l", "ph", "salinity_ppt", "temp_c", "tan_mg_l", "no2_mg_l", "h2s_mg_l", "alkalinity"}

func cleanFeatures(v any) rec {
	raw := object(v)
	out := rec{}
	for _, k := range featureKeys {
		out[k] = raw[k]
	}
	return out
}
func sampleMeta(v any) any {
	if v == nil {
		return nil
	}
	m := object(v)
	if m["wells"] == nil {
		m["wells"] = []any{}
	}
	if m["standards"] == nil {
		m["standards"] = []any{}
	}
	if _, ok := m["water_log_id"]; !ok {
		m["water_log_id"] = nil
	}
	return m
}
func validateFeatureInput(f rec) error {
	for _, k := range featureKeys {
		if f[k] == nil {
			continue
		}
		v, ok := f[k].(float64)
		if !ok {
			return invalid(k, "Masukkan angka pengukuran atau null bila belum diuji.")
		}
		if strings.HasPrefix(k, "ct_") && (v <= 0 || v > 40) {
			return invalid(k, "Nilai panel jejak penyakit harus lebih dari 0 sampai 40.")
		}
	}
	ranges := []struct {
		k        string
		min, max float64
	}{{"temp_c", 15, 40}, {"ph", 5, 10}, {"do_mg_l", 0, 20}, {"tan_mg_l", 0, 20}, {"no2_mg_l", 0, 20}, {"h2s_mg_l", 0, 2}, {"tom_mg_l", 0, 500}, {"alkalinity", 0, 400}, {"salinity_ppt", 0, 60}}
	for _, x := range ranges {
		if f[x.k] != nil && (num(f, x.k) < x.min || num(f, x.k) > x.max) {
			return invalid(x.k, fmt.Sprintf("Masukkan angka antara %g dan %g.", x.min, x.max))
		}
	}
	return nil
}
func validateSample(b rec) error {
	if e := requireText(b, "pond_id", "sample_code", "sampling_date"); e != nil {
		return e
	}
	if _, e := time.Parse("2006-01-02", str(b, "sampling_date")); e != nil {
		return invalid("sampling_date", "Gunakan tanggal YYYY-MM-DD.")
	}
	day, ok := b["cycle_day"].(float64)
	if !ok || day < 0 || day > 1000 || day != math.Trunc(day) {
		return invalid("cycle_day", "Gunakan bilangan hari siklus 0 sampai 1000.")
	}
	return validateFeatureInput(object(b["features"]))
}

package handler

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"aquaseq-back/internal/ai"
	"aquaseq-back/internal/repository"
)

var allowedVisualKeys = map[string]int{
	"WarnaAir":     5,
	"LapisanAlga":   2,
	"UdangMenyala":  2,
	"KotoranPutih":  2,
	"EkorMerah":     2,
	"InsangHitam":   2,
	"LumutTubuh":    2,
}

func (h *LiveHandler) handleMobileGetFarm(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		uid := getUserID(r)

		// 1. Gather all accessible farms for this user
		owned, err := find(t, "farms", "owner_id", uid)
		if err != nil {
			return nil, err
		}
		membs, err := find(t, "memberships", "user_id", uid)
		if err != nil {
			return nil, err
		}

		farmMap := make(map[string]rec)
		for _, f := range owned {
			if f["archived_at"] == nil {
				farmMap[str(f, "id")] = f
			}
		}
		for _, m := range membs {
			if boolean(m, "active") {
				f, e := t.Get("farms", str(m, "farm_id"))
				if e == nil && f["archived_at"] == nil {
					farmMap[str(f, "id")] = f
				}
			}
		}

		var farmList []rec
		for _, f := range farmMap {
			farmList = append(farmList, f)
		}
		sort.Slice(farmList, func(i, j int) bool {
			return str(farmList[i], "id") < str(farmList[j], "id")
		})

		var selectedFarm rec
		reqFarmID := r.URL.Query().Get("farm_id")
		if reqFarmID != "" {
			f, ok := farmMap[reqFarmID]
			if !ok {
				// Check if farm exists to distinguish 404 vs 403
				if _, e := t.Get("farms", reqFarmID); e != nil {
					return nil, repository.ErrNotFound
				}
				return nil, forbidden()
			}
			selectedFarm = f
		} else {
			if len(farmList) == 0 {
				return nil, repository.ErrNotFound
			}
			selectedFarm = farmList[0]
		}

		fid := str(selectedFarm, "id")
		isOwner := str(selectedFarm, "owner_id") == uid

		// 2. Fetch accessible ponds
		allPonds, err := find(t, "ponds", "farm_id", fid)
		if err != nil {
			return nil, err
		}
		var ponds []rec
		pondSet := make(map[string]bool)
		blokSet := make(map[string]bool)

		for _, p := range allPonds {
			if p["archived_at"] != nil {
				continue
			}
			if !isOwner && str(p, "assignee_id") != uid {
				continue
			}
			pID := str(p, "id")
			pondSet[pID] = true

			pCode := str(p, "pond_code")
			blok := "A"
			if len(pCode) > 0 {
				blok = string([]rune(pCode)[0])
			}
			blokSet[blok] = true

			docDays := 0
			if p["stocked_at"] != nil {
				tStock := timestamp(p["stocked_at"])
				if tStock.IsZero() {
					if tParsed, e := time.Parse("2006-01-02", str(p, "stocked_at")); e == nil {
						tStock = tParsed
					}
				}
				if !tStock.IsZero() {
					days := int(time.Since(tStock).Hours() / 24)
					if days > 0 {
						docDays = days
					}
				}
			}

			name := str(p, "name")
			if name == "" {
				name = "Kolam " + pCode
			}

			ponds = append(ponds, rec{
				"id":           pID,
				"name":         name,
				"blok":         blok,
				"docDays":      docDays,
				"srPercent":    nil,
				"areaM2":       num(p, "area_sqm"),
				"stockedCount": nil,
				"abwGram":      nil,
			})
		}
		sort.Slice(ponds, func(i, j int) bool {
			return str(ponds[i], "name") < str(ponds[j], "name")
		})

		// 3. Farm initials and bloks
		farmName := str(selectedFarm, "name")
		initials := "AS"
		words := strings.Fields(farmName)
		if len(words) >= 2 {
			initials = strings.ToUpper(string([]rune(words[0])[0]) + string([]rune(words[1])[0]))
		} else if len(words) == 1 && len(words[0]) >= 2 {
			initials = strings.ToUpper(words[0][:2])
		}

		var bloks []string
		for b := range blokSet {
			bloks = append(bloks, "Blok "+b)
		}
		sort.Strings(bloks)

		farmOut := rec{
			"id":       fid,
			"name":     farmName,
			"initials": initials,
			"bloks":    bloks,
		}

		// 4. Readings for accessible ponds
		allReadings, err := t.Find("mobile_readings", func(r rec) bool {
			return pondSet[str(r, "pond_id")]
		})
		if err != nil {
			allReadings = []rec{}
		}
		sort.Slice(allReadings, func(i, j int) bool {
			return str(allReadings[i], "recorded_at") > str(allReadings[j], "recorded_at")
		})
		readingsOut := make([]rec, 0, len(allReadings))
		for _, r := range allReadings {
			u := ref(t, str(r, "user_id"))
			readingsOut = append(readingsOut, rec{
				"id":     str(r, "client_id"),
				"pondId": str(r, "pond_id"),
				"at":     str(r, "recorded_at"),
				"by":     str(u, "full_name"),
				"values": r["values"],
				"synced": true,
			})
		}

		// 5. Risk scores and recommendations ONLY from valid SDRI
		riskScoresOut := []rec{}
		recsOut := []rec{}
		for _, p := range ponds {
			pID := str(p, "id")
			smps, _ := find(t, "samples", "pond_id", pID)
			sort.Slice(smps, func(i, j int) bool {
				if num(smps[i], "cycle_day") == num(smps[j], "cycle_day") {
					return str(smps[i], "created_at") > str(smps[j], "created_at")
				}
				return num(smps[i], "cycle_day") > num(smps[j], "cycle_day")
			})
			for _, s := range smps {
				res, e := first(t, "sdri_results", "sample_id", str(s, "id"))
				if e == nil && str(res, "status") == "VALID" {
					scoreVal := math.Round(num(res, "probability_pct"))
					rClass := int(num(res, "risk_class"))
					label := "Rendah"
					if rClass == 2 {
						label = "Sedang"
					} else if rClass >= 3 {
						label = "Tinggi"
					}

					ctxText := str(res, "disease_name")
					if ctxText == "" {
						ctxText = str(res, "predicted_disease")
					}
					if ctxText == "" {
						ctxText = "Evaluasi risiko eDNA valid"
					}

					riskScoresOut = append(riskScoresOut, rec{
						"pondId":  pID,
						"score":   int(scoreVal),
						"label":   label,
						"context": ctxText,
					})

					recText := str(res, "recommendation")
					if recText != "" {
						icon := "eye"
						low := strings.ToLower(recText)
						if strings.Contains(low, "pakan") {
							icon = "wheat"
						} else if strings.Contains(low, "probiotik") || strings.Contains(low, "kapur") {
							icon = "flask"
						} else if strings.Contains(low, "air") || strings.Contains(low, "aerasi") {
							icon = "droplet"
						} else if strings.Contains(low, "sipon") || strings.Contains(low, "lumpur") {
							icon = "shovel"
						}
						recsOut = append(recsOut, rec{
							"id":   str(s, "id"),
							"icon": icon,
							"text": recText,
						})
					}
					break // Only latest valid per pond
				}
			}
		}

		// 6. Tasks
		allTasks, _ := find(t, "tasks", "farm_id", fid)
		wibZone := time.FixedZone("WIB", 7*3600)
		tasksOut := []rec{}
		for _, task := range allTasks {
			if !pondSet[str(task, "pond_id")] {
				continue
			}
			if !isOwner && str(task, "assignee_id") != uid {
				continue
			}
			dueTime := timestamp(task["due_at"]).In(wibZone).Format("15.04 WIB")
			isDone := has([]string{"dilaporkan", "selesai"}, str(task, "status"))
			tasksOut = append(tasksOut, rec{
				"id":    str(task, "id"),
				"title": str(task, "title"),
				"time":  dueTime,
				"note":  str(task, "instruction"),
				"done":  isDone,
			})
		}

		return rec{
			"farm":            farmOut,
			"ponds":           ponds,
			"readings":        readingsOut,
			"riskScores":      riskScoresOut,
			"recommendations": recsOut,
			"tasks":           tasksOut,
			"feedTodayKg":     nil,
		}, nil
	})
}

func (h *LiveHandler) handleMobilePostReadings(w http.ResponseWriter, r *http.Request) {
	b, err := decode(w, r)
	if err != nil {
		h.fail(w, err)
		return
	}

	clientID := strings.TrimSpace(str(b, "id"))
	if clientID == "" || len(clientID) > 128 {
		h.fail(w, invalid("id", "ID pengamatan lokal wajib diisi (maks 128 karakter)."))
		return
	}

	pondID := strings.TrimSpace(str(b, "pondId"))
	if pondID == "" {
		h.fail(w, invalid("pondId", "ID kolam wajib diisi."))
		return
	}

	tAt := timestamp(b["at"])
	if tAt.IsZero() {
		if tParsed, e := time.Parse(time.RFC3339, str(b, "at")); e == nil {
			tAt = tParsed
		}
	}
	if tAt.IsZero() {
		h.fail(w, invalid("at", "Format waktu tidak valid."))
		return
	}

	rawValues, ok := b["values"].(map[string]any)
	if !ok || len(rawValues) == 0 {
		h.fail(w, invalid("values", "Nilai pengamatan wajib berupa objek non-kosong."))
		return
	}

	cleanValues := make(map[string]int)
	for k, v := range rawValues {
		maxVal, allowed := allowedVisualKeys[k]
		if !allowed {
			h.fail(w, invalid("values", fmt.Sprintf("Kunci pengamatan '%s' tidak valid.", k)))
			return
		}
		var numVal float64
		switch nv := v.(type) {
		case float64:
			numVal = nv
		case int:
			numVal = float64(nv)
		default:
			h.fail(w, invalid(k, "Nilai pengamatan harus angka."))
			return
		}
		if numVal != math.Trunc(numVal) || numVal < 0 || int(numVal) > maxVal {
			h.fail(w, invalid(k, fmt.Sprintf("Nilai untuk '%s' harus bilangan bulat 0..%d.", k, maxVal)))
			return
		}
		cleanValues[k] = int(numVal)
	}

	h.respond(w, r, true, func(t tx) (any, error) {
		uid := getUserID(r)

		// Pond access check enforces tenant and farm scoping
		if _, e := h.pondAccess(t, uid, pondID, false); e != nil {
			return nil, e
		}

		// Idempotency check: (user_id, client_id)
		existing, err := t.Find("mobile_readings", func(r rec) bool {
			return str(r, "user_id") == uid && str(r, "client_id") == clientID
		})
		if err == nil && len(existing) > 0 {
			return rec{"id": clientID, "status": "recorded"}, nil
		}

		readingRec := rec{
			"id":          generateUUID(),
			"pond_id":     pondID,
			"user_id":     uid,
			"client_id":   clientID,
			"recorded_at": tAt.UTC().Format(time.RFC3339Nano),
			"values":      cleanValues,
			"created_at":  now(),
		}
		if e := t.Put("mobile_readings", readingRec); e != nil {
			return nil, e
		}

		return rec{"id": clientID, "status": "recorded"}, nil
	})
}

func (h *LiveHandler) handleMobilePatchTask(w http.ResponseWriter, r *http.Request) {
	b, err := decode(w, r)
	if err != nil {
		h.fail(w, err)
		return
	}

	doneVal, ok := b["done"].(bool)
	if !ok {
		h.fail(w, invalid("done", "Field 'done' wajib boolean."))
		return
	}

	h.respond(w, r, true, func(t tx) (any, error) {
		uid := getUserID(r)
		taskID := r.PathValue("id")
		task, err := h.taskAccess(t, uid, taskID)
		if err != nil {
			return nil, err
		}

		curStatus := str(task, "status")
		if doneVal {
			// Only assigned technician may report done
			if str(task, "assignee_id") != uid {
				return nil, forbidden()
			}
			if curStatus == "dilaporkan" || curStatus == "selesai" {
				return rec{"id": taskID, "done": true, "status": curStatus}, nil
			}
			if curStatus == "batal" {
				return nil, &requestError{status: http.StatusConflict, code: "CONFLICT", message: "Tugas sudah dibatalkan."}
			}

			rep := rec{
				"id":          generateUUID(),
				"task_id":     task["id"],
				"note":        "Laporan diselesaikan dari aplikasi mobile",
				"measured":    nil,
				"photos":      []rec{},
				"reported_by": uid,
				"reported_at": now(),
			}
			if e := t.Put("task_reports", rep); e != nil {
				return nil, e
			}

			task["status"] = "dilaporkan"
			task["updated_at"] = now()
			if e := t.Put("tasks", task); e != nil {
				return nil, e
			}

			if e := h.history(t, uid, taskID, "dilaporkan", "Ditandai selesai dari aplikasi mobile"); e != nil {
				return nil, e
			}

			farm, _ := t.Get("farms", str(task, "farm_id"))
			_ = h.notify(t, str(farm, "owner_id"), "laporan_masuk", "Laporan tugas masuk", "task", taskID)
			return rec{"id": taskID, "done": true, "status": "dilaporkan"}, nil
		}

		// done: false cannot undo closed or reported without valid web workflow
		if has([]string{"dilaporkan", "selesai", "batal"}, curStatus) {
			return nil, &requestError{status: http.StatusConflict, code: "CONFLICT", message: "Tugas yang sudah dilaporkan atau ditutup tidak dapat dibatalkan lewat mobile."}
		}

		return rec{"id": taskID, "done": false, "status": curStatus}, nil
	})
}

func (h *LiveHandler) handleMobileScheduleRecommendation(w http.ResponseWriter, r *http.Request) {
	sampleID := r.PathValue("id")
	h.respond(w, r, true, func(t tx) (any, error) {
		uid := getUserID(r)

		s, err := t.Get("samples", sampleID)
		if err != nil {
			return nil, err
		}
		p, err := t.Get("ponds", str(s, "pond_id"))
		if err != nil {
			return nil, err
		}

		// Only farm owner can schedule recommendation
		if _, err := h.farmAccess(t, uid, str(p, "farm_id"), true); err != nil {
			return nil, err
		}

		sdriRes, err := first(t, "sdri_results", "sample_id", sampleID)
		if err != nil || str(sdriRes, "status") != "VALID" {
			return nil, &requestError{status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: "Skor rekomendasi belum valid atau tidak ditemukan."}
		}

		assigneeID := str(p, "assignee_id")
		if assigneeID == "" {
			return nil, invalid("assignee_id", "Kolam belum memiliki teknisi yang ditugaskan.")
		}

		instruction := str(sdriRes, "recommendation")
		if instruction == "" {
			instruction = "Lakukan tindakan mitigasi sesuai rekomendasi SDRI."
		}

		wibZone := time.FixedZone("WIB", 7*3600)
		nextWIB := time.Now().In(wibZone).AddDate(0, 0, 1)
		schedDate := nextWIB.Format("2006-01-02")
		dueAt := time.Date(nextWIB.Year(), nextWIB.Month(), nextWIB.Day(), 7, 0, 0, 0, wibZone).UTC().Format(time.RFC3339)

		schedKey := fmt.Sprintf("%s:%s:%s", sampleID, uid, schedDate)
		existing, err := t.Find("mobile_schedules", func(r rec) bool {
			return str(r, "schedule_key") == schedKey
		})
		if err == nil && len(existing) > 0 {
			return rec{"task_id": str(existing[0], "task_id"), "scheduled_date": schedDate, "status": "scheduled"}, nil
		}

		taskID := generateUUID()
		task := rec{
			"id":               taskID,
			"farm_id":          p["farm_id"],
			"pond_id":          p["id"],
			"assignee_id":      assigneeID,
			"created_by":       uid,
			"kind":             "tindakan",
			"title":            "Tindakan Rekomendasi: " + str(p, "pond_code"),
			"instruction":      instruction,
			"due_at":           dueAt,
			"status":           "baru",
			"owner_note":       nil,
			"source_sample_id": sampleID,
			"source_alert_id":  nil,
			"created_at":       now(),
			"updated_at":       now(),
		}
		if e := t.Put("tasks", task); e != nil {
			return nil, e
		}

		_ = h.history(t, uid, taskID, "baru", "Dijadwalkan dari rekomendasi mobile")
		_ = h.notify(t, assigneeID, "tugas_baru", "Tugas baru: "+str(task, "title"), "task", taskID)

		schedRec := rec{
			"id":             generateUUID(),
			"schedule_key":   schedKey,
			"task_id":        taskID,
			"sample_id":      sampleID,
			"owner_id":       uid,
			"scheduled_date": schedDate,
			"created_at":     now(),
		}
		if e := t.Put("mobile_schedules", schedRec); e != nil {
			return nil, e
		}

		return rec{"task_id": taskID, "scheduled_date": schedDate, "status": "scheduled"}, nil
	})
}

func (h *LiveHandler) handleMobileAIMitigasi(w http.ResponseWriter, r *http.Request) {
	b, err := decode(w, r)
	if err != nil {
		h.fail(w, err)
		return
	}

	for k := range b {
		if k != "pond_id" && k != "pond" {
			h.fail(w, invalid(k, "Endpoint mitigasi hanya menerima 'pond_id'."))
			return
		}
	}

	pondID := str(b, "pond_id")
	if pondID == "" {
		if pObj, ok := b["pond"].(map[string]any); ok {
			pondID = str(pObj, "id")
		}
	}
	if pondID == "" {
		h.fail(w, invalid("pond_id", "ID kolam wajib diisi."))
		return
	}

	uid := getUserID(r)
	var authedPond rec
	err = h.store.Read(r.Context(), func(t tx) error {
		p, e := h.pondAccess(t, uid, pondID, false)
		if e != nil {
			return e
		}
		authedPond = p
		return nil
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	_ = authedPond

	provider := NewStorePondProvider(h.store)
	facts, err := provider.GetLatestMachineFacts(r.Context(), pondID)
	if err != nil || !facts.DataAvailable || facts.QCStatus != "VALID" {
		msg := "Kontrol kualitas gagal atau data fakta mesin belum valid."
		if err != nil {
			msg = err.Error()
		}
		writeErrorWithCode(w, http.StatusUnprocessableEntity, "UNPROCESSABLE_ENTITY", msg, nil)
		return
	}

	report, err := ai.BuildExplanation(facts)
	if err != nil {
		writeErrorWithCode(w, http.StatusUnprocessableEntity, "UNPROCESSABLE_ENTITY", err.Error(), nil)
		return
	}

	severity := "WASPADA"
	if facts.SDRIClass == 1 {
		severity = "SEHAT"
	} else if facts.SDRIClass == 2 {
		severity = "WASPADA"
	} else if facts.SDRIClass >= 3 {
		severity = "KRITIS"
	}

	steps := make([]rec, 0, len(report.Actions))
	for _, a := range report.Actions {
		steps = append(steps, rec{
			"title":  a.Action,
			"detail": a.Source,
		})
	}

	summary := fmt.Sprintf("Potensi: %s. %s", report.DiseasePotential, report.EvidenceReason)
	writeSuccess(w, http.StatusOK, rec{
		"severity": severity,
		"summary":  summary,
		"steps":    steps,
	})
}

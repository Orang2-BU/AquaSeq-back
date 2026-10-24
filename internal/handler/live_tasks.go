package handler

import (
	"aquaseq-back/internal/repository"
	"bytes"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/report"
	"aquaseq-back/internal/sdri"
)

func (h *LiveHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, 200, rec{"status": "healthy", "service": "aquaseq-engine", "version": "1.0.0"})
}
func (h *LiveHandler) handleCalculateSDRI(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	for _, k := range []string{"ct_ppc", "s_qpcr", "s_bio", "s_chem"} {
		if _, ok := b[k].(float64); !ok {
			h.fail(w, invalid(k, "Masukkan angka yang valid."))
			return
		}
	}
	writeSuccess(w, 200, sdri.Calculate(domain.SDRIInput{CTPPC: num(b, "ct_ppc"), SQPCR: num(b, "s_qpcr"), SBio: num(b, "s_bio"), SChem: num(b, "s_chem")}))
}
func (h *LiveHandler) handleWhatsAppPreview(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, false, func(t tx) (any, error) {
		farms, e := find(t, "farms", "owner_id", getUserID(r))
		if e != nil {
			return nil, e
		}
		if len(farms) == 0 {
			return nil, forbidden()
		}
		return rec{"whatsapp_message": report.GenerateWhatsAppMessage(report.WhatsAppPayload{PondCode: str(b, "pond_code"), ProbabilityPct: num(b, "probability_pct"), RiskClass: domain.RiskClass(num(b, "risk_class")), PrimaryDriver: str(b, "primary_driver"), Action: str(b, "action")})}, nil
	})
}
func (h *LiveHandler) handleCycleOutcome(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		if _, e := h.pondAccess(t, getUserID(r), str(b, "pond_id"), true); e != nil {
			return nil, e
		}
		if num(b, "cycle_number") < 1 {
			return nil, invalid("cycle_number", "Nomor siklus minimal 1.")
		}
		if !has([]string{"HARVEST_SUCCESS", "OUTBREAK", "EMERGENCY_HARVEST"}, str(b, "outcome")) {
			return nil, invalid("outcome", "Pilih hasil siklus yang tersedia.")
		}
		rows, e := find(t, "cycle_outcomes", "pond_id", str(b, "pond_id"))
		if e != nil {
			return nil, e
		}
		id := generateUUID()
		for _, x := range rows {
			if num(x, "cycle_number") == num(b, "cycle_number") {
				id = str(x, "id")
			}
		}
		out := rec{"id": id, "pond_id": b["pond_id"], "cycle_number": int(num(b, "cycle_number")), "outcome": b["outcome"], "actual_harvest_kg": num(b, "actual_harvest_kg"), "outbreak_date": b["outbreak_date"], "primary_suspect_pathogen": b["primary_suspect_pathogen"], "notes": b["notes"], "created_at": now()}
		if e = t.Put("cycle_outcomes", out); e != nil {
			return nil, e
		}
		return rec{"status": "RECORDED", "message": "Hasil siklus berhasil dicatat."}, nil
	})
}
func (h *LiveHandler) taskAccess(t tx, user, id string) (rec, error) {
	task, e := t.Get("tasks", id)
	if e != nil {
		return nil, e
	}
	f, e := h.farmAccess(t, user, str(task, "farm_id"), false)
	if e != nil {
		return nil, e
	}
	if str(f, "owner_id") != user {
		if str(task, "assignee_id") != user {
			return nil, forbidden()
		}
		if _, e = h.pondAccess(t, user, str(task, "pond_id"), false); e != nil {
			return nil, e
		}
	}
	return task, nil
}
func (h *LiveHandler) taskView(t tx, task rec) rec {
	out := clone(task)
	out["pond"] = pondRef(t, str(task, "pond_id"))
	out["assignee"] = ref(t, str(task, "assignee_id"))
	out["created_by"] = ref(t, str(task, "created_by"))
	out["owner_note"] = task["owner_note"]
	out["source_sample_id"] = task["source_sample_id"]
	out["overdue"] = timestamp(task["due_at"]).Before(time.Now()) && !has([]string{"selesai", "batal"}, str(task, "status"))
	return out
}
func (h *LiveHandler) history(t tx, user, id, status string, note any) error {
	return t.Put("task_history", rec{"id": generateUUID(), "task_id": id, "status": status, "user_id": user, "note": note, "created_at": now()})
}
func (h *LiveHandler) handleGetFarmTasks(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		fid := r.PathValue("id")
		if _, e := h.farmAccess(t, getUserID(r), fid, true); e != nil {
			return nil, e
		}
		tasks, e := find(t, "tasks", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		items := []rec{}
		for _, x := range tasks {
			include := true
			for _, k := range []string{"status", "assignee_id", "pond_id"} {
				if q := r.URL.Query().Get(k); q != "" && str(x, k) != q {
					include = false
				}
			}
			if include {
				items = append(items, h.taskView(t, x))
			}
		}
		return page(r, items), nil
	})
}
func (h *LiveHandler) handleGetMyTasks(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		rows, e := find(t, "tasks", "assignee_id", getUserID(r))
		if e != nil {
			return nil, e
		}
		items := []rec{}
		date := r.URL.Query().Get("date")
		today := time.Now().UTC().Format("2006-01-02")
		for _, x := range rows {
			if _, e := h.pondAccess(t, getUserID(r), str(x, "pond_id"), false); e != nil {
				continue
			}
			due := timestamp(x["due_at"]).UTC().Format("2006-01-02")
			if date == "today" && due != today || date == "upcoming" && due <= today || date == "overdue" && (due >= today || has([]string{"selesai", "batal"}, str(x, "status"))) {
				continue
			}
			items = append(items, h.taskView(t, x))
		}
		return page(r, items), nil
	})
}
func (h *LiveHandler) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		p, e := h.pondAccess(t, getUserID(r), str(b, "pond_id"), true)
		if e != nil {
			return nil, e
		}
		if e = requireText(b, "title", "instruction", "due_at", "assignee_id"); e != nil {
			return nil, e
		}
		if str(p, "assignee_id") != str(b, "assignee_id") {
			return nil, invalid("assignee_id", "Pilih teknisi pemegang kolam ini.")
		}
		if !has([]string{"tindakan", "ambil_sampel", "catat_air"}, str(b, "kind")) {
			return nil, invalid("kind", "Pilih jenis tugas yang tersedia.")
		}
		if timestamp(b["due_at"]).IsZero() {
			return nil, invalid("due_at", "Gunakan waktu dengan zona yang valid.")
		}
		task := rec{"id": generateUUID(), "farm_id": p["farm_id"], "pond_id": p["id"], "assignee_id": b["assignee_id"], "created_by": getUserID(r), "kind": b["kind"], "title": b["title"], "instruction": b["instruction"], "due_at": b["due_at"], "status": "baru", "owner_note": nil, "source_sample_id": b["source_sample_id"], "source_alert_id": b["source_alert_id"], "created_at": now()}
		if e = h.validateTaskSources(t, p, task); e != nil {
			return nil, e
		}
		if e = t.Put("tasks", task); e != nil {
			return nil, e
		}
		if e = h.history(t, getUserID(r), str(task, "id"), "baru", nil); e != nil {
			return nil, e
		}
		if e = h.notify(t, str(b, "assignee_id"), "tugas_baru", "Tugas baru: "+str(b, "title"), "task", str(task, "id")); e != nil {
			return nil, e
		}
		return h.taskView(t, task), nil
	})
}
func (h *LiveHandler) validateTaskSources(t tx, p, task rec) error {
	if id := str(task, "source_sample_id"); id != "" {
		s, e := t.Get("samples", id)
		if e != nil {
			return e
		}
		if str(s, "pond_id") != str(p, "id") {
			return forbidden()
		}
	}
	if id := str(task, "source_alert_id"); id != "" {
		a, e := t.Get("alerts", id)
		if e != nil {
			return e
		}
		if str(a, "pond_id") != str(p, "id") {
			return forbidden()
		}
	}
	return nil
}
func (h *LiveHandler) handleGetTaskDetail(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		task, e := h.taskAccess(t, getUserID(r), r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		out := h.taskView(t, task)
		reports, e := find(t, "task_reports", "task_id", str(task, "id"))
		if e != nil {
			return nil, e
		}
		items := []rec{}
		for _, x := range reports {
			x = clone(x)
			x["reported_by"] = ref(t, str(x, "reported_by"))
			items = append(items, x)
		}
		hist, e := find(t, "task_history", "task_id", str(task, "id"))
		if e != nil {
			return nil, e
		}
		hs := []rec{}
		for _, x := range hist {
			hs = append(hs, rec{"status": x["status"], "by": ref(t, str(x, "user_id")), "note": x["note"], "at": x["created_at"]})
		}
		out["reports"] = items
		out["history"] = hs
		return out, nil
	})
}
func (h *LiveHandler) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		task, e := h.taskAccess(t, getUserID(r), r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		f, e := t.Get("farms", str(task, "farm_id"))
		if e != nil {
			return nil, e
		}
		owner := str(f, "owner_id") == getUserID(r)
		status := str(b, "status")
		current := str(task, "status")
		if owner {
			if has([]string{"selesai", "batal"}, current) {
				return nil, invalid("status", "Tugas sudah ditutup.")
			}
			if status != "" && !has([]string{"selesai", "diminta_ulang", "batal"}, status) {
				return nil, invalid("status", "Pilih status tugas yang tersedia.")
			}
			if status == "diminta_ulang" && str(b, "owner_note") == "" {
				return nil, invalid("owner_note", "Jelaskan bagian yang perlu diulang.")
			}
			if (status == "selesai" || status == "diminta_ulang") && current != "dilaporkan" {
				return nil, invalid("status", "Tunggu laporan teknisi sebelum menilai tugas.")
			}
			for _, k := range []string{"title", "instruction", "due_at", "assignee_id"} {
				if _, ok := b[k]; ok && current == "dilaporkan" {
					return nil, invalid(k, "Tugas yang sudah dilaporkan tidak dapat diubah.")
				}
			}
			mergeAllowed(task, b, "title", "instruction", "due_at", "assignee_id", "owner_note")
			p, e := t.Get("ponds", str(task, "pond_id"))
			if e != nil {
				return nil, e
			}
			if str(task, "assignee_id") != str(p, "assignee_id") {
				return nil, invalid("assignee_id", "Pilih teknisi pemegang kolam ini.")
			}
			if timestamp(task["due_at"]).IsZero() {
				return nil, invalid("due_at", "Waktu tidak valid.")
			}
		} else {
			for k := range b {
				if k != "status" {
					return nil, forbidden()
				}
			}
			if status != "dikerjakan" || !has([]string{"baru", "diminta_ulang"}, current) {
				return nil, invalid("status", "Tugas ini belum dapat dikerjakan.")
			}
		}
		if status != "" {
			task["status"] = status
			if e = h.history(t, getUserID(r), str(task, "id"), status, b["owner_note"]); e != nil {
				return nil, e
			}
		}
		if e = t.Put("tasks", task); e != nil {
			return nil, e
		}
		return h.taskView(t, task), nil
	})
}
func (h *LiveHandler) handleCreateTaskReport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if e := r.ParseMultipartForm(5 << 20); e != nil {
		h.fail(w, invalid("photos", "Laporan terlalu besar atau format tidak valid."))
		return
	}
	defer r.MultipartForm.RemoveAll()
	note := r.FormValue("note")
	if note == "" {
		h.fail(w, invalid("note", "Catatan laporan wajib diisi."))
		return
	}
	var measured map[string]float64
	if raw := r.FormValue("measured"); raw != "" && raw != "null" {
		if e := json.Unmarshal([]byte(raw), &measured); e != nil {
			h.fail(w, invalid("measured", "Kirim angka pengukuran dalam JSON yang valid."))
			return
		}
	}
	headers := append(r.MultipartForm.File["photos"], r.MultipartForm.File["photos[]"]...)
	if len(headers) > 4 {
		h.fail(w, invalid("photos", "Maksimal 4 foto per laporan."))
		return
	}
	paths := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, p := range paths {
				_ = os.Remove(p)
			}
		}
	}()
	var out rec
	err := h.store.Write(r.Context(), func(t tx) error {
		task, e := h.taskAccess(t, getUserID(r), r.PathValue("id"))
		if e != nil {
			return e
		}
		if str(task, "assignee_id") != getUserID(r) {
			return forbidden()
		}
		if !has([]string{"baru", "dikerjakan", "diminta_ulang"}, str(task, "status")) {
			return invalid("status", "Tugas sudah dilaporkan atau ditutup.")
		}
		photos := []rec{}
		for _, header := range headers {
			if header.Size > 1<<20 {
				return invalid("photos", "Setiap foto maksimal 1 MB.")
			}
			f, e := header.Open()
			if e != nil {
				return e
			}
			data, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
			_ = f.Close()
			if e != nil {
				return e
			}
			if len(data) > 1<<20 {
				return invalid("photos", "Setiap foto maksimal 1 MB.")
			}
			cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
			if e != nil || !has([]string{"jpeg", "png"}, format) || cfg.Width*cfg.Height > 40000000 {
				return invalid("photos", "Unggah gambar JPEG atau PNG yang valid.")
			}
			id := generateUUID()
			path := filepath.Join(h.cfg.FilesDir, id)
			if e = os.WriteFile(path, data, 0600); e != nil {
				return e
			}
			paths = append(paths, path)
			if e = t.Put("files", rec{"id": id, "farm_id": task["farm_id"], "pond_id": task["pond_id"], "uploader_id": getUserID(r), "file_name": header.Filename, "file_path": path, "file_size": len(data), "mime_type": "image/" + format, "created_at": now()}); e != nil {
				return e
			}
			photos = append(photos, rec{"id": id, "url": "/api/v1/files/" + id})
		}
		report := rec{"id": generateUUID(), "task_id": task["id"], "note": note, "measured": measured, "photos": photos, "reported_by": getUserID(r), "reported_at": now()}
		if e = t.Put("task_reports", report); e != nil {
			return e
		}
		task["status"] = "dilaporkan"
		if e = t.Put("tasks", task); e != nil {
			return e
		}
		if e = h.history(t, getUserID(r), str(task, "id"), "dilaporkan", note); e != nil {
			return e
		}
		farm, e := t.Get("farms", str(task, "farm_id"))
		if e != nil {
			return e
		}
		if e = h.notify(t, str(farm, "owner_id"), "laporan_masuk", "Laporan tugas masuk", "task", str(task, "id")); e != nil {
			return e
		}
		out = clone(report)
		out["reported_by"] = ref(t, getUserID(r))
		return nil
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	committed = true
	writeSuccess(w, 200, out)
}
func (h *LiveHandler) handleGetFile(w http.ResponseWriter, r *http.Request) {
	var file rec
	e := h.store.Read(r.Context(), func(t tx) error {
		var e error
		file, e = t.Get("files", r.PathValue("id"))
		if e != nil {
			return e
		}
		if _, e = h.pondAccess(t, getUserID(r), str(file, "pond_id"), false); e != nil {
			return e
		}
		return nil
	})
	if e != nil {
		h.fail(w, e)
		return
	}
	filePath := str(file, "file_path")
	absDir, errDir := filepath.Abs(h.cfg.FilesDir)
	absPath, errPath := filepath.Abs(filePath)
	if errDir != nil || errPath != nil {
		h.fail(w, forbidden())
		return
	}
	rel, errRel := filepath.Rel(absDir, absPath)
	if errRel != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)) {
		h.fail(w, forbidden())
		return
	}
	root, err := os.OpenRoot(absDir)
	if err != nil {
		h.fail(w, repository.ErrNotFound)
		return
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		h.fail(w, repository.ErrNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		h.fail(w, repository.ErrNotFound)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Type", str(file, "mime_type"))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, str(file, "file_name"), info.ModTime(), f)
}
func (h *LiveHandler) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		fid := r.PathValue("id")
		if _, e := h.farmAccess(t, getUserID(r), fid, true); e != nil {
			return nil, e
		}
		rows, e := find(t, "alerts", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		items := []rec{}
		for _, x := range rows {
			if x["acked_at"] != nil {
				continue
			}
			p, e := t.Get("ponds", str(x, "pond_id"))
			if e != nil {
				return nil, e
			}
			if p["archived_at"] != nil {
				continue
			}
			out := clone(x)
			out["pond_code"] = p["pond_code"]
			out["since"] = x["created_at"]
			items = append(items, out)
		}
		return rec{"items": items}, nil
	})
}
func (h *LiveHandler) handleAckAlert(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, true, func(t tx) (any, error) {
		a, e := t.Get("alerts", r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		if _, e = h.farmAccess(t, getUserID(r), str(a, "farm_id"), true); e != nil {
			return nil, e
		}
		a["acked_at"] = now()
		a["acked_by"] = getUserID(r)
		if e = t.Put("alerts", a); e != nil {
			return nil, e
		}
		p, e := t.Get("ponds", str(a, "pond_id"))
		if e != nil {
			return nil, e
		}
		a["pond_code"] = p["pond_code"]
		a["since"] = a["created_at"]
		return a, nil
	})
}
func (h *LiveHandler) handleGetNotifications(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		user := getUserID(r)
		rows, e := find(t, "notifications", "user_id", user)
		if e != nil {
			return nil, e
		}
		if rows == nil {
			rows = []rec{}
		}
		unread := 0
		for _, x := range rows {
			if x["read_at"] == nil {
				unread++
			}
		}
		return rec{"items": rows, "unread": unread}, nil
	})
}
func (h *LiveHandler) handleReadNotifications(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		rows, e := find(t, "notifications", "user_id", getUserID(r))
		if e != nil {
			return nil, e
		}
		for _, x := range rows {
			if boolean(b, "all") || has(stringsOf(b["ids"]), str(x, "id")) {
				x["read_at"] = now()
				if e = t.Put("notifications", x); e != nil {
					return nil, e
				}
			}
		}
		return rec{}, nil
	})
}
func (h *LiveHandler) handleGetActivity(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		fid := r.PathValue("id")
		if _, e := h.farmAccess(t, getUserID(r), fid, true); e != nil {
			return nil, e
		}
		rows, e := find(t, "activity_log", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		items := []rec{}
		for _, x := range rows {
			items = append(items, rec{"actor": ref(t, str(x, "actor_id")), "action": x["action"], "entity": x["entity"], "entity_id": x["entity_id"], "detail": x["detail"], "at": x["created_at"]})
		}
		return page(r, items), nil
	})
}

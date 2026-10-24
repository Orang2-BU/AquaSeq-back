package handler

import (
	"net/http"
	"strings"
	"time"

	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/repository"
	"aquaseq-back/internal/trajectory"
)

func (h *LiveHandler) pondSummary(t tx, p rec) (rec, error) {
	out := clone(p)
	fid := str(p, "farm_id")
	pid := str(p, "id")
	if p["name"] == nil {
		out["name"] = nil
	}
	if p["area_sqm"] == nil {
		out["area_sqm"] = nil
	}
	if p["stocked_at"] == nil {
		out["stocked_at"] = nil
	}
	if p["wa_target"] == nil {
		out["wa_target"] = nil
	}
	out["assignee"] = nil
	if str(p, "assignee_id") != "" {
		out["assignee"] = ref(t, str(p, "assignee_id"))
	}
	samples, e := t.Find("samples", func(r rec) bool { return str(r, "pond_id") == pid })
	if e != nil {
		return nil, e
	}
	var latestSample rec
	var latestDate string
	var history []domain.TrajectoryPoint
	for _, s := range samples {
		res, e := first(t, "sdri_results", "sample_id", str(s, "id"))
		if e != nil {
			continue
		}
		d := str(s, "sampling_date")
		history = append(history, domain.TrajectoryPoint{SamplingDate: d, CycleDay: int(num(s, "cycle_day")), ProbabilityPct: num(res, "probability_pct"), RiskClass: int(num(res, "risk_class"))})
		if d > latestDate {
			latestDate = d
			latestSample = s
		}
	}
	out["latest_sampling_date"] = nil
	out["latest_result"] = nil
	out["trend"] = "INSUFFICIENT_DATA"
	if latestDate != "" {
		out["latest_sampling_date"] = latestDate
		res, _ := first(t, "sdri_results", "sample_id", str(latestSample, "id"))
		if res != nil {
			out["latest_result"] = res
		}
	}
	if len(history) > 0 {
		out["trend"] = trajectory.EvaluateTrajectory(pid, history).Trend
	}
	_ = fid
	return out, nil
}
func (h *LiveHandler) handleGetFarms(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		user := getUserID(r)
		farms, e := t.List("farms")
		if e != nil {
			return nil, e
		}
		items := []rec{}
		for _, f := range farms {
			fid := str(f, "id")
			owner := str(f, "owner_id") == user
			if !owner {
				m, e := find(t, "memberships", "farm_id", fid)
				if e != nil {
					return nil, e
				}
				ok := false
				for _, x := range m {
					if str(x, "user_id") == user && boolean(x, "active") {
						ok = true
						break
					}
				}
				if !ok {
					continue
				}
			}
			ponds, e := find(t, "ponds", "farm_id", fid)
			if e != nil {
				return nil, e
			}
			var pondCount, atRiskCount int
			for _, p := range ponds {
				if p["archived_at"] != nil {
					continue
				}
				pondCount++
				smps, e := find(t, "samples", "pond_id", str(p, "id"))
				if e != nil {
					continue
				}
				var ld string
				var lr rec
				for _, s := range smps {
					d := str(s, "sampling_date")
					if d > ld {
						ld = d
						lr, _ = first(t, "sdri_results", "sample_id", str(s, "id"))
					}
				}
				if lr != nil && num(lr, "risk_class") >= 3 {
					atRiskCount++
				}
			}
			members, e := find(t, "memberships", "farm_id", fid)
			if e != nil {
				return nil, e
			}
			techCount := 0
			for _, m := range members {
				if str(m, "role") == "teknisi" && boolean(m, "active") {
					techCount++
				}
			}
			item := rec{"id": f["id"], "name": f["name"], "regency": f["regency"], "address": f["address"], "provenance": f["provenance"], "archived_at": f["archived_at"], "created_at": f["created_at"], "pond_count": pondCount, "technician_count": techCount, "at_risk_count": atRiskCount}
			items = append(items, item)
		}
		return rec{"items": items}, nil
	})
}
func (h *LiveHandler) handleCreateFarm(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		if e := h.canCreateFarm(t, getUserID(r)); e != nil {
			return nil, e
		}
		if e := requireText(b, "name", "regency"); e != nil {
			return nil, e
		}
		id := generateUUID()
		f := rec{"id": id, "name": str(b, "name"), "regency": str(b, "regency"), "address": b["address"], "owner_id": getUserID(r), "provenance": "nyata", "archived_at": nil, "created_at": now()}
		if e := t.Put("farms", f); e != nil {
			return nil, e
		}
		if e := t.Put("memberships", rec{"id": generateUUID(), "farm_id": id, "user_id": getUserID(r), "role": "pemilik", "active": true, "created_at": now()}); e != nil {
			return nil, e
		}
		_ = h.activity(t, getUserID(r), id, "TAMBAH_LOKASI", "farm", id, "Lokasi "+str(b, "name")+" dibuat.")
		return rec{"id": f["id"], "name": f["name"], "regency": f["regency"], "address": f["address"], "provenance": f["provenance"], "archived_at": f["archived_at"], "created_at": f["created_at"], "pond_count": 0, "technician_count": 0, "at_risk_count": 0}, nil
	})
}
func (h *LiveHandler) handlePatchFarm(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		f, e := h.farmAccess(t, getUserID(r), r.PathValue("id"), true)
		if e != nil {
			return nil, e
		}
		mergeAllowed(f, b, "name", "regency", "address")
		if e := requireText(f, "name", "regency"); e != nil {
			return nil, e
		}
		if e := t.Put("farms", f); e != nil {
			return nil, e
		}
		return h.farmSummary(t, f)
	})
}
func (h *LiveHandler) handleArchiveFarm(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, true, func(t tx) (any, error) {
		f, e := h.farmAccess(t, getUserID(r), r.PathValue("id"), true)
		if e != nil {
			return nil, e
		}
		f["archived_at"] = now()
		if e := t.Put("farms", f); e != nil {
			return nil, e
		}
		return h.farmSummary(t, f)
	})
}
func (h *LiveHandler) handleGetFarmPonds(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		fid := r.PathValue("id")
		f, e := h.farmAccess(t, getUserID(r), fid, false)
		if e != nil {
			return nil, e
		}
		owner := str(f, "owner_id") == getUserID(r)
		ponds, e := find(t, "ponds", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		status := r.URL.Query().Get("status")
		assignee := r.URL.Query().Get("assignee_id")
		items := []rec{}
		for _, p := range ponds {
			if status != "" && str(p, "status") != status {
				continue
			}
			if status == "" && p["archived_at"] != nil {
				continue
			}
			if !owner && str(p, "assignee_id") != getUserID(r) {
				continue
			}
			if assignee != "" && str(p, "assignee_id") != assignee {
				continue
			}
			sum, e := h.pondSummary(t, p)
			if e != nil {
				return nil, e
			}
			items = append(items, sum)
		}
		return rec{"items": items}, nil
	})
}
func (h *LiveHandler) handleCreatePond(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		fid := r.PathValue("id")
		_, e := h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}
		code := strings.ToUpper(strings.TrimSpace(str(b, "pond_code")))
		if !pondPattern.MatchString(code) {
			return nil, invalid("pond_code", "Gunakan 1-12 karakter huruf besar, angka, atau tanda minus.")
		}
		existing, e := find(t, "ponds", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		for _, p := range existing {
			if str(p, "pond_code") == code {
				return nil, &requestError{409, "CONFLICT", "Kode kolam sudah digunakan di lokasi ini.", map[string]string{"pond_code": "Gunakan kode kolam lain."}}
			}
		}
		sys := str(b, "system")
		if !has([]string{"intensif", "bioflok"}, sys) {
			return nil, invalid("system", "Pilih sistem intensif atau bioflok.")
		}
		p := rec{"id": generateUUID(), "farm_id": fid, "pond_code": code, "name": b["name"], "system": sys, "area_sqm": b["area_sqm"], "current_cycle": 1, "stocked_at": b["stocked_at"], "assignee_id": b["assignee_id"], "wa_target": b["wa_target"], "status": "ACTIVE", "archived_at": nil, "created_at": now()}
		if e := h.validatePond(t, p); e != nil {
			return nil, e
		}
		if e := t.Put("ponds", p); e != nil {
			return nil, e
		}
		return h.pondSummary(t, p)
	})
}
func (h *LiveHandler) handleGetPondDetail(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		p, e := h.pondAccess(t, getUserID(r), r.PathValue("id"), false)
		if e != nil {
			return nil, e
		}
		sum, e := h.pondSummary(t, p)
		if e != nil {
			return nil, e
		}
		wlogs, e := find(t, "water_logs", "pond_id", str(p, "id"))
		if e != nil {
			return nil, e
		}
		var latestLog rec
		var ld string
		for _, wl := range wlogs {
			d := str(wl, "measured_at")
			if d > ld {
				ld = d
				latestLog = wl
			}
		}
		sum["latest_water_log"] = nil
		if latestLog != nil {
			wout := clone(latestLog)
			wout["pond"] = pondRef(t, str(p, "id"))
			wout["recorded_by"] = ref(t, str(latestLog, "recorded_by"))
			sum["latest_water_log"] = wout
		}
		return sum, nil
	})
}
func (h *LiveHandler) handlePatchPond(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		pid := r.PathValue("id")
		p, e := h.pondAccess(t, getUserID(r), pid, true)
		if e != nil {
			return nil, e
		}
		fid := str(p, "farm_id")
		if b["pond_code"] != nil {
			code := strings.ToUpper(strings.TrimSpace(str(b, "pond_code")))
			if !pondPattern.MatchString(code) {
				return nil, invalid("pond_code", "Gunakan 1-12 karakter huruf besar, angka, atau minus.")
			}
			all, e := find(t, "ponds", "farm_id", fid)
			if e != nil {
				return nil, e
			}
			for _, x := range all {
				if str(x, "id") != pid && str(x, "pond_code") == code {
					return nil, &requestError{409, "CONFLICT", "Kode kolam sudah digunakan di lokasi ini.", map[string]string{"pond_code": "Gunakan kode kolam lain."}}
				}
			}
			p["pond_code"] = code
		}
		mergeAllowed(p, b, "name", "system", "area_sqm", "assignee_id", "wa_target")
		if e := h.validatePond(t, p); e != nil {
			return nil, e
		}
		if e := t.Put("ponds", p); e != nil {
			return nil, e
		}
		return h.pondSummary(t, p)
	})
}
func (h *LiveHandler) handleArchivePond(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, true, func(t tx) (any, error) {
		p, e := h.pondAccess(t, getUserID(r), r.PathValue("id"), true)
		if e != nil {
			return nil, e
		}
		p["archived_at"] = now()
		p["status"] = "ARCHIVED"
		p["assignee_id"] = nil
		if e := t.Put("ponds", p); e != nil {
			return nil, e
		}
		return h.pondSummary(t, p)
	})
}
func (h *LiveHandler) handleGetMembers(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		fid := r.PathValue("id")
		_, e := h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}
		mems, e := find(t, "memberships", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		ponds, e := find(t, "ponds", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		tasks, e := find(t, "tasks", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		memberItems := []rec{}
		for _, m := range mems {
			uid := str(m, "user_id")
			u, e := t.Get("users", uid)
			if e != nil {
				continue
			}
			codes := []string{}
			for _, p := range ponds {
				if str(p, "assignee_id") == uid && p["archived_at"] == nil {
					codes = append(codes, str(p, "pond_code"))
				}
			}
			openTasks := 0
			for _, tsk := range tasks {
				if str(tsk, "assignee_id") == uid && has([]string{"baru", "dikerjakan", "diminta_ulang"}, str(tsk, "status")) {
					openTasks++
				}
			}
			memberItems = append(memberItems, rec{"user_id": uid, "full_name": u["full_name"], "email": u["email"], "role": m["role"], "active": boolean(m, "active"), "pond_codes": codes, "open_tasks": openTasks})
		}
		invs, e := find(t, "invitations", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		pendingInvs := []rec{}
		for _, inv := range invs {
			if inv["accepted_at"] != nil {
				continue
			}
			pondIDs := stringsOf(inv["pond_ids"])
			codes := []string{}
			for _, pid := range pondIDs {
				p, e := t.Get("ponds", pid)
				if e == nil {
					codes = append(codes, str(p, "pond_code"))
				}
			}
			pendingInvs = append(pendingInvs, rec{"id": inv["id"], "email": inv["email"], "pond_codes": codes, "expires_at": inv["expires_at"]})
		}
		return rec{"items": memberItems, "invitations": pendingInvs}, nil
	})
}
func (h *LiveHandler) handlePatchMember(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		fid := r.PathValue("id")
		uid := r.PathValue("user_id")
		_, e := h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}
		mems, e := find(t, "memberships", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		var target rec
		for _, m := range mems {
			if str(m, "user_id") == uid {
				target = m
				break
			}
		}
		if target == nil {
			return nil, repository.ErrNotFound
		}
		if b["active"] != nil {
			active, ok := b["active"].(bool)
			if !ok {
				return nil, invalid("active", "Gunakan nilai benar atau salah.")
			}
			if str(target, "role") != "teknisi" {
				return nil, invalid("active", "Pemilik tidak dapat dinonaktifkan.")
			}
			target["active"] = active
			if !active {
				ponds, e := find(t, "ponds", "farm_id", fid)
				if e != nil {
					return nil, e
				}
				for _, p := range ponds {
					if str(p, "assignee_id") == uid {
						p["assignee_id"] = nil
						_ = t.Put("ponds", p)
					}
				}
			}
		}
		if e := t.Put("memberships", target); e != nil {
			return nil, e
		}
		u, _ := t.Get("users", uid)
		return rec{"user_id": uid, "full_name": u["full_name"], "email": u["email"], "role": target["role"], "active": boolean(target, "active"), "pond_codes": []string{}, "open_tasks": 0}, nil
	})
}
func (h *LiveHandler) handlePutMemberPonds(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		fid := r.PathValue("id")
		uid := r.PathValue("user_id")
		_, e := h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}
		mems, e := find(t, "memberships", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		var target rec
		for _, m := range mems {
			if str(m, "user_id") == uid {
				target = m
				break
			}
		}
		if target == nil || !boolean(target, "active") {
			return nil, invalid("user_id", "Teknisi tidak aktif atau tidak terdaftar.")
		}
		selectedIDs := stringsOf(b["pond_ids"])
		if e := h.validatePondIDs(t, fid, selectedIDs); e != nil {
			return nil, e
		}
		ponds, e := find(t, "ponds", "farm_id", fid)
		if e != nil {
			return nil, e
		}
		assignedCodes := []string{}
		for _, p := range ponds {
			pid := str(p, "id")
			if has(selectedIDs, pid) {
				p["assignee_id"] = uid
				assignedCodes = append(assignedCodes, str(p, "pond_code"))
			} else if str(p, "assignee_id") == uid {
				p["assignee_id"] = nil
			}
			if e := t.Put("ponds", p); e != nil {
				return nil, e
			}
		}
		u, _ := t.Get("users", uid)
		return rec{"user_id": uid, "full_name": u["full_name"], "email": u["email"], "role": target["role"], "active": boolean(target, "active"), "pond_codes": assignedCodes, "open_tasks": 0}, nil
	})
}
func (h *LiveHandler) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		fid := r.PathValue("id")
		_, e := h.farmAccess(t, getUserID(r), fid, true)
		if e != nil {
			return nil, e
		}
		email := strings.ToLower(strings.TrimSpace(str(b, "email")))
		if !emailValid(email) {
			return nil, invalid("email", "Alamat email tidak valid.")
		}
		raw, hash, e := auth.GenerateTokenString()
		if e != nil {
			return nil, e
		}
		pondIDs := stringsOf(b["pond_ids"])
		if e := h.validatePondIDs(t, fid, pondIDs); e != nil {
			return nil, e
		}
		exp := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
		id := generateUUID()
		inv := rec{"id": id, "farm_id": fid, "email": email, "token_hash": hash, "pond_ids": pondIDs, "expires_at": exp, "accepted_at": nil, "created_at": now()}
		if e := t.Put("invitations", inv); e != nil {
			return nil, e
		}
		link := strings.TrimRight(h.cfg.WebURL, "/") + "/undangan/" + raw
		h.sendLink(email, "Undangan Bergabung ke AquaSeq", link)
		return rec{"id": id, "email": email, "expires_at": exp}, nil
	})
}
func (h *LiveHandler) handleDeleteInvitation(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, true, func(t tx) (any, error) {
		id := r.PathValue("id")
		inv, e := t.Get("invitations", id)
		if e != nil {
			return nil, e
		}
		_, e = h.farmAccess(t, getUserID(r), str(inv, "farm_id"), true)
		if e != nil {
			return nil, e
		}
		e = t.Delete("invitations", id)
		return rec{}, e
	})
}
func (h *LiveHandler) handleInvitationByToken(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		tok := r.PathValue("token")
		inv, e := first(t, "invitations", "token_hash", auth.HashToken(tok))
		if e != nil || inv["accepted_at"] != nil {
			return nil, repository.ErrNotFound
		}
		farm, e := t.Get("farms", str(inv, "farm_id"))
		if e != nil {
			return nil, e
		}
		owner, e := t.Get("users", str(farm, "owner_id"))
		if e != nil {
			return nil, e
		}
		pondIDs := stringsOf(inv["pond_ids"])
		codes := []string{}
		for _, pid := range pondIDs {
			p, e := t.Get("ponds", pid)
			if e == nil {
				codes = append(codes, str(p, "pond_code"))
			}
		}
		exp := timestamp(inv["expires_at"]).Before(time.Now())
		return rec{"farm_name": farm["name"], "owner_name": owner["full_name"], "email": inv["email"], "pond_codes": codes, "expired": exp}, nil
	})
}
func (h *LiveHandler) handleInvitationAccept(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		tok := r.PathValue("token")
		inv, e := first(t, "invitations", "token_hash", auth.HashToken(tok))
		if e != nil || inv["accepted_at"] != nil {
			return nil, repository.ErrNotFound
		}
		if timestamp(inv["expires_at"]).Before(time.Now()) {
			return nil, &requestError{410, "EXPIRED", "Tautan undangan telah kedaluwarsa. Minta pemilik mengirim ulang undangan.", nil}
		}
		email := str(inv, "email")
		if _, e := first(t, "users", "email", email); e == nil {
			return nil, &requestError{409, "CONFLICT", "Email sudah memiliki akun di sistem.", map[string]string{"email": "Masuk dengan akun yang sudah ada."}}
		}
		if !passwordValid(str(b, "password")) {
			return nil, invalid("password", "Gunakan 10 sampai 72 karakter.")
		}
		if e := requireText(b, "full_name"); e != nil {
			return nil, e
		}
		pwHash, e := auth.HashPassword(str(b, "password"))
		if e != nil {
			return nil, e
		}
		newUserID := generateUUID()
		userRec := rec{"id": newUserID, "full_name": str(b, "full_name"), "email": email, "password_hash": pwHash, "created_at": now()}
		if e := t.Put("users", userRec); e != nil {
			return nil, e
		}
		fid := str(inv, "farm_id")
		if e := t.Put("memberships", rec{"id": generateUUID(), "farm_id": fid, "user_id": newUserID, "role": "teknisi", "active": true, "created_at": now()}); e != nil {
			return nil, e
		}
		pondIDs := stringsOf(inv["pond_ids"])
		for _, pid := range pondIDs {
			p, e := t.Get("ponds", pid)
			if e == nil && str(p, "farm_id") == fid {
				p["assignee_id"] = newUserID
				_ = t.Put("ponds", p)
			}
		}
		inv["accepted_at"] = now()
		if e := t.Put("invitations", inv); e != nil {
			return nil, e
		}
		return h.tokens(t, userRec)
	})
}

func (h *LiveHandler) canCreateFarm(t tx, user string) error {
	ms, e := find(t, "memberships", "user_id", user)
	if e != nil {
		return e
	}
	tech, owner := false, false
	for _, m := range ms {
		if str(m, "role") == "pemilik" {
			owner = true
		}
		if str(m, "role") == "teknisi" {
			tech = true
		}
	}
	if tech && !owner {
		return forbidden()
	}
	return nil
}
func (h *LiveHandler) validatePond(t tx, p rec) error {
	if !has([]string{"intensif", "bioflok"}, str(p, "system")) {
		return invalid("system", "Pilih sistem intensif atau bioflok.")
	}
	if p["area_sqm"] != nil {
		area, ok := p["area_sqm"].(float64)
		if !ok || area <= 0 || area > 1000000 {
			return invalid("area_sqm", "Masukkan luas lebih dari nol hingga 1000000 m2.")
		}
	}
	if p["wa_target"] != nil && !phonePattern.MatchString(str(p, "wa_target")) {
		return invalid("wa_target", "Gunakan nomor berawalan 62.")
	}
	if p["stocked_at"] != nil {
		if _, e := time.Parse("2006-01-02", str(p, "stocked_at")); e != nil {
			return invalid("stocked_at", "Gunakan tanggal YYYY-MM-DD.")
		}
	}
	uid := str(p, "assignee_id")
	if uid != "" {
		ms, e := find(t, "memberships", "farm_id", str(p, "farm_id"))
		if e != nil {
			return e
		}
		valid := false
		for _, m := range ms {
			if str(m, "user_id") == uid && str(m, "role") == "teknisi" && boolean(m, "active") {
				valid = true
			}
		}
		if !valid {
			return invalid("assignee_id", "Pilih teknisi aktif pada lokasi ini.")
		}
	}
	return nil
}
func (h *LiveHandler) validatePondIDs(t tx, fid string, ids []string) error {
	if len(ids) > 50 {
		return invalid("pond_ids", "Maksimal 50 kolam.")
	}
	for _, id := range ids {
		p, e := t.Get("ponds", id)
		if e != nil {
			return invalid("pond_ids", "Kolam tidak ditemukan.")
		}
		if str(p, "farm_id") != fid || p["archived_at"] != nil {
			return invalid("pond_ids", "Pilih kolam aktif di lokasi ini.")
		}
	}
	return nil
}
func (h *LiveHandler) farmSummary(t tx, f rec) (rec, error) {
	out := clone(f)
	ps, e := find(t, "ponds", "farm_id", str(f, "id"))
	if e != nil {
		return nil, e
	}
	count, risk := 0, 0
	for _, p := range ps {
		if p["archived_at"] != nil {
			continue
		}
		count++
		summary, e := h.pondSummary(t, p)
		if e != nil {
			return nil, e
		}
		if num(object(summary["latest_result"]), "risk_class") >= 3 {
			risk++
		}
	}
	ms, e := find(t, "memberships", "farm_id", str(f, "id"))
	if e != nil {
		return nil, e
	}
	tech := 0
	for _, m := range ms {
		if str(m, "role") == "teknisi" && boolean(m, "active") {
			tech++
		}
	}
	out["pond_count"] = count
	out["at_risk_count"] = risk
	out["technician_count"] = tech
	return out, nil
}

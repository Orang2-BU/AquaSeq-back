package handler

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/repository"
)

func (h *LiveHandler) tokens(t tx, u rec) (rec, error) {
	return h.familyTokens(t, u, "")
}
func (h *LiveHandler) familyTokens(t tx, u rec, family string) (rec, error) {
	access, e := h.cfg.Auth.Access(str(u, "id"))
	if e != nil {
		return nil, e
	}
	plain, hash, e := auth.GenerateTokenString()
	if e != nil {
		return nil, e
	}
	id := generateUUID()
	if family == "" {
		family = id
	}
	if e = t.Put("refresh_tokens", rec{"id": id, "family_id": family, "user_id": u["id"], "token_hash": hash, "expires_at": time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339), "revoked_at": nil, "created_at": now()}); e != nil {
		return nil, e
	}
	return rec{"user": userView(u), "access_token": access, "refresh_token": plain, "expires_in": 900}, nil
}
func (h *LiveHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		if e := requireText(b, "full_name", "email", "password"); e != nil {
			return nil, e
		}
		email := strings.ToLower(strings.TrimSpace(str(b, "email")))
		if !emailValid(email) {
			return nil, invalid("email", "Alamat email tidak valid.")
		}
		if !passwordValid(str(b, "password")) {
			return nil, invalid("password", "Gunakan 10 sampai 72 karakter.")
		}
		if b["phone_wa"] != nil && !phonePattern.MatchString(str(b, "phone_wa")) {
			return nil, invalid("phone_wa", "Gunakan nomor berawalan 62.")
		}
		if _, e := first(t, "users", "email", email); e == nil {
			return nil, &requestError{409, "CONFLICT", "Email sudah terdaftar.", map[string]string{"email": "Gunakan email lain atau masuk."}}
		} else if !errors.Is(e, repository.ErrNotFound) {
			return nil, e
		}
		hash, e := auth.HashPassword(str(b, "password"))
		if e != nil {
			return nil, e
		}
		u := rec{"id": generateUUID(), "full_name": str(b, "full_name"), "email": email, "phone_wa": b["phone_wa"], "password_hash": hash, "created_at": now()}
		if e = t.Put("users", u); e != nil {
			return nil, e
		}
		return h.tokens(t, u)
	})
}
func (h *LiveHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	email := strings.ToLower(strings.TrimSpace(str(b, "email")))
	if !h.limiter.Allow("ip:"+ip) || !h.limiter.Allow("email:"+email) {
		writeErrorWithCode(w, 429, "RATE_LIMITED", "Terlalu banyak percobaan. Coba lagi satu menit kemudian.", nil)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		u, e := first(t, "users", "email", email)
		if e != nil || !auth.CheckPassword(str(u, "password_hash"), str(b, "password")) {
			return nil, unauthorized()
		}
		return h.tokens(t, u)
	})
}
func (h *LiveHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	var out rec
	rejected := false
	err := h.store.Write(r.Context(), func(t tx) error {
		token, e := first(t, "refresh_tokens", "token_hash", auth.HashToken(str(b, "refresh_token")))
		if errors.Is(e, repository.ErrNotFound) {
			rejected = true
			return nil
		}
		if e != nil {
			return e
		}
		family := str(token, "family_id")
		if family == "" {
			family = str(token, "id")
		}
		// Lock the family root before reading its current token; unrelated sessions do not contend.
		if _, e = t.Get("refresh_tokens", family); e != nil {
			return e
		}
		token, e = t.Get("refresh_tokens", str(token, "id"))
		if e != nil {
			return e
		}
		if token["revoked_at"] != nil {
			rows, e := find(t, "refresh_tokens", "family_id", family)
			if e != nil {
				return e
			}
			for _, row := range rows {
				row["revoked_at"] = now()
				if e = t.Put("refresh_tokens", row); e != nil {
					return e
				}
			}
			rejected = true
			return nil
		}
		if !timestamp(token["expires_at"]).After(time.Now()) {
			rejected = true
			return nil
		}
		token["revoked_at"] = now()
		if e = t.Put("refresh_tokens", token); e != nil {
			return e
		}
		u, e := t.Get("users", str(token, "user_id"))
		if e != nil {
			return e
		}
		out, e = h.familyTokens(t, u, family)
		delete(out, "user")
		return e
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	if rejected {
		h.fail(w, unauthorized())
		return
	}
	writeSuccess(w, 200, out)
}
func (h *LiveHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		token, e := first(t, "refresh_tokens", "token_hash", auth.HashToken(str(b, "refresh_token")))
		if e == nil {
			token["revoked_at"] = now()
			e = t.Put("refresh_tokens", token)
		} else if errors.Is(e, repository.ErrNotFound) {
			e = nil
		}
		return rec{}, e
	})
}
func (h *LiveHandler) handleGetMe(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false, func(t tx) (any, error) {
		u, e := t.Get("users", getUserID(r))
		if e != nil {
			return nil, e
		}
		out := userView(u)
		ms, e := find(t, "memberships", "user_id", getUserID(r))
		if e != nil {
			return nil, e
		}
		items := []rec{}
		for _, m := range ms {
			if !boolean(m, "active") {
				continue
			}
			f, e := t.Get("farms", str(m, "farm_id"))
			if e != nil {
				return nil, e
			}
			if f["archived_at"] == nil {
				items = append(items, rec{"farm_id": f["id"], "farm_name": f["name"], "role": m["role"]})
			}
		}
		out["memberships"] = items
		return out, nil
	})
}
func (h *LiveHandler) handlePatchMe(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		u, e := t.Get("users", getUserID(r))
		if e != nil {
			return nil, e
		}
		mergeAllowed(u, b, "full_name", "phone_wa")
		if e = requireText(u, "full_name"); e != nil {
			return nil, e
		}
		if u["phone_wa"] != nil && !phonePattern.MatchString(str(u, "phone_wa")) {
			return nil, invalid("phone_wa", "Gunakan nomor berawalan 62.")
		}
		e = t.Put("users", u)
		return userView(u), e
	})
}
func (h *LiveHandler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		u, e := t.Get("users", getUserID(r))
		if e != nil {
			return nil, e
		}
		if !auth.CheckPassword(str(u, "password_hash"), str(b, "current_password")) {
			return nil, invalid("current_password", "Sandi lama tidak cocok.")
		}
		if !passwordValid(str(b, "new_password")) {
			return nil, invalid("new_password", "Gunakan 10 sampai 72 karakter.")
		}
		hash, e := auth.HashPassword(str(b, "new_password"))
		if e != nil {
			return nil, e
		}
		u["password_hash"] = hash
		if e = t.Put("users", u); e != nil {
			return nil, e
		}
		return rec{}, h.revokeAll(t, str(u, "id"))
	})
}
func (h *LiveHandler) revokeAll(t tx, user string) error {
	rows, e := find(t, "refresh_tokens", "user_id", user)
	if e != nil {
		return e
	}
	for _, row := range rows {
		row["revoked_at"] = now()
		if e = t.Put("refresh_tokens", row); e != nil {
			return e
		}
	}
	return nil
}
func (h *LiveHandler) sendLink(email, subject, link string) {
	if h.cfg.SMTPHost == "" {
		log.Printf("Tautan %s untuk %s: %s", subject, email, link)
		return
	}
	port := h.cfg.SMTPPort
	if port == "" {
		port = "587"
	}
	var a smtp.Auth
	if h.cfg.SMTPUser != "" {
		a = smtp.PlainAuth("", h.cfg.SMTPUser, h.cfg.SMTPPass, h.cfg.SMTPHost)
	}
	msg := []byte("From: " + h.cfg.SMTPFrom + "\r\nTo: " + email + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + link)
	if e := smtp.SendMail(net.JoinHostPort(h.cfg.SMTPHost, port), a, h.cfg.SMTPFrom, []string{email}, msg); e != nil {
		log.Printf("Pengiriman email AquaSeq gagal: %v", e)
	}
}
func (h *LiveHandler) handlePasswordForgot(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	var link, email string
	err := h.store.Write(r.Context(), func(t tx) error {
		u, e := first(t, "users", "email", strings.ToLower(str(b, "email")))
		if errors.Is(e, repository.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		raw, hash, e := auth.GenerateTokenString()
		if e != nil {
			return e
		}
		email = str(u, "email")
		link = strings.TrimRight(h.cfg.WebURL, "/") + "/atur-sandi?token=" + raw
		return t.Put("password_resets", rec{"id": generateUUID(), "user_id": u["id"], "token_hash": hash, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "used_at": nil})
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	if link != "" {
		h.sendLink(email, "Atur sandi AquaSeq", link)
	}
	writeSuccess(w, 200, rec{})
}
func (h *LiveHandler) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		if !passwordValid(str(b, "password")) {
			return nil, invalid("password", "Gunakan 10 sampai 72 karakter.")
		}
		token, e := first(t, "password_resets", "token_hash", auth.HashToken(str(b, "token")))
		if e != nil || token["used_at"] != nil || !timestamp(token["expires_at"]).After(time.Now()) {
			return nil, &requestError{410, "EXPIRED", "Tautan tidak berlaku. Minta tautan reset baru.", nil}
		}
		u, e := t.Get("users", str(token, "user_id"))
		if e != nil {
			return nil, e
		}
		hash, e := auth.HashPassword(str(b, "password"))
		if e != nil {
			return nil, e
		}
		u["password_hash"] = hash
		token["used_at"] = now()
		if e = t.Put("users", u); e != nil {
			return nil, e
		}
		if e = t.Put("password_resets", token); e != nil {
			return nil, e
		}
		return rec{}, h.revokeAll(t, str(u, "id"))
	})
}
func (h *LiveHandler) handleDemo(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.DemoEnabled {
		writeErrorWithCode(w, 404, "NOT_FOUND", "Akun simulasi tidak tersedia.", nil)
		return
	}
	b, e := decode(w, r)
	if e != nil {
		h.fail(w, e)
		return
	}
	if !has([]string{"pemilik", "teknisi"}, str(b, "role")) {
		h.fail(w, invalid("role", "Pilih pemilik atau teknisi."))
		return
	}
	h.respond(w, r, true, func(t tx) (any, error) {
		if e := h.seed(t); e != nil {
			return nil, e
		}
		id := demoOwner
		if str(b, "role") == "teknisi" {
			id = demoTechnician
		}
		u, e := t.Get("users", id)
		if e != nil {
			return nil, e
		}
		return h.tokens(t, u)
	})
}

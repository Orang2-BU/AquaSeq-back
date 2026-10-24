package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"aquaseq-back/internal/repository"
	"github.com/jackc/pgx/v5/pgconn"
)

type rec = repository.Record
type tx = repository.WebTx
type requestError struct {
	status        int
	code, message string
	fields        map[string]string
}

func (e *requestError) Error() string { return e.message }
func invalid(field, msg string) error {
	return &requestError{400, "VALIDATION_ERROR", "Periksa isian yang ditandai.", map[string]string{field: msg}}
}
func forbidden() error {
	return &requestError{403, "FORBIDDEN", "Akses ditolak. Gunakan akun yang berhak atas lokasi atau kolam ini.", nil}
}
func unauthorized() error {
	return &requestError{401, "UNAUTHORIZED", "Email atau sandi salah. Periksa kembali kredensial Anda.", nil}
}
func str(r rec, k string) string {
	v := r[k]
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func num(r rec, k string) float64 {
	switch n := r[k].(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
func boolean(r rec, k string) bool { v, _ := r[k].(bool); return v }
func now() string                  { return time.Now().UTC().Format(time.RFC3339Nano) }
func timestamp(v any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case string:
		t, _ := time.Parse(time.RFC3339Nano, x)
		return t
	}
	return time.Time{}
}
func clone(r rec) rec {
	out := rec{}
	for k, v := range r {
		out[k] = v
	}
	return out
}
func object(v any) rec { b, _ := json.Marshal(v); r := rec{}; _ = json.Unmarshal(b, &r); return r }
func stringsOf(v any) []string {
	b, _ := json.Marshal(v)
	out := []string{}
	_ = json.Unmarshal(b, &out)
	return out
}
func find(t tx, table, key, value string) ([]rec, error) {
	return t.FindEqual(table, key, value)
}
func first(t tx, table, key, value string) (rec, error) {
	rows, e := find(t, table, key, value)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, repository.ErrNotFound
	}
	return rows[0], nil
}
func decode(w http.ResponseWriter, r *http.Request) (rec, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	var v rec
	if e := d.Decode(&v); e != nil || v == nil {
		return nil, invalid("body", "Kirim objek JSON yang valid.")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return nil, invalid("body", "Kirim satu objek JSON saja.")
	}
	return v, nil
}
func (h *LiveHandler) respond(w http.ResponseWriter, r *http.Request, write bool, fn func(tx) (any, error)) {
	var data any
	cb := func(t tx) error { var e error; data, e = fn(t); return e }
	var e error
	if write {
		e = h.store.Write(r.Context(), cb)
	} else {
		e = h.store.Read(r.Context(), cb)
	}
	if e != nil {
		h.fail(w, e)
		return
	}
	writeSuccess(w, 200, data)
}
func (h *LiveHandler) fail(w http.ResponseWriter, e error) {
	var ae *requestError
	if errors.As(e, &ae) {
		writeErrorWithCode(w, ae.status, ae.code, ae.message, ae.fields)
		return
	}
	if errors.Is(e, repository.ErrNotFound) {
		writeErrorWithCode(w, 404, "NOT_FOUND", "Data tidak ditemukan. Periksa tautan atau muat ulang halaman.", nil)
		return
	}
	var pe *pgconn.PgError
	if errors.As(e, &pe) && pe.Code == "23505" {
		writeErrorWithCode(w, 409, "CONFLICT", "Data sudah dipakai. Periksa kembali isian Anda.", nil)
		return
	}
	log.Printf("Galat penyimpanan AquaSeq: %v", e)
	writeErrorWithCode(w, 500, "INTERNAL", "Data belum dapat diproses. Silakan coba lagi.", nil)
}
func emailValid(s string) bool {
	a, e := mail.ParseAddress(s)
	return e == nil && a.Address == s && len(s) <= 255
}
func passwordValid(s string) bool { return len(s) >= 10 && len(s) <= 72 }

var phonePattern = regexp.MustCompile(`^62[0-9]{7,14}$`)
var pondPattern = regexp.MustCompile(`^[A-Z0-9-]{1,12}$`)

func (h *LiveHandler) farmAccess(t tx, user, farmID string, owner bool) (rec, error) {
	f, e := t.Get("farms", farmID)
	if e != nil {
		return nil, e
	}
	if str(f, "owner_id") == user {
		return f, nil
	}
	if owner {
		return nil, forbidden()
	}
	m, e := find(t, "memberships", "farm_id", farmID)
	if e != nil {
		return nil, e
	}
	for _, x := range m {
		if str(x, "user_id") == user && boolean(x, "active") {
			return f, nil
		}
	}
	return nil, forbidden()
}
func (h *LiveHandler) pondAccess(t tx, user, pondID string, owner bool) (rec, error) {
	p, e := t.Get("ponds", pondID)
	if e != nil {
		return nil, e
	}
	f, e := h.farmAccess(t, user, str(p, "farm_id"), owner)
	if e != nil {
		return nil, e
	}
	if str(f, "owner_id") != user && str(p, "assignee_id") != user {
		return nil, forbidden()
	}
	return p, nil
}
func userView(u rec) rec {
	return rec{"id": u["id"], "full_name": u["full_name"], "email": u["email"], "phone_wa": u["phone_wa"]}
}
func ref(t tx, id string) rec {
	u, e := t.Get("users", id)
	if e != nil {
		return rec{"id": id, "full_name": "Pengguna"}
	}
	return rec{"id": id, "full_name": u["full_name"]}
}
func pondRef(t tx, id string) rec {
	p, e := t.Get("ponds", id)
	if e != nil {
		return rec{"id": id, "pond_code": ""}
	}
	return rec{"id": id, "pond_code": p["pond_code"]}
}
func page(r *http.Request, rows []rec) rec {
	sort.SliceStable(rows, func(i, j int) bool { return str(rows[i], "created_at") > str(rows[j], "created_at") })
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if n < 1 {
		n = 1
	}
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per < 1 {
		per = 20
	}
	if per > 100 {
		per = 100
	}
	total := len(rows)
	start := total
	if n-1 <= total/per {
		start = (n - 1) * per
		if start > total {
			start = total
		}
	}
	end := total
	if per <= total-start {
		end = start + per
	}
	items := rows[start:end]
	if items == nil {
		items = []rec{}
	}
	return rec{"items": items, "total": total}
}
func has(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
func requireText(b rec, keys ...string) error {
	for _, k := range keys {
		if strings.TrimSpace(str(b, k)) == "" {
			return invalid(k, "Wajib diisi.")
		}
		if len(str(b, k)) > 4000 {
			return invalid(k, "Isian terlalu panjang.")
		}
	}
	return nil
}
func mergeAllowed(dst, src rec, keys ...string) {
	for _, k := range keys {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
}
func (h *LiveHandler) notify(t tx, user, kind, title, entity, id string) error {
	return t.Put("notifications", rec{"id": generateUUID(), "user_id": user, "kind": kind, "title": title, "entity": entity, "entity_id": id, "read_at": nil, "created_at": now()})
}
func (h *LiveHandler) activity(t tx, user, farm, action, entity, id, detail string) error {
	return t.Put("activity_log", rec{"id": generateUUID(), "farm_id": farm, "actor_id": user, "action": action, "entity": entity, "entity_id": id, "detail": detail, "created_at": now()})
}

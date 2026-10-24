package auth

import (
	"testing"
	"time"
)

func TestTokens(t *testing.T) {
	if _, e := New(""); e == nil {
		t.Fatal("secret kosong diterima")
	}
	a, e := New("test-secret-012345678901234567890123456789")
	if e != nil {
		t.Fatal(e)
	}
	tok, e := a.Access("user-a")
	if e != nil {
		t.Fatal(e)
	}
	if got, e := a.Verify(tok); e != nil || got != "user-a" {
		t.Fatalf("verify %q %v", got, e)
	}
	if _, e := a.Verify(tok + "x"); e == nil {
		t.Fatal("token rusak diterima")
	}
	hash, e := HashPassword("sandi-panjang-123")
	if e != nil || !CheckPassword(hash, "sandi-panjang-123") || CheckPassword(hash, "salah") {
		t.Fatal("bcrypt salah")
	}
	l := NewLimiter(2, time.Minute)
	if !l.Allow("a") || !l.Allow("a") || l.Allow("a") {
		t.Fatal("rate limit gagal")
	}
}

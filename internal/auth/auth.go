package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Auth struct {
	secret []byte
}

func New(secret string) (*Auth, error) {
	if len(secret) < 16 {
		return nil, errors.New("JWT_SECRET wajib diisi dan minimal 16 karakter")
	}
	return &Auth{secret: []byte(secret)}, nil
}

func (a *Auth) Access(userID string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

func (a *Auth) Verify(tok string) (string, error) {
	token, err := jwt.ParseWithClaims(tok, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("metode penandatangan tidak valid")
		}
		return a.secret, nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return "", errors.New("token tidak valid")
	}
	return claims.Subject, nil
}

func GenerateTokenString() (plain, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(b)
	hash = HashToken(plain)
	return plain, hash, nil
}

func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

type limiterEntry struct {
	count     int
	expiresAt time.Time
}

type Limiter struct {
	mu  sync.Mutex
	max int
	ttl time.Duration
	req map[string]*limiterEntry
}

func NewLimiter(max int, ttl time.Duration) *Limiter {
	return &Limiter{
		max: max,
		ttl: ttl,
		req: make(map[string]*limiterEntry),
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry, ok := l.req[key]
	if !ok || now.After(entry.expiresAt) {
		l.req[key] = &limiterEntry{count: 1, expiresAt: now.Add(l.ttl)}
		return true
	}
	if entry.count >= l.max {
		return false
	}
	entry.count++
	return true
}

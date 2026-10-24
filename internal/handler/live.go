package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/repository"
)

type Config struct {
	Auth          *auth.Auth
	DemoEnabled   bool
	FilesDir      string
	WebURL        string
	SMTPHost      string
	SMTPPort      string
	SMTPUser      string
	SMTPPass      string
	RegisterExtra func(*http.ServeMux, func(http.HandlerFunc) http.HandlerFunc)
	SMTPFrom      string
}

type LiveHandler struct {
	store   repository.WebStore
	cfg     Config
	limiter *auth.Limiter
}

type contextKey string

const (
	ctxKeyUserID contextKey = "user_id"
	ctxKeyEmail  contextKey = "email"
)

func NewLiveHandler(store repository.WebStore, cfg Config) *LiveHandler {
	if cfg.FilesDir == "" {
		cfg.FilesDir = "./uploads"
	}
	_ = os.MkdirAll(cfg.FilesDir, 0755)
	return &LiveHandler{
		store:   store,
		cfg:     cfg,
		limiter: auth.NewLimiter(10, time.Minute),
	}
}

func (h *LiveHandler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("GET /api/v1/health", h.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/register", h.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.handleRefresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.handleLogout)
	mux.HandleFunc("POST /api/v1/auth/password/forgot", h.handlePasswordForgot)
	mux.HandleFunc("POST /api/v1/auth/password/reset", h.handlePasswordReset)
	mux.HandleFunc("POST /api/v1/auth/demo", h.handleDemo)
	mux.HandleFunc("GET /api/v1/invitations/by-token/{token}", h.handleInvitationByToken)
	mux.HandleFunc("POST /api/v1/invitations/by-token/{token}/accept", h.handleInvitationAccept)
	mux.HandleFunc("POST /api/v1/sdri/preview", h.handleSDRIPreview)
	mux.HandleFunc("POST /api/v1/sdri/calculate", h.withAuth(h.handleCalculateSDRI))

	// Protected routes
	mux.HandleFunc("GET /api/v1/auth/me", h.withAuth(h.handleGetMe))
	mux.HandleFunc("PATCH /api/v1/auth/me", h.withAuth(h.handlePatchMe))
	mux.HandleFunc("POST /api/v1/auth/password", h.withAuth(h.handleChangePassword))

	mux.HandleFunc("GET /api/v1/farms", h.withAuth(h.handleGetFarms))
	mux.HandleFunc("POST /api/v1/farms", h.withAuth(h.handleCreateFarm))
	mux.HandleFunc("PATCH /api/v1/farms/{id}", h.withAuth(h.handlePatchFarm))
	mux.HandleFunc("POST /api/v1/farms/{id}/archive", h.withAuth(h.handleArchiveFarm))

	mux.HandleFunc("GET /api/v1/farms/{id}/ponds", h.withAuth(h.handleGetFarmPonds))
	mux.HandleFunc("POST /api/v1/farms/{id}/ponds", h.withAuth(h.handleCreatePond))
	mux.HandleFunc("GET /api/v1/ponds/{id}", h.withAuth(h.handleGetPondDetail))
	mux.HandleFunc("PATCH /api/v1/ponds/{id}", h.withAuth(h.handlePatchPond))
	mux.HandleFunc("POST /api/v1/ponds/{id}/archive", h.withAuth(h.handleArchivePond))

	mux.HandleFunc("GET /api/v1/farms/{id}/members", h.withAuth(h.handleGetMembers))
	mux.HandleFunc("PATCH /api/v1/farms/{id}/members/{user_id}", h.withAuth(h.handlePatchMember))
	mux.HandleFunc("PUT /api/v1/farms/{id}/members/{user_id}/ponds", h.withAuth(h.handlePutMemberPonds))
	mux.HandleFunc("POST /api/v1/farms/{id}/invitations", h.withAuth(h.handleCreateInvitation))
	mux.HandleFunc("DELETE /api/v1/invitations/{id}", h.withAuth(h.handleDeleteInvitation))

	mux.HandleFunc("POST /api/v1/samples", h.withAuth(h.handleCreateSample))
	mux.HandleFunc("POST /api/v1/samples/batch", h.withAuth(h.handleCreateSampleBatch))
	mux.HandleFunc("GET /api/v1/batches", h.withAuth(h.handleGetBatches))
	mux.HandleFunc("GET /api/v1/batches/{id}", h.withAuth(h.handleGetBatchDetail))
	mux.HandleFunc("GET /api/v1/ponds/{id}/samples", h.withAuth(h.handleGetPondSamples))
	mux.HandleFunc("GET /api/v1/samples/{id}", h.withAuth(h.handleGetSampleDetail))
	mux.HandleFunc("GET /api/v1/ponds/{id}/trajectory", h.withAuth(h.handleGetTrajectoryLive))

	mux.HandleFunc("POST /api/v1/ponds/{id}/water-logs", h.withAuth(h.handleCreateWaterLog))
	mux.HandleFunc("GET /api/v1/ponds/{id}/water-logs", h.withAuth(h.handleGetPondWaterLogs))
	mux.HandleFunc("GET /api/v1/farms/{id}/water-logs", h.withAuth(h.handleGetFarmWaterLogs))
	mux.HandleFunc("PATCH /api/v1/water-logs/{id}", h.withAuth(h.handlePatchWaterLog))

	mux.HandleFunc("GET /api/v1/farms/{id}/alerts", h.withAuth(h.handleGetAlerts))
	mux.HandleFunc("POST /api/v1/alerts/{id}/ack", h.withAuth(h.handleAckAlert))

	mux.HandleFunc("GET /api/v1/farms/{id}/tasks", h.withAuth(h.handleGetFarmTasks))
	mux.HandleFunc("GET /api/v1/me/tasks", h.withAuth(h.handleGetMyTasks))
	mux.HandleFunc("POST /api/v1/tasks", h.withAuth(h.handleCreateTask))
	mux.HandleFunc("GET /api/v1/tasks/{id}", h.withAuth(h.handleGetTaskDetail))
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", h.withAuth(h.handlePatchTask))
	mux.HandleFunc("POST /api/v1/tasks/{id}/reports", h.withAuth(h.handleCreateTaskReport))
	mux.HandleFunc("GET /api/v1/files/{id}", h.withAuth(h.handleGetFile))

	mux.HandleFunc("GET /api/v1/me/notifications", h.withAuth(h.handleGetNotifications))
	mux.HandleFunc("POST /api/v1/me/notifications/read", h.withAuth(h.handleReadNotifications))
	mux.HandleFunc("GET /api/v1/farms/{id}/activity", h.withAuth(h.handleGetActivity))

	mux.HandleFunc("POST /api/v1/reports/whatsapp", h.withAuth(h.handleWhatsAppPreview))
	mux.HandleFunc("POST /api/v1/cycles/outcome", h.withAuth(h.handleCycleOutcome))
	mux.HandleFunc("GET /api/v1/ponds/{id}/cycles", h.withAuth(h.handleGetPondCycles))
	mux.HandleFunc("GET /api/v1/models", h.withAuth(h.handleGetModels))
	mux.HandleFunc("POST /api/v1/models/{version}/promote", h.withAuth(h.handlePromoteModel))

	// Mobile routes
	mux.HandleFunc("GET /api/v1/mobile/farm", h.withAuth(h.handleMobileGetFarm))
	mux.HandleFunc("POST /api/v1/mobile/readings", h.withAuth(h.handleMobilePostReadings))
	mux.HandleFunc("PATCH /api/v1/mobile/tasks/{id}", h.withAuth(h.handleMobilePatchTask))
	mux.HandleFunc("POST /api/v1/mobile/recommendations/{id}/schedule", h.withAuth(h.handleMobileScheduleRecommendation))
	mux.HandleFunc("POST /api/v1/mobile/ai/mitigasi", h.withAuth(h.handleMobileAIMitigasi))

	if h.cfg.RegisterExtra != nil {
		h.cfg.RegisterExtra(mux, h.withAuth)
	}
	return mux
}

func (h *LiveHandler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeErrorWithCode(w, http.StatusUnauthorized, "UNAUTHORIZED", "Token autentikasi tidak ditemukan. Silakan masuk kembali.", nil)
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		userID, err := h.cfg.Auth.Verify(token)
		if err != nil {
			writeErrorWithCode(w, http.StatusUnauthorized, "UNAUTHORIZED", "Sesi telah kedaluwarsa atau tidak valid. Silakan masuk kembali.", nil)
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func getUserID(r *http.Request) string {
	val := r.Context().Value(ctxKeyUserID)
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

func writeSuccess(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"data":    data,
		"error":   nil,
	})
}

func writeErrorWithCode(w http.ResponseWriter, status int, code, msg string, fields map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	errObj := map[string]any{
		"code":    code,
		"message": msg,
	}
	if len(fields) > 0 {
		errObj["fields"] = fields
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"data":    nil,
		"error":   errObj,
	})
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

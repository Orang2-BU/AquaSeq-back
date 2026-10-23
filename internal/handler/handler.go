package handler

import (
	"encoding/json"
	"net/http"

	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/report"
	"aquaseq-back/internal/service"
)

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Error   *APIError   `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Handler struct {
	svc *service.AppService
}

func NewHandler(svc *service.AppService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	// Endpoints using Go 1.22+ pattern routing
	mux.HandleFunc("POST /api/v1/sdri/calculate", h.handleCalculateSDRI)
	mux.HandleFunc("POST /api/v1/samples", h.handleIngestSample)
	mux.HandleFunc("GET /api/v1/ponds/{id}/trajectory", h.handleGetTrajectory)
	mux.HandleFunc("POST /api/v1/reports/whatsapp", h.handleWhatsAppPreview)
	mux.HandleFunc("POST /api/v1/cycles/outcome", h.handleCycleOutcome)
	mux.HandleFunc("GET /api/v1/health", h.handleHealth)

	return withCORS(mux)
}

func (h *Handler) handleCalculateSDRI(w http.ResponseWriter, r *http.Request) {
	var input domain.SDRIInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "Invalid JSON format")
		return
	}

	result := h.svc.CalculateSDRI(input)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleIngestSample(w http.ResponseWriter, r *http.Request) {
	var sample domain.Sample
	if err := json.NewDecoder(r.Body).Decode(&sample); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "Invalid JSON format")
		return
	}

	if sample.PondID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_POND_ID", "pond_id is required")
		return
	}

	result, err := h.svc.IngestSample(r.Context(), sample)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INGESTION_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"sample_id":   sample.ID,
		"sdri_result": result,
	})
}

func (h *Handler) handleGetTrajectory(w http.ResponseWriter, r *http.Request) {
	pondID := r.PathValue("id")
	if pondID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "pond id is required in URL path")
		return
	}

	report, err := h.svc.GetPondTrajectory(r.Context(), pondID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TRAJECTORY_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) handleWhatsAppPreview(w http.ResponseWriter, r *http.Request) {
	var payload report.WhatsAppPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "Invalid JSON format")
		return
	}

	msg := h.svc.FormatWhatsAppReport(payload)
	writeJSON(w, http.StatusOK, map[string]string{
		"whatsapp_message": msg,
	})
}

func (h *Handler) handleCycleOutcome(w http.ResponseWriter, r *http.Request) {
	var outcome domain.CycleOutcome
	if err := json.NewDecoder(r.Body).Decode(&outcome); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "Invalid JSON format")
		return
	}

	if outcome.PondID == "" || outcome.CycleNumber <= 0 || outcome.Outcome == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "pond_id, cycle_number, and outcome are required")
		return
	}

	if err := h.svc.RecordCycleOutcome(r.Context(), outcome); err != nil {
		writeError(w, http.StatusInternalServerError, "RECORDING_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "RECORDED",
		"message": "Hasil siklus berhasil dicatat untuk data kalibrasi V2.",
	})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "aquaseq-engine",
		"version": "1.0.0",
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    data,
		Error:   nil,
	})
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Data:    nil,
		Error: &APIError{
			Code:    code,
			Message: msg,
		},
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

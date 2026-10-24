package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"aquaseq-back/internal/ai"
	"aquaseq-back/internal/learning"
)

type AuthFunc func(r *http.Request) (userID string, role string, err error)

type PondDataProvider interface {
	GetTrajectoryPoints(ctx context.Context, pondID string) ([]learning.Sample, error)
	GetLatestMachineFacts(ctx context.Context, pondID string) (ai.MachineFacts, error)
}

type CaseRetriever interface {
	RetrieveSimilarCases(ctx context.Context, pondID string, cb *learning.CaseBase, query map[string]float64, k int) ([]learning.CaseMatch, error)
}

type AIDependencies struct {
	Auth     AuthFunc
 AuthorizePond func(context.Context, string, string, bool) error
	Decider  ai.Decider
	PondData PondDataProvider
	CaseBase *learning.CaseBase
}

type aiHandlers struct {
	deps AIDependencies
}

func RegisterAI(mux *http.ServeMux, deps AIDependencies) {
	h := &aiHandlers{deps: deps}

	mux.HandleFunc("POST /api/v1/documents/ingest", h.handleDocumentIngest)
	mux.HandleFunc("POST /api/v1/ai/explain", h.handleExplain)
	mux.HandleFunc("GET /api/v1/ponds/{id}/insights", h.handlePondInsights)
}

func (h *aiHandlers) authenticate(w http.ResponseWriter, r *http.Request) (string, string, bool) {
 if h.deps.Auth == nil {
  writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Otorisasi wajib")
  return "", "", false
 }
 user, role, err := h.deps.Auth(r)
 if err != nil || strings.TrimSpace(user) == "" {
  writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Otorisasi gagal")
  return "", "", false
 }
 return user, role, true
}

func (h *aiHandlers) handleDocumentIngest(w http.ResponseWriter, r *http.Request) {
	_, role, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	roleLower := strings.ToLower(role)
	if roleLower != "owner" && roleLower != "pemilik" && roleLower != "admin" {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Hanya pemilik tambak atau admin yang berhak mengunggah dokumen lab")
		return
	}

	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	// 10MB limit
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "FILE_TOO_LARGE", "Ukuran file melebihi batas maksimum 10MB atau format multipart tidak valid")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "MISSING_FILE", "Field form 'file' wajib diisi")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "READ_FAILED", "Gagal membaca berkas yang diunggah")
		return
	}

	result, err := ai.Ingest(r.Context(), header.Filename, data, h.deps.Decider)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INGESTION_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *aiHandlers) handleExplain(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	type explainReq struct {
		PondID string `json:"pond_id"`
	}
	var req explainReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "Permintaan hanya menerima pond_id tanpa bidang tambahan")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "Format JSON hanya menerima satu objek tunggal")
		return
	}
	if strings.TrimSpace(req.PondID) == "" {
		writeError(w, http.StatusBadRequest, "MISSING_POND_ID", "Field 'pond_id' wajib diisi")
		return
	}

	if h.deps.AuthorizePond == nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Otorisasi kolam tidak tersedia")
		return
	}
	if err := h.deps.AuthorizePond(r.Context(), userID, req.PondID, false); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Akses kolam ditolak")
		return
	}

	if h.deps.PondData == nil {
		writeError(w, http.StatusInternalServerError, "PROVIDER_UNAVAILABLE", "Sumber data kolam belum terkonfigurasi")
		return
	}

	facts, err := h.deps.PondData.GetLatestMachineFacts(r.Context(), req.PondID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "FACTS_UNAVAILABLE", err.Error())
		return
	}

	report, err := ai.BuildExplanation(facts)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "EXPLANATION_FAILED", err.Error())
		return
	}

	if err := ai.Verify(report.FullNarrative, facts); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VERIFICATION_REJECTED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func (h *aiHandlers) handlePondInsights(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	pondID := r.PathValue("id")
	if strings.TrimSpace(pondID) == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID", "ID kolam wajib disertakan pada path URL")
		return
	}

	if h.deps.AuthorizePond == nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Otorisasi kolam tidak tersedia")
		return
	}
	if err := h.deps.AuthorizePond(r.Context(), userID, pondID, false); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Akses kolam ditolak")
		return
	}

	if h.deps.PondData == nil {
		writeError(w, http.StatusInternalServerError, "PROVIDER_UNAVAILABLE", "Sumber data kolam belum terkonfigurasi")
		return
	}

	points, err := h.deps.PondData.GetTrajectoryPoints(r.Context(), pondID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATA_FETCH_FAILED", err.Error())
		return
	}

	facts, errFacts := h.deps.PondData.GetLatestMachineFacts(r.Context(), pondID)

	var status string
	var explanation *ai.ExplanationReport

	isQCRejected := (errFacts != nil && (strings.Contains(strings.ToLower(errFacts.Error()), "kontrol kualitas") || strings.Contains(strings.ToLower(errFacts.Error()), "qc"))) || (facts.QCStatus != "" && facts.QCStatus != "VALID")

	if isQCRejected {
		status = "QC_REJECTED"
		explanation = nil
	} else if errFacts != nil || len(points) < 4 {
		status = "INSUFFICIENT_DATA"
		explanation = nil
	} else {
		status = "OK"
		rep, err := ai.BuildExplanation(facts)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "EXPLANATION_FAILED", err.Error())
			return
		}
		if err := ai.Verify(rep.FullNarrative, facts); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "VERIFICATION_REJECTED", err.Error())
			return
		}
		explanation = &rep
	}

	// Anomaly analysis
	var anomaly learning.AnomalyReport
	if len(points) >= 2 {
		anom, err := learning.Analyze(pondID, "kolam_"+pondID, points, 4)
		if err == nil {
			anomaly = anom
		} else {
			anomaly = learning.AnomalyReport{
				PondID:       pondID,
				DatasetName:  "kolam_" + pondID,
				Contributors: []learning.Contributor{},
			}
		}
	} else {
		anomaly = learning.AnomalyReport{
			PondID:       pondID,
			DatasetName:  "kolam_" + pondID,
			Contributors: []learning.Contributor{},
		}
	}
	if anomaly.Contributors == nil {
		anomaly.Contributors = []learning.Contributor{}
	}

	// Forecast (7 days & 14 days)
	forecast7, _ := learning.ForecastRisk(points, 1)
	forecast14, _ := learning.ForecastRisk(points, 2)
	if len(points) < 4 {
		forecast7.Horizon = 7
		forecast7.Method = "THEIL_SEN_LOGIT"
		forecast7.Status = "INSUFFICIENT_DATA"
		forecast7.PredictedProbabilityPct = nil
		forecast7.DataPoints = len(points)

		forecast14.Horizon = 14
		forecast14.Method = "THEIL_SEN_LOGIT"
		forecast14.Status = "INSUFFICIENT_DATA"
		forecast14.PredictedProbabilityPct = nil
		forecast14.DataPoints = len(points)
	}

	// Similar cases retrieval
	similarCases := []learning.CaseMatch{}
	if h.deps.CaseBase != nil && len(points) > 0 {
		latest := points[len(points)-1]
		if len(latest.Features) > 0 {
			if retriever, ok := h.deps.PondData.(CaseRetriever); ok {
				matches, err := retriever.RetrieveSimilarCases(r.Context(), pondID, h.deps.CaseBase, latest.Features, 3)
				if err == nil && matches != nil {
					similarCases = matches
				}
			}
		}
	}

	type InsightsResponse struct {
		PondID       string                 `json:"pond_id"`
		Status       string                 `json:"status"`
		Anomaly      learning.AnomalyReport `json:"anomaly"`
		Forecast7    learning.Forecast      `json:"forecast_7d"`
		Forecast14   learning.Forecast      `json:"forecast_14d"`
		SimilarCases []learning.CaseMatch   `json:"similar_cases"`
		Explanation  *ai.ExplanationReport  `json:"explanation"`
	}

	writeJSON(w, http.StatusOK, InsightsResponse{
		PondID:       pondID,
		Status:       status,
		Anomaly:      anomaly,
		Forecast7:    forecast7,
		Forecast14:   forecast14,
		SimilarCases: similarCases,
		Explanation:  explanation,
	})
}

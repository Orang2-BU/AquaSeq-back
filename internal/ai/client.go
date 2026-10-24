package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const maxJevResponseBytes = 1 << 20 // 1 MB limit

// JevClient communicates with the TypeSafe AI System One API.
// Note: Confidence and Noul values represent model certainty regarding document
// formatting, metadata matching, or action text selection. They MUST NEVER be
// interpreted as biological or disease outbreak probabilities.
type JevClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewJevClient(baseURL, apiKey, model string, httpClient *http.Client) *JevClient {
	if baseURL == "" {
		baseURL = "https://api.typesafe.ai"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if httpClient == nil && strings.HasPrefix(baseURL, "http://") {
		baseURL = "https://" + strings.TrimPrefix(baseURL, "http://")
	}
	if model == "" {
		model = "jev-latest"
	}
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Timeout == 0 {
		clientCopy.Timeout = 10 * time.Second
	}
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return errors.New("redirects disabled to prevent credential exposure")
	}
	return &JevClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		httpClient: &clientCopy,
	}
}

func NewClientFromEnv() Decider {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER")))
	if provider != "jev" {
		return nil
	}
	apiKey := strings.TrimSpace(os.Getenv("JEV_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
	}
	if apiKey == "" {
		return nil
	}
	baseURL := os.Getenv("JEV_BASE_URL")
	model := os.Getenv("JEV_MODEL")
	return NewJevClient(baseURL, apiKey, model, nil)
}

type systemOneQuestion struct {
	Type         string      `json:"type"`
	Instructions string      `json:"instructions"`
	Criteria     interface{} `json:"criteria,omitempty"`
}

type systemOneReq struct {
	Model     string                       `json:"model"`
	State     string                       `json:"state"`
	Questions map[string]systemOneQuestion `json:"questions"`
}

type rawAnswer struct {
	Type          string              `json:"type"`
	Choice        string              `json:"choice,omitempty"`
	Confidence    *float64            `json:"confidence,omitempty"`
	Probabilities map[string]*float64 `json:"probabilities,omitempty"`
	Noul          *float64            `json:"noul,omitempty"`
	Score         *float64            `json:"score,omitempty"`
}

type systemOneResp struct {
	Model   string               `json:"model"`
	Answers map[string]rawAnswer `json:"answers"`
}

func (c *JevClient) querySystemOne(ctx context.Context, state string, questions map[string]systemOneQuestion) (*systemOneResp, error) {
	if c.apiKey == "" {
		return nil, errors.New("klien Jev tidak terkonfigurasi (kunci API kosong)")
	}

	endpoint := fmt.Sprintf("%s/v1/systemone", c.baseURL)
	reqBody := systemOneReq{
		Model:     c.model,
		State:     state,
		Questions: questions,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke TypeSafe Jev gagal: %w", err)
	}
	defer resp.Body.Close()

	limitReader := io.LimitReader(resp.Body, int64(maxJevResponseBytes+1))
	respData, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca respons TypeSafe Jev: %w", err)
	}
	if len(respData) > maxJevResponseBytes {
		return nil, errors.New("respons TypeSafe Jev terlalu besar (melebihi batas 1MB)")
	}

	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("autentikasi TypeSafe Jev gagal (401 Unauthorized): periksa API key")
		case http.StatusUnprocessableEntity:
			return nil, fmt.Errorf("permintaan tidak valid menurut TypeSafe Jev (422 Unprocessable Entity)")
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("batas kuota TypeSafe Jev terlampaui (429 Too Many Requests)")
		case 529:
			return nil, fmt.Errorf("layanan TypeSafe Jev sedang kelebihan beban (529 Overloaded)")
		default:
			return nil, fmt.Errorf("layanan TypeSafe Jev mengembalikan status error: %d", resp.StatusCode)
		}
	}

	var parsed systemOneResp
	if err := json.Unmarshal(respData, &parsed); err != nil {
		return nil, fmt.Errorf("gagal mem-parsing format respons JSON TypeSafe Jev: %w", err)
	}

	if parsed.Model == "" || parsed.Answers == nil {
		return nil, errors.New("format respons TypeSafe Jev tidak lengkap: field 'model' atau 'answers' tidak ditemukan")
	}

	return &parsed, nil
}

func (c *JevClient) ClassifyDocument(ctx context.Context, sampleText string) (string, float64, error) {
	if c.apiKey == "" {
		return "", 0, errors.New("klien Jev tidak terkonfigurasi")
	}

	allowedChoices := map[string]string{
		"lab_report": "Laporan pengujian laboratorium eDNA air tambak resmi",
		"demo_file":  "Berkas data contoh, mock, atau data demo buatan",
		"worksheet":  "Lembar kerja catatan lapangan",
		"other":      "Dokumen lainnya",
	}

	questions := map[string]systemOneQuestion{
		"doc_type": {
			Type:         "choice",
			Instructions: "Klasifikasikan tipe dokumen ini berdasarkan struktur teksnya.",
			Criteria:     allowedChoices,
		},
	}

	resp, err := c.querySystemOne(ctx, sampleText, questions)
	if err != nil {
		return "", 0, err
	}

	ans, ok := resp.Answers["doc_type"]
	if !ok {
		return "", 0, errors.New("jawaban doc_type tidak ditemukan")
	}

	return validateChoice(ans, allowedChoices)
}

func validateChoice(ans rawAnswer, allowedChoices map[string]string) (string, float64, error) {
	if ans.Type != "choice" {
		return "", 0, fmt.Errorf("tipe jawaban tidak sesuai (diharapkan 'choice', didapat '%s')", ans.Type)
	}

	if _, allowed := allowedChoices[ans.Choice]; !allowed {
		return "", 0, fmt.Errorf("pilihan tidak valid: '%s' tidak termasuk opsi klasifikasi dokumen", ans.Choice)
	}

	if ans.Confidence == nil {
		return "", 0, errors.New("confidence tidak ditemukan pada jawaban choice")
	}
	conf := *ans.Confidence
	if math.IsNaN(conf) || math.IsInf(conf, 0) || conf < 0.0 || conf > 1.0 {
		return "", 0, fmt.Errorf("nilai confidence di luar rentang valid [0, 1]: %f", conf)
	}

	if ans.Probabilities == nil {
		return "", 0, errors.New("probabilities tidak ditemukan pada jawaban choice")
	}
	sum := 0.0
	maxProb := -1.0
	maxOpt := ""
	for opt := range allowedChoices {
		value, exists := ans.Probabilities[opt]
		if !exists || value == nil {
			return "", 0, fmt.Errorf("distribusi probabilitas tidak lengkap: opsi '%s' tidak ditemukan", opt)
		}
		prob := *value
		if math.IsNaN(prob) || math.IsInf(prob, 0) || prob < 0.0 || prob > 1.0 {
			return "", 0, fmt.Errorf("probabilitas opsi '%s' di luar rentang valid [0, 1]: %f", opt, prob)
		}
		sum += prob
		if prob > maxProb {
			maxProb = prob
			maxOpt = opt
		}
	}
	for opt := range ans.Probabilities {
		if _, allowed := allowedChoices[opt]; !allowed {
			return "", 0, fmt.Errorf("opsi probabilitas tidak dikenal: '%s'", opt)
		}
	}
	if math.Abs(sum-1.0) > 0.02 {
		return "", 0, fmt.Errorf("jumlah probabilitas harus sama dengan 1.0 (didapat %f)", sum)
	}
	if maxOpt != ans.Choice && maxProb > (*ans.Probabilities[ans.Choice]+0.01) {
		return "", 0, fmt.Errorf("pilihan '%s' tidak cocok dengan probabilitas tertinggi ('%s': %f)", ans.Choice, maxOpt, maxProb)
	}

	return ans.Choice, conf, nil
}

func (c *JevClient) ValidateSampleIDMatch(ctx context.Context, sampleCode, docContext string) (bool, float64, error) {
	if c.apiKey == "" {
		return false, 0, errors.New("klien Jev tidak terkonfigurasi")
	}

	questions := map[string]systemOneQuestion{
		"id_match": {
			Type:         "noul",
			Instructions: fmt.Sprintf("Apakah ID sampel '%s' cocok dan valid terdapat di dalam konteks dokumen ini?", sampleCode),
		},
	}

	resp, err := c.querySystemOne(ctx, docContext, questions)
	if err != nil {
		return false, 0, err
	}

	ans, ok := resp.Answers["id_match"]
	if !ok {
		return false, 0, errors.New("jawaban id_match tidak ditemukan")
	}

	if ans.Type != "noul" {
		return false, 0, fmt.Errorf("tipe jawaban tidak sesuai untuk id_match (diharapkan 'noul', didapat '%s')", ans.Type)
	}

	if ans.Noul == nil {
		return false, 0, errors.New("nilai noul tidak ditemukan pada jawaban noul")
	}
	noulVal := *ans.Noul
	if math.IsNaN(noulVal) || math.IsInf(noulVal, 0) || noulVal < 0.0 || noulVal > 1.0 {
		return false, 0, fmt.Errorf("nilai noul di luar rentang valid [0, 1]: %f", noulVal)
	}

	matched := noulVal >= 0.5
	return matched, noulVal, nil
}

func (c *JevClient) SelectActionLabel(ctx context.Context, contextText string, options []string) (string, float64, error) {
	if c.apiKey == "" {
		return "", 0, errors.New("klien Jev tidak terkonfigurasi")
	}

	if len(options) == 0 {
		return "", 0, errors.New("opsi tindakan tidak boleh kosong")
	}
	crit := make(map[string]string)
	for _, opt := range options {
		crit[opt] = fmt.Sprintf("Pilihan tindakan: %s", opt)
	}

	questions := map[string]systemOneQuestion{
		"action_pick": {
			Type:         "choice",
			Instructions: "Pilih opsi tindakan mitigasi terbaik yang sesuai dengan kondisi air tambak ini.",
			Criteria:     crit,
		},
	}

	resp, err := c.querySystemOne(ctx, contextText, questions)
	if err != nil {
		return "", 0, err
	}

	ans, ok := resp.Answers["action_pick"]
	if !ok {
		return "", 0, errors.New("jawaban action_pick tidak ditemukan")
	}

	return validateChoice(ans, crit)
}

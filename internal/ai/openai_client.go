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
	"strings"
	"time"
)

// OpenAIClient implements Decider for any OpenAI-compatible API (e.g. Gemini via OpenAI gateway, LiteLLM, vLLM).
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenAIClient(baseURL, apiKey, model string, httpClient *http.Client) *OpenAIClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if model == "" {
		model = "gpt-4o-mini"
	}
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Timeout == 0 {
		clientCopy.Timeout = 45 * time.Second
	}
	return &OpenAIClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		httpClient: &clientCopy,
	}
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatReq struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	Stream      bool                `json:"stream"`
}

type openAIChatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenAIClient) complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("API key OpenAI/Gemini tidak terkonfigurasi")
	}

	endpoint := c.baseURL + "/chat/completions"
	reqPayload := openAIChatReq{
		Model: c.model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
		Stream:      false,
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AquaSeq-AI/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("koneksi AI gateway gagal: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("gagal membaca respons AI: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI gateway mengembalikan status %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed openAIChatResp
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("gagal parsing respons AI gateway: %w", err)
	}

	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("AI gateway error: %s", parsed.Error.Message)
	}

	if len(parsed.Choices) == 0 {
		return "", errors.New("tidak ada choices dari respons AI gateway")
	}

	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// cleanJSON trims markdown fences like ```json ... ``` from the response.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func (c *OpenAIClient) ClassifyDocument(ctx context.Context, sampleText string) (string, float64, error) {
	systemPrompt := `Anda adalah sistem klasifikasi dokumen laboratorium eDNA akuakultur.
Tugas Anda adalah mengklasifikasikan jenis dokumen ke dalam salah satu dari pilihan berikut:
- "lab_report": Laporan pengujian laboratorium eDNA air tambak resmi
- "demo_file": Berkas data contoh, mock, atau data demo buatan
- "worksheet": Lembar kerja catatan lapangan
- "other": Dokumen lainnya

Format jawaban WAJIB hanya JSON valid tanpa teks lain:
{"choice": "lab_report", "confidence": 0.95}`

	raw, err := c.complete(ctx, systemPrompt, sampleText)
	if err != nil {
		return "", 0, err
	}

	var res struct {
		Choice     string  `json:"choice"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &res); err != nil {
		return "", 0, fmt.Errorf("format JSON klasifikasi tidak valid: %w", err)
	}

	allowed := map[string]bool{"lab_report": true, "demo_file": true, "worksheet": true, "other": true}
	if !allowed[res.Choice] {
		res.Choice = "other"
	}
	if res.Confidence <= 0 || res.Confidence > 1.0 || math.IsNaN(res.Confidence) {
		res.Confidence = 0.8
	}

	return res.Choice, res.Confidence, nil
}

func (c *OpenAIClient) ValidateSampleIDMatch(ctx context.Context, sampleCode, docContext string) (bool, float64, error) {
	systemPrompt := `Anda memvalidasi kecocokan kode sampel dengan konteks dokumen.
Format jawaban WAJIB hanya JSON valid tanpa teks lain:
{"matches": true, "confidence": 0.95}`

	userPrompt := fmt.Sprintf("Kode sampel: %s\nKonteks dokumen:\n%s", sampleCode, docContext)
	raw, err := c.complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return false, 0, err
	}

	var res struct {
		Matches    bool    `json:"matches"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &res); err != nil {
		return false, 0, fmt.Errorf("format JSON validasi sampel tidak valid: %w", err)
	}

	if res.Confidence <= 0 || res.Confidence > 1.0 || math.IsNaN(res.Confidence) {
		res.Confidence = 0.8
	}

	return res.Matches, res.Confidence, nil
}

func (c *OpenAIClient) SelectActionLabel(ctx context.Context, contextText string, options []string) (string, float64, error) {
	optJSON, _ := json.Marshal(options)
	systemPrompt := fmt.Sprintf(`Pilihlah salah satu label tindakan dari opsi yang tersedia: %s.
Format jawaban WAJIB hanya JSON valid tanpa teks lain:
{"choice": "...", "confidence": 0.95}`, string(optJSON))

	raw, err := c.complete(ctx, systemPrompt, contextText)
	if err != nil {
		return "", 0, err
	}

	var res struct {
		Choice     string  `json:"choice"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &res); err != nil {
		return "", 0, fmt.Errorf("format JSON pemilihan label tidak valid: %w", err)
	}

	matched := false
	for _, opt := range options {
		if opt == res.Choice {
			matched = true
			break
		}
	}
	if !matched && len(options) > 0 {
		res.Choice = options[0]
	}

	if res.Confidence <= 0 || res.Confidence > 1.0 || math.IsNaN(res.Confidence) {
		res.Confidence = 0.8
	}

	return res.Choice, res.Confidence, nil
}

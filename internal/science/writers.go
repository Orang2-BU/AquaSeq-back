package science

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"aquaseq-back/internal/ai"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultClaudeModel dipakai bila ANTHROPIC_MODEL kosong.
const DefaultClaudeModel = "claude-opus-5-5"

// ClaudeWriter memanggil Claude lewat SDK resmi Anthropic dengan keluaran JSON terstruktur. System prompt (aturan +
// paket pengetahuan) di-cache karena sama untuk semua sampel; fallback server memakai model lain bila Claude menolak.
type ClaudeWriter struct {
	client anthropic.Client
	model  string
}

func NewClaudeWriter(apiKey, model, baseURL string) *ClaudeWriter {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(1)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if model == "" {
		model = DefaultClaudeModel
	}
	return &ClaudeWriter{client: anthropic.NewClient(opts...), model: model}
}

func (w *ClaudeWriter) Name() string  { return "claude" }
func (w *ClaudeWriter) Model() string { return w.model }

func (w *ClaudeWriter) Write(ctx context.Context, system, user string) (string, error) {
	resp, err := w.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     w.model,
		MaxTokens: 8000,
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		System: []anthropic.BetaTextBlockParam{{
			Text:         system,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		// Penjelasan pendek dari fakta yang sudah dihitung: effort rendah cukup dan menjaga waktu di bawah batas server.
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: OutputSchema()},
		},
		Messages: []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(user))},
	})
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			return "", fmt.Errorf("Claude API status %d", apiErr.StatusCode)
		}
		return "", err
	}
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return "", errors.New("Claude menolak permintaan")
	}
	if resp.StopReason == anthropic.BetaStopReasonMaxTokens {
		return "", errors.New("jawaban Claude terpotong")
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String(), nil
}

// openAIWriter memakai klien kompatibel OpenAI yang sudah ada di backend (LLM_PROVIDER=openai/gemini).
type openAIWriter struct{ client *ai.OpenAIClient }

func (w openAIWriter) Name() string  { return "openai" }
func (w openAIWriter) Model() string { return w.client.Model() }
func (w openAIWriter) Write(ctx context.Context, system, user string) (string, error) {
	return w.client.Complete(ctx, system+"\nJawab hanya dengan satu objek JSON tanpa teks lain.", user)
}

// NewFromEnv membaca konfigurasi AI dari env, sehingga aktif otomatis begitu kunci tersedia di server:
//   - Penulis: ANTHROPIC_API_KEY (+ ANTHROPIC_MODEL, ANTHROPIC_BASE_URL opsional) dipakai lebih dulu; bila kosong,
//     klien kompatibel OpenAI dari LLM_PROVIDER=openai/gemini (OPENAI_*/GEMINI_*/AI_*).
//   - Pemilih: JEV_API_KEY atau TYPESAFE_API_KEY (+ JEV_BASE_URL, JEV_MODEL), tidak bergantung LLM_PROVIDER.
//
// Tanpa kunci apa pun, generator tetap jalan dengan penjelasan template.
func NewFromEnv() *Generator {
	var writer Writer
	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
		writer = NewClaudeWriter(key, strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")), strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")))
	} else if c, ok := ai.NewClientFromEnv().(*ai.OpenAIClient); ok && c != nil {
		writer = openAIWriter{client: c}
	}
	var picker Picker
	key := strings.TrimSpace(os.Getenv("JEV_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
	}
	if key != "" {
		picker = ai.NewJevClient(os.Getenv("JEV_BASE_URL"), key, os.Getenv("JEV_MODEL"), nil)
	}
	return New(writer, picker)
}

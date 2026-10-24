// Package science menulis penjelasan ilmiah berbahasa Indonesia untuk satu sampel dari fakta mesin (SDRI, X1-X12,
// komunitas mikroba) dan paket pengetahuan yang sudah dikurasi (knowledge.json).
//
// Alurnya: penulis LLM (Claude Opus, atau API kompatibel OpenAI) menyusun JSON terstruktur, lalu kode memeriksanya:
// setiap angka harus ada di fakta atau paket pengetahuan, setiap alasan wajib menyebut sumber yang dikenal, dan kata
// mutlak seperti "pasti" ditolak. Bila penulis tidak tersedia atau jawabannya gagal diperiksa, dipakai penjelasan
// template deterministik. Pemilih Jev (TypeSafe) hanya memilih tindakan prioritas dari daftar mitigasi mesin.
// LLM tidak pernah menghitung skor atau membuat angka baru.
package science

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aquaseq-back/internal/microbiome"
	"aquaseq-back/internal/sdri"
)

//go:embed knowledge.json
var knowledgeJSON []byte

// Source adalah satu catatan pengetahuan yang boleh dirujuk penjelasan.
type Source struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Source string `json:"source"`
	DOI    string `json:"doi"`
	Text   string `json:"text"`
	Limits string `json:"limits"`
}

// Knowledge mengembalikan paket pengetahuan yang tertanam di biner.
func Knowledge() []Source {
	var out []Source
	if err := json.Unmarshal(knowledgeJSON, &out); err != nil {
		panic("knowledge.json rusak: " + err.Error())
	}
	return out
}

// FactSource adalah kode sumber untuk angka yang langsung berasal dari mesin AquaSeq.
const FactSource = "FAKTA"

// Facts adalah semua yang boleh dipakai penulis; hanya angka di sini yang sah muncul di penjelasan.
type Facts struct {
	PondCode          string              `json:"pond_code"`
	SampleCode        string              `json:"sample_code"`
	CycleDay          int                 `json:"cycle_day"`
	Status            string              `json:"status"`
	RiskClass         int                 `json:"risk_class"`
	RiskLevel         string              `json:"risk_level"`
	ProbabilityPct    float64             `json:"probability_pct"`
	ModelVersion      string              `json:"model_version"`
	PredictedDisease  string              `json:"predicted_disease"`
	DiseaseName       string              `json:"disease_name"`
	Features          []sdri.Feature      `json:"engineered_features"`
	Microbiome        *microbiome.Metrics `json:"microbiome"`
	Markers           []microbiome.Marker `json:"markers"`
	MitigationActions []string            `json:"mitigation_actions"`
}

// Reason adalah satu alasan beserta kode sumbernya.
type Reason struct {
	Text    string   `json:"text"`
	Sources []string `json:"sources"`
}

// Action adalah satu tindakan; Source berisi FAKTA (daftar mitigasi mesin) atau kode pengetahuan.
type Action struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// Explanation adalah hasil akhir yang disimpan dan dikirim ke web.
type Explanation struct {
	Summary        string    `json:"summary"`
	Reasons        []Reason  `json:"reasons"`
	Actions        []Action  `json:"actions"`
	Limitations    []string  `json:"limitations"`
	Writer         string    `json:"writer"` // "claude", "openai", atau "template"
	WriterModel    string    `json:"writer_model,omitempty"`
	FallbackReason string    `json:"fallback_reason,omitempty"`
	Priority       *Priority `json:"priority_action"`
	Sources        []Source  `json:"sources"` // hanya sumber yang dirujuk, untuk tautan DOI di web
	GeneratedAt    string    `json:"generated_at"`
}

// Priority adalah tindakan yang dipilih Jev dari daftar mitigasi mesin.
type Priority struct {
	Action      string  `json:"action"`
	Confidence  float64 `json:"confidence"`
	NeedsReview bool    `json:"needs_review"` // keyakinan pemilih rendah: perlu dicek manusia
	Picker      string  `json:"picker"`
}

// Writer menghasilkan JSON penjelasan mentah dari system prompt dan pesan pengguna.
type Writer interface {
	Name() string
	Model() string
	Write(ctx context.Context, system, user string) (string, error)
}

// Picker memilih satu opsi dari daftar beserta keyakinannya (antarmuka sama dengan ai.Decider).
type Picker interface {
	SelectActionLabel(ctx context.Context, contextText string, options []string) (string, float64, error)
}

// Generator merangkai penulis, pemeriksa, dan pemilih.
type Generator struct {
	writer    Writer
	picker    Picker
	knowledge []Source
	timeout   time.Duration
	now       func() time.Time
}

// PickerConfidence adalah ambang keyakinan Jev; di bawahnya tindakan ditandai perlu dicek manusia.
const PickerConfidence = 0.6

// New membuat generator; writer dan picker boleh nil (template saja).
func New(writer Writer, picker Picker) *Generator {
	return &Generator{writer: writer, picker: picker, knowledge: Knowledge(), timeout: 22 * time.Second, now: time.Now}
}

// Status melaporkan komponen AI yang aktif.
func (g *Generator) Status() map[string]string {
	out := map[string]string{"writer": "template", "picker": ""}
	if g.writer != nil {
		out["writer"] = g.writer.Name()
		out["writer_model"] = g.writer.Model()
	}
	if g.picker != nil {
		out["picker"] = "jev"
	}
	return out
}

// Explain menulis penjelasan untuk fakta sampel. Tidak pernah gagal: tanpa LLM yang lolos periksa, hasilnya template.
func (g *Generator) Explain(ctx context.Context, f Facts) Explanation {
	var exp Explanation
	var reason string
	if g.writer == nil {
		reason = "penulis AI belum dikonfigurasi"
	} else {
		wctx, cancel := context.WithTimeout(ctx, g.timeout)
		raw, err := g.writer.Write(wctx, g.systemPrompt(), userPrompt(f))
		cancel()
		switch {
		case err != nil:
			reason = "penulis AI gagal: " + shorten(err.Error())
		default:
			if parsed, perr := parse(raw); perr != nil {
				reason = "jawaban AI bukan JSON yang sah"
			} else if verr := g.Check(parsed, f); verr != nil {
				reason = "jawaban AI ditolak pemeriksa: " + verr.Error()
			} else {
				exp = parsed
				exp.Writer = g.writer.Name()
				exp.WriterModel = g.writer.Model()
			}
		}
	}
	if exp.Writer == "" {
		exp = Template(f)
		exp.FallbackReason = reason
	}
	exp.Priority = g.pick(ctx, f)
	exp.Sources = g.cited(exp)
	exp.GeneratedAt = g.now().UTC().Format(time.RFC3339)
	return exp
}

func shorten(s string) string {
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}

func parse(raw string) (Explanation, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "```json"), "```")
	raw = strings.TrimSuffix(raw, "```")
	var e Explanation
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return Explanation{}, err
	}
	return e, nil
}

func (g *Generator) pick(ctx context.Context, f Facts) *Priority {
	if g.picker == nil || len(f.MitigationActions) == 0 {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	choice, conf, err := g.picker.SelectActionLabel(pctx, pickerContext(f), f.MitigationActions)
	if err != nil {
		return nil
	}
	for _, a := range f.MitigationActions {
		if a == choice {
			return &Priority{Action: a, Confidence: math.Round(conf*100) / 100, NeedsReview: conf < PickerConfidence, Picker: "jev"}
		}
	}
	return nil // pilihan di luar daftar diabaikan
}

func pickerContext(f Facts) string {
	parts := []string{fmt.Sprintf("Kelas risiko %d (%s), dugaan: %s.", f.RiskClass, f.RiskLevel, f.DiseaseName)}
	for _, x := range f.Features {
		if x.Status == "kritis" || x.Status == "peringatan" {
			parts = append(parts, fmt.Sprintf("%s %s.", x.Name, x.Status))
		}
	}
	if f.Microbiome != nil {
		for _, s := range f.Microbiome.Signals {
			if s.Status == "kritis" || s.Status == "peringatan" {
				parts = append(parts, fmt.Sprintf("%s %s.", s.Code, s.Status))
			}
		}
	}
	return "Pilih tindakan yang paling mendesak untuk kolam udang ini. " + strings.Join(parts, " ")
}

func (g *Generator) cited(e Explanation) []Source {
	used := map[string]bool{}
	for _, r := range e.Reasons {
		for _, s := range r.Sources {
			used[s] = true
		}
	}
	for _, a := range e.Actions {
		used[a.Source] = true
	}
	out := []Source{}
	for _, s := range g.knowledge {
		if used[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// --- Prompt ---

func (g *Generator) systemPrompt() string {
	var b strings.Builder
	b.WriteString(`Kamu adalah penjelas sains akuakultur untuk petambak udang vaname di Indonesia. Tugasmu menjelaskan hasil mesin AquaSeq untuk satu sampel air kolam, bukan menghitung ulang.

Aturan:
- Tulis dalam Bahasa Indonesia sederhana untuk petambak, kalimat pendek, tanpa jargon yang tidak dijelaskan.
- Pakai hanya angka yang ada di FAKTA atau di PAKET PENGETAHUAN. Jangan membuat, membulatkan ke arah lain, atau menghitung angka baru.
- Setiap alasan wajib punya minimal satu kode sumber: "FAKTA" untuk angka dari mesin, atau kode dari PAKET PENGETAHUAN.
- Tindakan diambil dari mitigation_actions di FAKTA (sumber "FAKTA"). Boleh menambah paling banyak satu tindakan pemantauan bila didukung paket pengetahuan (sebut kodenya).
- Sebut batasan dengan jujur: model belum divalidasi, porsi 16S bersifat relatif, ini alat bantu keputusan dan bukan diagnosis.
- Jangan memakai kata mutlak seperti "pasti", "dijamin", "bebas penyakit", atau "100% aman".
- Bila status sampel bukan VALID, jelaskan bahwa sampel ditolak dan apa yang perlu diulang, tanpa menyebut peluang wabah.
- Jawab hanya dengan JSON sesuai skema: summary (2-3 kalimat), reasons (2-5 butir), actions (1-4 butir), limitations (1-3 butir).

PAKET PENGETAHUAN:
`)
	for _, s := range g.knowledge {
		fmt.Fprintf(&b, "[%s] %s (%s). %s Batasan: %s\n", s.ID, s.Title, s.Source, s.Text, s.Limits)
	}
	return b.String()
}

func userPrompt(f Facts) string {
	j, _ := json.MarshalIndent(f, "", "  ")
	return "FAKTA sampel (JSON dari mesin AquaSeq):\n" + string(j) + "\n\nTulis penjelasan untuk sampel ini."
}

// OutputSchema adalah skema JSON keluaran penulis (structured outputs).
func OutputSchema() map[string]any {
	str := map[string]any{"type": "string"}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"summary", "reasons", "actions", "limitations"},
		"properties": map[string]any{
			"summary": str,
			"reasons": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"text", "sources"},
				"properties": map[string]any{"text": str, "sources": map[string]any{"type": "array", "items": str}},
			}},
			"actions": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"text", "source"},
				"properties": map[string]any{"text": str, "source": str},
			}},
			"limitations": map[string]any{"type": "array", "items": str},
		},
	}
}

// --- Pemeriksa ---

var (
	numberToken = regexp.MustCompile(`^[+-]?\d+(?:[.,]\d+)?%?$`)
	splitter    = regexp.MustCompile(`[\s()\[\]{}:;"'/]+`)
	forbidden   = []string{"pasti ", "dijamin", "bebas penyakit", "100% aman", "diagnosis pasti", "tidak mungkin terjadi"}
)

func parseNumber(tok string) (float64, bool) {
	tok = strings.TrimSuffix(strings.TrimRight(tok, ".,"), "%")
	if !numberToken.MatchString(tok) {
		return 0, false
	}
	// Desimal Indonesia memakai koma; titik di angka >= 1000 adalah pemisah ribuan.
	if strings.Count(tok, ".") == 1 && !strings.Contains(tok, ",") && len(tok)-strings.Index(tok, ".") == 4 {
		tok = strings.ReplaceAll(tok, ".", "")
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(tok, ",", "."), 64)
	return v, err == nil
}

func numbersIn(text string) []float64 {
	out := []float64{}
	for _, tok := range splitter.Split(text, -1) {
		tok = strings.TrimRight(tok, ".,!?")
		if v, ok := parseNumber(tok); ok {
			out = append(out, v)
		}
	}
	return out
}

func collectNumbers(v any, out *[]float64) {
	switch x := v.(type) {
	case float64:
		*out = append(*out, x)
	case string:
		*out = append(*out, numbersIn(x)...)
	case []any:
		for _, y := range x {
			collectNumbers(y, out)
		}
	case map[string]any:
		for _, y := range x {
			collectNumbers(y, out)
		}
	}
}

func (g *Generator) allowedNumbers(f Facts) []float64 {
	var raw any
	b, _ := json.Marshal(f)
	_ = json.Unmarshal(b, &raw)
	out := []float64{}
	collectNumbers(raw, &out)
	for _, s := range g.knowledge {
		out = append(out, numbersIn(s.Text+" "+s.Source+" "+s.Limits)...)
	}
	return out
}

func allowed(v float64, set []float64) bool {
	if v >= 0 && v <= 12 && v == math.Trunc(v) {
		return true // hitungan kecil (jumlah butir, X1-X12, hari) tidak dianggap angka ilmiah baru
	}
	for _, a := range set {
		if math.Abs(v-a) <= math.Max(0.01, 0.006*math.Abs(a)) {
			return true
		}
	}
	return false
}

// Check memeriksa jawaban penulis terhadap fakta dan paket pengetahuan.
func (g *Generator) Check(e Explanation, f Facts) error {
	if strings.TrimSpace(e.Summary) == "" || len(e.Reasons) == 0 || len(e.Actions) == 0 || len(e.Limitations) == 0 {
		return errors.New("bagian wajib kosong")
	}
	if len(e.Reasons) > 6 || len(e.Actions) > 5 || len(e.Limitations) > 4 {
		return errors.New("terlalu panjang")
	}
	known := map[string]bool{FactSource: true}
	for _, s := range g.knowledge {
		known[s.ID] = true
	}
	texts := []string{e.Summary}
	texts = append(texts, e.Limitations...)
	for _, r := range e.Reasons {
		if len(r.Sources) == 0 {
			return fmt.Errorf("alasan tanpa sumber: %q", shorten(r.Text))
		}
		for _, s := range r.Sources {
			if !known[s] {
				return fmt.Errorf("sumber tidak dikenal: %s", s)
			}
		}
		texts = append(texts, r.Text)
	}
	for _, a := range e.Actions {
		if !known[a.Source] {
			return fmt.Errorf("sumber tindakan tidak dikenal: %s", a.Source)
		}
		texts = append(texts, a.Text)
	}
	set := g.allowedNumbers(f)
	for _, t := range texts {
		low := strings.ToLower(t) + " "
		for _, w := range forbidden {
			if strings.Contains(low, w) {
				return fmt.Errorf("kata mutlak: %q", strings.TrimSpace(w))
			}
		}
		for _, v := range numbersIn(t) {
			if !allowed(v, set) {
				return fmt.Errorf("angka tidak ada di fakta: %v", v)
			}
		}
	}
	if f.Status != "VALID" && strings.Contains(strings.Join(texts, " "), "%") {
		return errors.New("sampel ditolak tidak boleh diberi angka peluang")
	}
	return nil
}

// --- Template deterministik ---

var featureSource = map[string]string{
	"jejak_ahpnd":          "WANG2022",
	"fold_regulation_amoa": "XIONG2024",
	"indeks_disbiosis":     "XIONG2024",
	"nh3_mg_l":             "EMERSON1975",
	"porsi_vibrio":         "KOMPOSISI",
	"penurunan_keragaman":  "KERAGAMAN",
	"pergeseran_komunitas": "KERAGAMAN",
}

var featureLabel = map[string]string{
	"kontrol_kualitas": "Kontrol kualitas", "fold_regulation_amoa": "Bakteri nitrifikasi (amoA)",
	"jejak_ahpnd": "Jejak AHPND", "jejak_wssv": "Jejak WSSV", "jejak_ehp": "Jejak EHP",
	"indeks_disbiosis": "Indeks disbiosis", "nh3_mg_l": "Amonia bebas", "no2_mg_l": "Nitrit",
	"h2s_mg_l": "H2S", "do_mg_l": "Oksigen terlarut", "tom_mg_l": "Bahan organik (TOM)", "alkalinity": "Alkalinitas",
	"porsi_vibrio": "Porsi Vibrio", "penurunan_keragaman": "Penurunan keragaman", "pergeseran_komunitas": "Pergeseran komunitas",
}

func fmtNum(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if math.Abs(v) >= 100 {
		s = strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	} else if strings.Contains(s, ".") && len(s)-strings.Index(s, ".") > 4 {
		s = strconv.FormatFloat(v, 'f', 3, 64)
	}
	return strings.ReplaceAll(s, ".", ",")
}

// Template menyusun penjelasan dari fakta tanpa LLM; selalu lolos pemeriksa.
func Template(f Facts) Explanation {
	e := Explanation{Writer: "template", Reasons: []Reason{}, Actions: []Action{}}
	if f.Status != "VALID" {
		e.Summary = fmt.Sprintf("Sampel %s kolam %s ditolak mesin karena kontrol kualitas atau data belum lengkap, jadi peluang wabah tidak dihitung.", f.SampleCode, f.PondCode)
		e.Reasons = append(e.Reasons, Reason{Text: "Kontrol PPC harus berada di rentang sah supaya hasil negatif tidak palsu.", Sources: []string{"AQS-MODEL"}})
		e.Actions = append(e.Actions, Action{Text: "Ulangi pengambilan sampel dan pastikan semua parameter diuji lab.", Source: "AQS-MODEL"})
		e.Limitations = []string{"Sampel ditolak tidak memberi gambaran risiko kolam."}
		return e
	}
	e.Summary = fmt.Sprintf("Kolam %s pada umur kultur %d hari masuk kelas risiko %d (%s) dengan peluang wabah %s%% menurut model %s. %s.",
		f.PondCode, f.CycleDay, f.RiskClass, f.RiskLevel, fmtNum(f.ProbabilityPct), f.ModelVersion, strings.TrimSuffix(f.DiseaseName, "."))
	for _, x := range f.Features {
		if (x.Status == "kritis" || x.Status == "peringatan") && x.Value != nil && len(e.Reasons) < 4 {
			src := featureSource[x.Name]
			if src == "" {
				src = "AQS-AMBANG"
			}
			e.Reasons = append(e.Reasons, Reason{
				Text:    fmt.Sprintf("%s %s, status %s (ambang: %s).", featureLabel[x.Name], fmtNum(*x.Value), x.Status, strings.ReplaceAll(x.Threshold, ";", ",")),
				Sources: []string{FactSource, src},
			})
		}
	}
	if m := f.Microbiome; m != nil && m.Status == "VALID" {
		for _, s := range m.Signals {
			if (s.Status == "kritis" || s.Status == "peringatan") && s.Value != nil && len(e.Reasons) < 5 {
				e.Reasons = append(e.Reasons, Reason{
					Text:    fmt.Sprintf("%s %s, status %s.", featureLabel[s.Code], fmtNum(*s.Value), s.Status),
					Sources: []string{FactSource, featureSource[s.Code], "AQS-MIKROBA"},
				})
			}
		}
	}
	if len(e.Reasons) == 0 {
		e.Reasons = append(e.Reasons, Reason{Text: "Semua parameter yang diuji masih dalam ambang aman mesin.", Sources: []string{FactSource, "AQS-AMBANG"}})
	}
	for _, a := range f.MitigationActions {
		if len(e.Actions) < 4 {
			e.Actions = append(e.Actions, Action{Text: a, Source: FactSource})
		}
	}
	if len(e.Actions) == 0 {
		e.Actions = append(e.Actions, Action{Text: "Lanjutkan sampling eDNA sesuai jadwal dan catat kimia air harian.", Source: "XIONG2024"})
	}
	e.Limitations = []string{"Model peluang wabah memakai koefisien awal dan belum divalidasi pada data hasil siklus; ini alat bantu keputusan, bukan diagnosis."}
	if f.Microbiome != nil {
		e.Limitations = append(e.Limitations, "Porsi bakteri dari sekuensing 16S bersifat relatif, bukan jumlah mutlak.")
	}
	return e
}

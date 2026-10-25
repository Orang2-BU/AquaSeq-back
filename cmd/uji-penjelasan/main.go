// Command uji-penjelasan menjalankan set uji penjelasan ilmiah atas data SINTETIS (testdata/sintetis) memakai
// penulis dan pemilih dari env yang sama dengan server (ANTHROPIC_API_KEY / AI_*, JEV_API_KEY). Tanpa kunci,
// semua kasus memakai template. Setiap panggilan LLM berbiaya; jalankan dengan sengaja.
//
//	go run ./cmd/uji-penjelasan                 # 19 sampel + 6 kasus tepi
//	go run ./cmd/uji-penjelasan -dir testdata/sintetis -out laporan.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"aquaseq-back/internal/science"
	"aquaseq-back/internal/sintetis"
)

type result struct {
	Case        string  `json:"case"`
	Writer      string  `json:"writer"`
	Fallback    string  `json:"fallback_reason,omitempty"`
	Priority    string  `json:"priority_action,omitempty"`
	NeedsReview bool    `json:"needs_review,omitempty"`
	Seconds     float64 `json:"seconds"`
	Summary     string  `json:"summary"`
}

// edgeCases menambah kasus tepi dari sampel nyata set sintetis: tanpa mitigasi, tanpa kelimpahan, dan sampel ditolak.
func edgeCases(cases []sintetis.Case) []sintetis.Case {
	pick := map[string]bool{"B1-D14": true, "B2-D42": true, "B3-D35": true}
	out := []sintetis.Case{}
	for _, c := range cases {
		if !pick[c.Facts.SampleCode] {
			continue
		}
		noAction := c
		noAction.Facts.SampleCode += "/tanpa-mitigasi"
		noAction.Facts.MitigationActions = []string{}
		noMicro := c
		noMicro.Facts.SampleCode += "/tanpa-16S"
		noMicro.Facts.Microbiome = nil
		out = append(out, noAction, noMicro)
	}
	return out
}

func main() {
	dir := flag.String("dir", "testdata/sintetis", "folder data sintetis")
	outPath := flag.String("out", "", "simpan laporan JSON (opsional)")
	flag.Parse()

	samples, err := sintetis.Load(*dir)
	if err != nil {
		log.Fatal(err)
	}
	cases := sintetis.Cases(samples)
	cases = append(cases, edgeCases(cases)...)
	gen := science.NewFromEnv()
	fmt.Printf("Penulis: %v · %d kasus (DATA SINTETIS)\n\n", gen.Status(), len(cases))

	results := []result{}
	byWriter := map[string]int{}
	needsReview := 0
	for _, c := range cases {
		start := time.Now()
		e := gen.Explain(context.Background(), c.Facts)
		r := result{Case: c.Facts.SampleCode, Writer: e.Writer, Fallback: e.FallbackReason, Seconds: time.Since(start).Seconds(), Summary: e.Summary}
		if e.Priority != nil {
			r.Priority, r.NeedsReview = e.Priority.Action, e.Priority.NeedsReview
			if e.Priority.NeedsReview {
				needsReview++
			}
		}
		// Hasil akhir selalu harus lolos pemeriksa, apa pun penulisnya.
		if err := gen.Check(science.Explanation{Summary: e.Summary, Reasons: e.Reasons, Actions: e.Actions, Limitations: e.Limitations}, c.Facts); err != nil {
			log.Fatalf("%s: hasil akhir tidak lolos pemeriksa: %v", r.Case, err)
		}
		byWriter[e.Writer]++
		results = append(results, r)
		fmt.Printf("%-26s %-9s %5.1fs  %s\n", r.Case, r.Writer, r.Seconds, r.Fallback)
	}
	llm := len(cases) - byWriter["template"]
	fmt.Printf("\nDitulis LLM dan lolos pemeriksa: %d/%d (%.0f%%) · template: %d · prioritas perlu dicek: %d\n",
		llm, len(cases), 100*float64(llm)/float64(len(cases)), byWriter["template"], needsReview)
	if *outPath != "" {
		b, _ := json.MarshalIndent(map[string]any{"label": "DATA SINTETIS", "ai": gen.Status(), "results": results}, "", "  ")
		if err := os.WriteFile(*outPath, append(b, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

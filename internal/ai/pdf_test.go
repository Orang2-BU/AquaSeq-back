package ai

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// A complete PDF fixture, not a header followed by pretend CSV.
func textPDF(lines ...string) []byte {
	var stream strings.Builder
	stream.WriteString("BT /F1 10 Tf 40 750 Td 14 TL\n")
	for _, line := range lines {
		escaped := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(line)
		fmt.Fprintf(&stream, "(%s) Tj T*\n", escaped)
	}
	stream.WriteString("ET\n")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 1200 800] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", stream.Len(), stream.String()),
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return pdf.Bytes()
}

func TestPDFCanonicalText(t *testing.T) {
	result, err := Ingest(context.Background(), "text.pdf", textPDF(
		"sample_code    ct_ahpnd    ct_ppc    ph    do_mg_l",
		"POND-1         31.24       19.95     7.8   6.4",
		"POND-2         30.1        24.46     8.0   5.0",
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Samples) != 2 || result.FileType != "PDF" {
		t.Fatalf("unexpected result: %+v", result)
	}
	first, second := result.Samples[0], result.Samples[1]
	if first.Features.CTAhpnd == nil || *first.Features.CTAhpnd != 31.24 || first.Features.PH == nil || *first.Features.PH != 7.8 {
		t.Fatalf("wrong raw features: %+v", first)
	}
	if first.BlockPrediction || !second.BlockPrediction {
		t.Fatalf("PPC QC bypassed: %+v", result)
	}
	if !strings.HasPrefix(first.Provenance["ct_ahpnd"].Cell, "PDF:") {
		t.Fatalf("missing PDF source: %+v", first.Provenance)
	}
	if result.Warnings == nil {
		t.Fatal("warnings must be a JSON array")
	}
}

func TestPDFReferenceGolden(t *testing.T) {
	path := os.Getenv("AQUASEQ_LAB_PDF")
	if path == "" {
		t.Skip("set AQUASEQ_LAB_PDF for private reference PDF")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	grid, err := parsePDFGrid(data)
	if err != nil {
		t.Fatal(err)
	}
	cols, units, err := mapHeaderColumns(grid[0])
	if err != nil {
		t.Fatal(err)
	}
	expected := []struct {
		id                                                     string
		amoa, s16, ahpnd, wssv, ehp, ppc, ph, tan, oxygen, tom float64
	}{
		{"CTRL", 26.2, 18.4, 40, 40, 40, 19.95, 7.8, .25, 6.4, 34.5},
		{"POND-BIO", 19.55, 18.38, 34.25, 40, 40, 19.95, 7.9, .6, 5.8, 78},
		{"POND-TOX", 28.22, 18.44, 18.84, 40, 25.16, 19.95, 8.4, 3.2, 2.65, 128.4},
		{"POND-REM", 22.87, 18.45, 27.89, 40, 28.56, 19.95, 7.85, .45, 5.5, 52},
		{"UNK-HUMIC", 25.037, 18.42, 31.24, 40, 40, 24.46, 7.6, .85, 4.9, 112},
	}
	if len(grid) != 6 {
		t.Fatalf("expected 5 explicit sample rows, got %d", len(grid)-1)
	}
	for i, want := range expected {
		row := grid[i+1]
		if getGridCell(row, cols, "sample_code").Value != want.id {
			t.Fatalf("sample mapping %d", i)
		}
		f, p, _, _, _, err := parseRowFeatures(row, cols, units, i+2)
		if err != nil {
			t.Fatal(err)
		}
		for key, pair := range map[string]struct {
			got  *float64
			want float64
		}{
			"ct_amoa": {f.CTAmoa, want.amoa}, "ct_16s": {f.CT16s, want.s16}, "ct_ahpnd": {f.CTAhpnd, want.ahpnd},
			"ct_wssv": {f.CTWssv, want.wssv}, "ct_ehp": {f.CTEhp, want.ehp}, "ct_ppc": {f.CTPpc, want.ppc},
			"ph": {f.PH, want.ph}, "tan_mg_l": {f.TANMgL, want.tan}, "do_mg_l": {f.DOMgL, want.oxygen}, "tom_mg_l": {f.TOMMgL, want.tom},
		} {
			if pair.got == nil || *pair.got != pair.want || !strings.HasPrefix(p[key].Cell, "PDF:") {
				t.Fatalf("%s %s: got %+v source %+v want %v", want.id, key, pair.got, p[key], pair.want)
			}
		}
	}
	result, err := Ingest(context.Background(), "reference.pdf", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.BlockPrediction || result.QCStatus != "REVIEW_REQUIRED" {
		t.Fatalf("reference not blocked: %+v", result)
	}
	found := false
	ppc := false
	for _, w := range result.Warnings {
		if w.Code == "REFERENCE_SIMULATION" {
			found = true
		}
		if w.Code == "PPC_OUT_OF_BOUNDS" {
			ppc = true
		}
	}
	if !found || !ppc || result.Warnings == nil {
		t.Fatalf("missing reference/PPC warning: %+v", result.Warnings)
	}
	if !result.Samples[4].BlockPrediction || *result.Samples[4].Features.CTAhpnd != 31.24 {
		t.Fatal("humic raw result corrected or accepted")
	}
	if result.Run != (RunMeta{}) {
		t.Fatal("invented lab metadata")
	}
}

func TestPDFOptInPathExists(t *testing.T) {
	path := os.Getenv("AQUASEQ_LAB_PDF")
	if path == "" {
		t.Skip("set AQUASEQ_LAB_PDF for private reference PDF")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

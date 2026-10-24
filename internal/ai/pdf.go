package ai

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxPDFTextBytes = 4 << 20

// Returning an error stops the child and avoids buffering attacker-controlled output.
type pdfTextBuffer struct {
	bytes.Buffer
	limit int
}

func (b *pdfTextBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("PDF text output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func extractPDFText(ctx context.Context, data []byte, mode string) (string, error) {
	if len(data) == 0 || len(data) > maxFileSize || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", errors.New("UNSUPPORTED_DOCUMENT: invalid PDF or input limit exceeded")
	}
	if bytes.Contains(data, []byte("/Encrypt")) {
		return "", errors.New("UNSUPPORTED_DOCUMENT: PDF terenkripsi tidak didukung")
	}
	dir, err := os.MkdirTemp("", "aquaseq-pdf-")
	if err != nil {
		return "", errors.New("REVIEW_REQUIRED: private PDF workspace unavailable")
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "input.pdf")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return "", errors.New("REVIEW_REQUIRED: PDF input unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Fixed executable/arguments; upload names and document text never reach a shell.
	cmd := exec.CommandContext(ctx, "pdftotext", mode, "-enc", "UTF-8", path, "-")
	cmd.WaitDelay = time.Second
	out := &pdfTextBuffer{limit: maxPDFTextBytes}
	stderr := &pdfTextBuffer{limit: 8192}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err = cmd.Run(); err != nil {
		if bytes.Contains(data, []byte("/Subtype /Image")) {
			return "", errors.New("OCR_REQUIRED: PDF gambar perlu OCR; tidak ada data yang diterima")
		}
		return "", errors.New("UNSUPPORTED_DOCUMENT: ekstraksi PDF gagal atau melebihi batas; perlu tinjauan manual")
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("OCR_REQUIRED: PDF tanpa teks perlu OCR")
	}
	return out.String(), nil
}

var pdfColumnGap = regexp.MustCompile(`[ \t]{2,}`)

func canonicalPDFGrid(text string) ([][]CellData, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var grid [][]CellData
	width := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		values := strings.Fields(line)
		if strings.Contains(line, ",") {
			r := csvPDFLine(line)
			if r != nil {
				values = r
			}
		}
		if width == 0 {
			if len(values) < 2 || canonicalFeatureKey(strings.ToLower(values[0])) != "sample_code" {
				continue
			}
			header := make([]CellData, len(values))
			for j, value := range values {
				header[j] = CellData{Value: value, ColIdx: j, RowIdx: i + 1}
			}
			if _, _, err := mapHeaderColumns(header); err != nil {
				return nil, fmt.Errorf("REVIEW_REQUIRED: %w", err)
			}
			grid = append(grid, header)
			width = len(values)
			continue
		}
		if len(values) != width {
			return nil, errors.New("REVIEW_REQUIRED: baris tabel PDF ambigu; gunakan CSV/XLSX asli")
		}
		row := make([]CellData, width)
		for j, value := range values {
			row[j] = CellData{Value: value, ColIdx: j, RowIdx: i + 1, Cell: fmt.Sprintf("PDF:layout:L%d:C%d", i+1, j+1)}
		}
		grid = append(grid, row)
	}
	if len(grid) < 2 {
		return nil, errors.New("REVIEW_REQUIRED: tidak ada tabel PDF canonical dengan sample_code dan nilai yang dapat dipetakan pasti")
	}
	return grid, nil
}

func csvPDFLine(line string) []string {
	reader := csv.NewReader(strings.NewReader(line))
	row, err := reader.Read()
	if err != nil {
		return nil
	}
	if _, err = reader.Read(); err != io.EOF {
		return nil
	}
	return row
}

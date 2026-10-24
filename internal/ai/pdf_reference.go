package ai

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pdfDecimalSpace = regexp.MustCompile(`([0-9][.,])[ \t]+([0-9])`)
var pdfNumber = regexp.MustCompile(`^[+-]?[0-9]+(?:[.,][0-9]+)?$`)
var pdfReportIDs = []string{"CTRL", "POND-BIO", "POND-TOX", "POND-REM", "UNK-HUMIC"}

func ingestPDF(ctx context.Context, filename string, data []byte, decider Decider) (IngestResult, error) {
	grid, err := parsePDFGrid(data)
	if err != nil {
		return IngestResult{}, err
	}
	var text strings.Builder
	writer := csv.NewWriter(&text)
	for _, row := range grid {
		values := make([]string, len(row))
		for _, cell := range row {
			values[cell.ColIdx] = cell.Value
		}
		if err = writer.Write(values); err != nil {
			return IngestResult{}, err
		}
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return IngestResult{}, err
	}
	// The existing numeric parser and QC remain the single source of truth.
	result, err := Ingest(ctx, "extracted.csv", []byte(text.String()), decider)
	if err != nil {
		return IngestResult{}, err
	}
	result.FileName, result.FileType = filename, "PDF"
	if result.Warnings == nil {
		result.Warnings = []Warning{}
	}
	columns, _, err := mapHeaderColumns(grid[0])
	if err != nil {
		return IngestResult{}, err
	}
	reference := strings.Contains(grid[1][0].Cell, "reference")
	for i := range result.Samples {
		sample := &result.Samples[i]
		for key, source := range sample.Provenance {
			cell := getGridCell(grid[i+1], columns, key)
			source.Cell = cell.Cell
			source.Row = cell.RowIdx
			sample.Provenance[key] = source
		}
		if reference {
			sample.BlockPrediction = true
			sample.QCStatus = "REVIEW_REQUIRED"
			sample.ReviewReasons = append(sample.ReviewReasons, "REFERENCE/SIMULATION: arketipe ilmiah, bukan laporan laboratorium operasional terverifikasi")
		}
	}
	if reference {
		result.BlockPrediction = true
		result.QCStatus = "REVIEW_REQUIRED"
		result.Warnings = append(result.Warnings, Warning{Code: "REFERENCE_SIMULATION", Message: "REFERENCE/SIMULATION: lima arketipe ilmiah; bukan laporan laboratorium operasional terverifikasi. Nilai mentah, tanpa koreksi inhibitor; tidak untuk skor operasional.", Location: "PDF: Tabel 1.2, 4.1, 4.3"})
	}
	return result, nil
}

func referencePDFGrid(raw string) ([][]CellData, error) {
	fail := errors.New("REVIEW_REQUIRED: tabel referensi PDF ambigu atau pemetaan sample_code tidak 1:1; tidak ada skor")
	// Only spacing immediately after the decimal separator is normalized. Never strip arbitrary characters.
	text := pdfDecimalSpace.ReplaceAllString(strings.ReplaceAll(raw, "\r\n", "\n"), "$1$2")
	section := func(start, end string) (string, error) {
		a := strings.Index(text, start)
		if a < 0 {
			return "", fail
		}
		b := strings.Index(text[a+len(start):], end)
		if b < 0 {
			return "", fail
		}
		return text[a : a+len(start)+b], nil
	}
	chemistry, err := section("1.2 Tabel Matriks", "BAGIAN 2")
	if err != nil {
		return nil, err
	}
	stats, err := section("4.1 Tabel Statistik", "4.2 Matriks")
	if err != nil {
		return nil, err
	}
	pathogens, err := section("4.3 Tabel Kuantifikasi", "*Catatan Ilmiah")
	if err != nil {
		return nil, err
	}
	// The column order must be stated explicitly, including every code, on each chemistry continuation.
	expected := "CTRL (DOC 15) POND-BIO (DOC 60) POND-TOX (DOC 85) POND-REM (DOC 90) UNK-HUMIC (Gambut)"
	if strings.Count(chemistry, expected) != 3 {
		return nil, fail
	}
	keys := []string{"sample_code", "ct_amoa", "ct_16s", "ct_ahpnd", "ct_wssv", "ct_ehp", "ct_ppc", "temp_c", "ph", "do_mg_l", "salinity_ppt", "tan_mg_l", "no2_mg_l", "h2s_mg_l", "alkalinity", "tom_mg_l"}
	grid := make([][]CellData, 6)
	for i := range grid {
		grid[i] = make([]CellData, len(keys))
		for j, key := range keys {
			grid[i][j] = CellData{ColIdx: j, Value: key}
		}
	}
	put := func(index int, key, value, source string) error {
		col := -1
		for j, k := range keys {
			if k == key {
				col = j
			}
		}
		if col < 0 || index < 0 || index >= 5 || grid[index+1][col].Cell != "" {
			return fail
		}
		if key != "sample_code" && value != "Undetermined" {
			if !pdfNumber.MatchString(value) {
				return fail
			}
			if _, e := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64); e != nil {
				return fail
			}
		}
		grid[index+1][col] = CellData{Value: value, ColIdx: col, RowIdx: 1 + strings.Count(text[:strings.Index(text, source)], "\n"), Cell: fmt.Sprintf("PDF:raw:reference:L%d:%s", 1+strings.Count(text[:strings.Index(text, source)], "\n"), key)}
		return nil
	}
	for i, id := range pdfReportIDs {
		if err = put(i, "sample_code", id, id); err != nil {
			return nil, err
		}
	}
	// Each chemistry segment is bounded by its next explicit parameter, not a guessed number window.
	rows := []struct{ label, next, key, method, standard string }{
		{"Suhu Air", "Derajat Keasaman", "temp_c", "Termometri", "$28.0\\text{--}31.5$"},
		{"Derajat Keasaman", "Oksigen Terlarut", "ph", "Potensiometri", "$7.50\\text{--}8.50$"},
		{"Oksigen Terlarut", "Salinitas", "do_mg_l", "Elektroda Luminesensi", "4.00"},
		{"Salinitas", "Padatan Tersuspensi", "salinity_ppt", "Refraktometri", "$15.0\\text{--}25.0$"},
		{"Total Ammonia Nitrogen", "Fraksi Amonia", "tan_mg_l", "Spektrofotometri Fenat", "1.00"},
		{"Nitrit", "Nitrat", "no2_mg_l", "Spektrofotometri Sulfanilamid", "1.00"},
		{"Hidrogen Sulfida", "C. Mineral", "h2s_mg_l", "Spektrofotometri Metilen Biru", "0.010"},
		{"Alkalinitas Total", "Kalsium", "alkalinity", "Titrasi Asam Sulfat", "$120\\text{--}180$"},
		{"Bahan Organik Total", "Asam Humat", "tom_mg_l", "Titrasi Permanganometri", "90.0"},
	}
	annotations := strings.NewReplacer("(Hipoksia)", "", "(Bahaya)", "", "(Toksik)", "", "(Drop)", "", "(Berlebih)", "", "(Tinggi)", "")
	for _, spec := range rows {
		a := strings.Index(chemistry, spec.label)
		if a < 0 {
			return nil, fail
		}
		b := strings.Index(chemistry[a+len(spec.label):], spec.next)
		if b < 0 {
			return nil, fail
		}
		segment := chemistry[a : a+len(spec.label)+b]
		m := strings.Index(segment, spec.method)
		if m < 0 {
			return nil, fail
		}
		values := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(segment[m+len(spec.method):]), "≤≥"))
		if !strings.HasPrefix(values, spec.standard) {
			return nil, fail
		}
		values = strings.TrimSpace(values[len(spec.standard):])
		tokens := strings.Fields(annotations.Replace(values))
		if len(tokens) != 5 {
			return nil, fail
		}
		for i, value := range tokens {
			if err = put(i, spec.key, value, segment); err != nil {
				return nil, err
			}
		}
	}
	// Join molecular means by the exact leading code and target; exclude post-filter/corrected rows explicitly.
	records := regexp.MustCompile(`(?m)^(CTRL|POND-BIO|POND-TOX|POND-REM|UNK-HUMIC|PPC-Norm|PPC-Humic)(?:\s|$)`).FindAllStringIndex(stats, -1)
	ppc := map[string]string{}
	ppcSource := map[string]string{}
	for n, pos := range records {
		end := len(stats)
		if n+1 < len(records) {
			end = records[n+1][0]
		}
		record := stats[pos[0]:end]
		// Page headers and sterile controls are not part of the preceding sample.
		if cut := strings.Index(record, "STERILE-SW"); cut >= 0 {
			record = record[:cut]
		}
		fields := strings.Fields(record)
		id := fields[0]
		if strings.Contains(record, "Pasca-Filter") {
			continue
		}
		marker := ""
		key := ""
		switch {
		case strings.Contains(record, "amoA_Nit (GOI)"):
			marker = "amoA_Nit (GOI)"
			key = "ct_amoa"
		case strings.Contains(record, "16S_Univ (HKG)"):
			marker = "16S_Univ (HKG)"
			key = "ct_16s"
		case strings.Contains(record, "Kontrol QIAGEN"):
			marker = "Kontrol QIAGEN"
			key = "ct_ppc"
		default:
			return nil, fail
		}
		tokens := strings.Fields(record[strings.Index(record, marker)+len(marker):])
		if len(tokens) < 6 {
			return nil, fail
		}
		// well, replicate 1, replicate 2, replicate 3, mean, SD
		for _, value := range tokens[1:6] {
			if !pdfNumber.MatchString(value) {
				return nil, fail
			}
		}
		mean := tokens[4]
		if key == "ct_ppc" {
			if ppc[id] != "" {
				return nil, fail
			}
			ppc[id] = mean
			ppcSource[id] = record
			continue
		}
		index := -1
		for i, code := range pdfReportIDs {
			if code == id {
				index = i
			}
		}
		if err = put(index, key, mean, record); err != nil {
			return nil, err
		}
	}
	if len(ppc) != 2 || ppc["PPC-Norm"] == "" || ppc["PPC-Humic"] == "" {
		return nil, fail
	}
	for i := range pdfReportIDs {
		control := "PPC-Norm"
		if i == 4 {
			control = "PPC-Humic"
		}
		if err = put(i, "ct_ppc", ppc[control], ppcSource[control]); err != nil {
			return nil, err
		}
	}
	// Parse only the three explicitly labelled Ct columns, consuming copy-count cells separately.
	starts := regexp.MustCompile(`(?m)^(CTRL|POND-BIO|POND-TOX|POND-REM|UNK-HUMIC)\s`).FindAllStringIndex(pathogens, -1)
	ct := regexp.MustCompile(`^(?:\(\$\	ext\{Undet\.\}\$\)\s*>\s*40\.00|[0-9]+\.[0-9]+\s+±\s+[0-9]+\.[0-9]+)\s*`)
	copies := regexp.MustCompile(`^(?:0\s+|\$[0-9]+\.[0-9]+(?:\s*\\times\s*10\^[0-9]+)?\\text\{\s*copies/mL\}\$\s*(?:\(Semu\)\s*)?)`)
	for n, pos := range starts {
		end := len(pathogens)
		if n+1 < len(starts) {
			end = starts[n+1][0]
		}
		record := pathogens[pos[0]:end]
		if strings.HasPrefix(record, "UNK-HUMIC (Koreksi)") {
			continue
		}
		close := strings.Index(record, ")")
		if close < 0 {
			return nil, fail
		}
		remaining := strings.TrimSpace(record[close+1:])
		id := strings.Fields(record)[0]
		index := -1
		for i, code := range pdfReportIDs {
			if id == code {
				index = i
			}
		}
		for _, key := range []string{"ct_ahpnd", "ct_wssv", "ct_ehp"} {
			match := ct.FindString(remaining)
			if match == "" {
				return nil, fmt.Errorf("%w: %s %s", fail, id, key)
			}
			value := strings.Fields(match)[0]
			if strings.Contains(match, "Undet.") {
				value = "Undetermined"
			}
			if err = put(index, key, value, record); err != nil {
				return nil, err
			}
			remaining = strings.TrimSpace(remaining[len(match):])
			copy := copies.FindString(remaining)
			if copy == "" {
				return nil, fail
			}
			remaining = strings.TrimSpace(remaining[len(copy):])
		}
	}
	for _, row := range grid[1:] {
		for _, cell := range row {
			if cell.Cell == "" {
				return nil, fail
			}
		}
	}
	return grid, nil
}

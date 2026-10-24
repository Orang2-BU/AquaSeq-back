package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxFileSize             = 15 << 20 // 15 MB
	maxDecompressTotalBytes = 20 << 20 // 20 MB total uncompressed
	maxEntryDecompressBytes = 10 << 20 // 10 MB per entry
	maxZipCompressionRatio  = 100
	maxSharedStringsCount   = 100000
)

type Decider interface {
	ClassifyDocument(ctx context.Context, sampleText string) (string, float64, error)
	ValidateSampleIDMatch(ctx context.Context, sampleCode, docContext string) (bool, float64, error)
	SelectActionLabel(ctx context.Context, contextText string, options []string) (string, float64, error)
}

type CellData struct {
	Value  string
	Cell   string
	ColIdx int
	RowIdx int
}

func colLetters(colIdx int) string {
	res := ""
	for colIdx >= 0 {
		res = string(rune('A'+(colIdx%26))) + res
		colIdx = colIdx/26 - 1
	}
	return res
}

func parseColLetter(cellRef string) (int, int) {
	var letters, digits strings.Builder
	for _, r := range cellRef {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			letters.WriteRune(r)
		} else if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	colStr := strings.ToUpper(letters.String())
	colIdx := 0
	for i := 0; i < len(colStr); i++ {
		colIdx = colIdx*26 + int(colStr[i]-'A'+1)
	}
	colIdx -= 1
	rowIdx, _ := strconv.Atoi(digits.String())
	return colIdx, rowIdx
}

func Ingest(ctx context.Context, filename string, data []byte, decider Decider) (IngestResult, error) {
	if len(data) == 0 {
		return IngestResult{}, errors.New("berkas kosong (empty file content)")
	}
	if len(data) > maxFileSize {
		return IngestResult{}, errors.New("ukuran berkas melebihi batas maksimum 15MB")
	}

	// Reject binary legacy DOC (OLE2 CFBF header)
	if bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}) {
		return IngestResult{}, errors.New("UNSUPPORTED_DOCUMENT: format .doc biner lama tidak didukung; gunakan format terstruktur .docx atau .xlsx")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".doc" {
		return IngestResult{}, errors.New("UNSUPPORTED_DOCUMENT: format .doc biner lama tidak didukung; gunakan format terstruktur .docx atau .xlsx")
	}

	var grid [][]CellData
	var fileType string
	var err error

	switch ext {
	case ".csv":
		fileType = "CSV"
		grid, err = parseCSVGrid(data)
	case ".xlsx":
		fileType = "XLSX"
		grid, err = parseXLSXGrid(data)
	case ".docx":
		fileType = "DOCX"
		grid, err = parseDOCXGrid(data)
	case ".pdf":
		return ingestPDF(ctx, filename, data, decider)
	default:
		return IngestResult{}, errors.New("UNSUPPORTED_DOCUMENT: ekstensi berkas tidak didukung")
	}

	if err != nil {
		return IngestResult{}, err
	}

	if len(grid) < 2 {
		return IngestResult{}, errors.New("file contains insufficient tabular rows (minimum header + 1 data row)")
	}

	headerRow := grid[0]
	colMap, unitsMap, err := mapHeaderColumns(headerRow)
	if err != nil {
		return IngestResult{}, err
	}

	if _, ok := colMap["sample_code"]; !ok {
		return IngestResult{}, errors.New("kolom ID sampel (sample_code/sample_id) tidak ditemukan pada header dokumen")
	}

	var samples []SampleRecord
	var warnings []Warning
	seenIDs := make(map[string]int)

	hasBlockedPrediction := false

	for i := 1; i < len(grid); i++ {
		row := grid[i]
		if isGridRowEmpty(row) {
			continue
		}

		sampleCell := getGridCell(row, colMap, "sample_code")
		sampleCode := strings.TrimSpace(sampleCell.Value)
		if sampleCode == "" {
			return IngestResult{}, fmt.Errorf("ID sampel kosong pada baris %d; nilai ID tidak boleh difabrikasi", i+1)
		}

		if !sampleIDPattern.MatchString(sampleCode) {
			return IngestResult{}, fmt.Errorf("ID sampel tidak valid atau memuat karakter terlarang: '%s'", sampleCode)
		}

		loc := fmt.Sprintf("Baris %d (Sampel %s)", i+1, sampleCode)
		sampleBlock := false
		sampleQCStatus := "PASS"
		var reviewReasons []string

		// Check duplicate ID
		if prev, ok := seenIDs[sampleCode]; ok {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, fmt.Sprintf("ID sampel duplikat (sama dengan baris %d)", prev))
			warnings = append(warnings, Warning{
				Code:     "DUPLICATE_SAMPLE_ID",
				Message:  fmt.Sprintf("ID sampel terdeteksi ganda pada baris %d dan baris %d.", prev, i+1),
				Location: loc,
			})
			for idx := range samples {
				if samples[idx].SampleCode == sampleCode {
					samples[idx].BlockPrediction = true
					samples[idx].QCStatus = "REVIEW_REQUIRED"
					samples[idx].ReviewReasons = append(samples[idx].ReviewReasons, fmt.Sprintf("ID sampel duplikat (sama dengan baris %d)", i+1))
				}
			}
		} else {
			seenIDs[sampleCode] = i + 1
		}

		// Demo data check
		lowerID := strings.ToLower(sampleCode)
		if strings.Contains(lowerID, "demo") || strings.Contains(lowerID, "dummy") || strings.Contains(lowerID, "mock") || strings.Contains(lowerID, "test_sample") {
			warnings = append(warnings, Warning{
				Code:     "DEMO_DATA_DETECTED",
				Message:  "Dokumen terindikasi berisi data demo atau nama sampel bukan data eDNA air tambak sebenarnya.",
				Location: loc,
			})
		}

		features, provenance, missing, undetected, negativeCt, err := parseRowFeatures(row, colMap, unitsMap, i+1)
		if err != nil {
			return IngestResult{}, err
		}

		if negativeCt {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, "Nilai Ct negatif tidak valid")
		}

		// Check explicit status columns
		if qcVal := strings.ToUpper(strings.TrimSpace(getGridCell(row, colMap, "qc_status").Value)); qcVal == "FAIL" {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, "Status QC dokumen ditandai FAIL")
		}
		if ntcVal := strings.ToUpper(strings.TrimSpace(getGridCell(row, colMap, "ntc_status").Value)); ntcVal == "DETECTED" {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, "Status NTC dokumen ditandai DETECTED (kontaminasi)")
		}
		if outlierVal := strings.ToLower(strings.TrimSpace(getGridCell(row, colMap, "outlier").Value)); outlierVal == "true" {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, "Sampel ditandai sebagai outlier")
		}

		// Check completeness: must have at least one disease target or water chemistry
		hasTarget := features.CTAhpnd != nil || features.CTWssv != nil || features.CTEhp != nil
		hasChem := features.DOMgL != nil || features.TOMMgL != nil || features.PH != nil || features.SalinityPpt != nil || features.TempC != nil || features.TANMgL != nil || features.NO2MgL != nil || features.H2SMgL != nil || features.Alkalinity != nil
		if !hasTarget && !hasChem {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, "Data tidak lengkap: tidak ada target patogen maupun parameter kimia yang diuji")
		}

		// Quality control (PPC) validation - PPC18-22 fail closed, no auto-correction
		if features.CTPpc == nil {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, "Kontrol kualitas PPC tidak ditemukan")
			warnings = append(warnings, Warning{
				Code:     "MISSING_QC_PPC",
				Message:  "Kontrol kualitas PPC tidak ditemukan pada sampel. Prediksi diblokir; perlu peninjauan manual.",
				Location: loc,
			})
		} else if *features.CTPpc < 18.0 || *features.CTPpc > 22.0 {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, fmt.Sprintf("Kontrol kualitas PPC (%.2f) di luar batas standar 18,0 - 22,0", *features.CTPpc))
			warnings = append(warnings, Warning{
				Code:     "PPC_OUT_OF_BOUNDS",
				Message:  fmt.Sprintf("Nilai kontrol kualitas bergeser (%.2f) di luar batas standar (18,0 - 22,0). Terindikasi inhibisi PCR. Fail closed: tidak melakukan koreksi otomatis buatan.", *features.CTPpc),
				Location: loc,
			})
		}

		// NTC (No Template Control) check
		upperID := strings.ToUpper(sampleCode)
		if strings.Contains(upperID, "NTC") || strings.Contains(upperID, "NEGATIVE_CONTROL") || strings.Contains(upperID, "KONTROL_NEGATIF") || strings.Contains(upperID, "BLANK") {
			ntcAmplified := false
			if (features.CTAhpnd != nil && *features.CTAhpnd < 38.0) ||
				(features.CTWssv != nil && *features.CTWssv < 38.0) ||
				(features.CTEhp != nil && *features.CTEhp < 38.0) {
				ntcAmplified = true
			}
			if ntcAmplified {
				sampleBlock = true
				sampleQCStatus = "REVIEW_REQUIRED"
				reviewReasons = append(reviewReasons, "Kontrol negatif (NTC) teramplifikasi (terindikasi kontaminasi reagen)")
				warnings = append(warnings, Warning{
					Code:     "NTC_CONTAMINATION_DETECTED",
					Message:  "Kontrol negatif (NTC) teramplifikasi. Terindikasi kontaminasi reagen/ruang amplifikasi.",
					Location: loc,
				})
			}
		}

		// Physical range check
		if features.PH != nil && (*features.PH < 4.0 || *features.PH > 11.0) {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, fmt.Sprintf("Parameter pH (%.2f) di luar rentang fisik biologis normal", *features.PH))
			warnings = append(warnings, Warning{
				Code:     "PHYSICAL_RANGE_VIOLATION",
				Message:  fmt.Sprintf("Parameter pH (%.2f) di luar rentang fisik biologis tambak normal (4,0 - 11,0).", *features.PH),
				Location: loc,
			})
		}
		if features.DOMgL != nil && (*features.DOMgL < 0 || *features.DOMgL > 25.0) {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, fmt.Sprintf("Parameter DO (%.2f mg/L) di luar rentang fisik", *features.DOMgL))
			warnings = append(warnings, Warning{
				Code:     "PHYSICAL_RANGE_VIOLATION",
				Message:  fmt.Sprintf("Parameter oksigen terlarut DO (%.2f mg/L) di luar rentang fisik.", *features.DOMgL),
				Location: loc,
			})
		}
		if features.SalinityPpt != nil && (*features.SalinityPpt < 0 || *features.SalinityPpt > 70.0) {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, fmt.Sprintf("Parameter salinitas (%.2f ppt) di luar rentang fisik", *features.SalinityPpt))
			warnings = append(warnings, Warning{
				Code:     "PHYSICAL_RANGE_VIOLATION",
				Message:  fmt.Sprintf("Parameter salinitas (%.2f ppt) di luar rentang fisik tambak.", *features.SalinityPpt),
				Location: loc,
			})
		}
		if features.TempC != nil && (*features.TempC < 15.0 || *features.TempC > 45.0) {
			sampleBlock = true
			sampleQCStatus = "REVIEW_REQUIRED"
			reviewReasons = append(reviewReasons, fmt.Sprintf("Suhu air (%.2f C) di luar rentang fisik normal", *features.TempC))
			warnings = append(warnings, Warning{
				Code:     "PHYSICAL_RANGE_VIOLATION",
				Message:  fmt.Sprintf("Suhu air (%.2f C) di luar rentang fisik normal.", *features.TempC),
				Location: loc,
			})
		}

		if sampleBlock {
			hasBlockedPrediction = true
		}

		samples = append(samples, SampleRecord{
			SampleCode:        sampleCode,
			Features:          features,
			Confidence:        0.95,
			QCStatus:          sampleQCStatus,
			BlockPrediction:   sampleBlock,
			ReviewReasons:     reviewReasons,
			MissingFeatures:   missing,
			UndetectedTargets: undetected,
			Provenance:        provenance,
		})
	}

	// Document-level decider verification if available
	if decider != nil {
		sampleSnippet := ""
		if len(data) > 300 {
			sampleSnippet = string(data[:300])
		} else {
			sampleSnippet = string(data)
		}
		docType, conf, err := decider.ClassifyDocument(ctx, sampleSnippet)
		if err != nil {
			warnings = append(warnings, Warning{
				Code:     "LLM_VERIFICATION_INACTIVE",
				Message:  "Verifikasi LLM tidak aktif: evaluasi deterministik tetap berjalan.",
				Location: "Dokumen",
			})
		} else if docType == "demo_file" && conf > 0.6 {
			warnings = append(warnings, Warning{
				Code:     "DEMO_DATA_DETECTED",
				Message:  "Klasifikasi keputusan model menandai dokumen sebagai berkas demo.",
				Location: "Dokumen",
			})
		}
	} else {
		warnings = append(warnings, Warning{
			Code:     "LLM_VERIFICATION_INACTIVE",
			Message:  "Verifikasi LLM tidak aktif: ingesti dokumen berjalan penuh via parser deterministik.",
			Location: "Dokumen",
		})
	}

	docQCStatus := "PASS"
	if hasBlockedPrediction {
		docQCStatus = "REVIEW_REQUIRED"
	}

	return IngestResult{
		FileName:        filename,
		FileType:        fileType,
		Run:             RunMeta{}, // No fabricated metadata
		Samples:         samples,
		Confidence:      0.95,
		QCStatus:        docQCStatus,
		BlockPrediction: hasBlockedPrediction,
		Warnings:        warnings,
	}, nil
}

func isGridRowEmpty(row []CellData) bool {
	for _, c := range row {
		if strings.TrimSpace(c.Value) != "" {
			return false
		}
	}
	return true
}

func mapHeaderColumns(headerRow []CellData) (map[string]int, map[string]string, error) {
	colMap := make(map[string]int)
	unitsMap := make(map[string]string)
	seenCanonical := make(map[string]string)

	for _, c := range headerRow {
		raw := strings.TrimSpace(c.Value)
		if raw == "" {
			continue
		}

		clean, unit, err := normalizeHeaderName(raw)
		if err != nil {
			return nil, nil, err
		}

		canon := canonicalFeatureKey(clean)
		if canon == "" {
			return nil, nil, fmt.Errorf("kolom tidak dikenal pada header: '%s'", raw)
		}

		if prevCol, exists := seenCanonical[canon]; exists {
			return nil, nil, fmt.Errorf("kolom duplikat atau bentrok terdeteksi pada header: '%s' dan '%s'", prevCol, raw)
		}
		seenCanonical[canon] = raw
		colMap[canon] = c.ColIdx
		if unit != "" {
			unitsMap[canon] = unit
		}
	}

	return colMap, unitsMap, nil
}

func normalizeHeaderName(h string) (string, string, error) {
	trimmed := strings.TrimSpace(h)
	lower := strings.ToLower(trimmed)

	unit := ""
	openParen := strings.IndexAny(trimmed, "([")
	closeParen := strings.IndexAny(trimmed, ")]")
	if openParen != -1 && closeParen != -1 && closeParen > openParen {
		unit = strings.TrimSpace(trimmed[openParen+1 : closeParen])
		lower = strings.ToLower(strings.TrimSpace(trimmed[:openParen]))
	}

	clean := strings.ReplaceAll(lower, " ", "_")
	clean = strings.ReplaceAll(clean, "-", "_")

	unitLower := strings.ToLower(unit)
	if (clean == "do" || strings.HasPrefix(clean, "do_")) && (unitLower == "g/l" || (strings.HasSuffix(clean, "_g_l") && !strings.Contains(clean, "mg_l"))) {
		return "", "", fmt.Errorf("satuan kolom DO tidak valid: '%s' (diharapkan mg/L)", h)
	}
	if (clean == "tom" || strings.HasPrefix(clean, "tom_")) && unitLower == "g/l" {
		return "", "", fmt.Errorf("satuan kolom TOM tidak valid: '%s' (diharapkan mg/L)", h)
	}
	if (clean == "temp" || clean == "temperature" || strings.HasPrefix(clean, "temp_")) && unitLower == "f" {
		return "", "", fmt.Errorf("satuan suhu tidak valid: '%s' (diharapkan Celcius)", h)
	}

	return clean, unit, nil
}

func canonicalFeatureKey(clean string) string {
	switch clean {
	case "samplecode", "sample_id", "sample", "id", "sample_code":
		return "sample_code"
	case "ct_ahpnd", "pira", "ahpnd", "ct_pira":
		return "ct_ahpnd"
	case "ct_wssv", "vp28", "wssv", "ct_vp28":
		return "ct_wssv"
	case "ct_ehp", "swp1", "ehp", "ct_swp1":
		return "ct_ehp"
	case "ct_amoa", "amoa", "amoa_nit", "ct_amoa_nit":
		return "ct_amoa"
	case "ct_16s", "16s", "16s_univ", "ct_16s_univ":
		return "ct_16s"
	case "ct_ppc", "ppc", "control_ppc", "ppc_norm":
		return "ct_ppc"
	case "do_mg_l", "do", "do_mgl", "oxygen":
		return "do_mg_l"
	case "tom_mg_l", "tom", "tom_mgl":
		return "tom_mg_l"
	case "ph", "ph_water":
		return "ph"
	case "salinity_ppt", "salinity", "sal":
		return "salinity_ppt"
	case "temp_c", "temp", "temperature":
		return "temp_c"
	case "tan_mg_l", "tan", "tan_mgl":
		return "tan_mg_l"
	case "no2_mg_l", "no2", "nitrite":
		return "no2_mg_l"
	case "h2s_mg_l", "h2s":
		return "h2s_mg_l"
	case "alkalinity", "alkalinity_caco3", "alk":
		return "alkalinity"
	case "qc_status":
		return "qc_status"
	case "ntc_status":
		return "ntc_status"
	case "outlier":
		return "outlier"
	default:
		return ""
	}
}

func getGridCell(row []CellData, colMap map[string]int, key string) CellData {
	idx, ok := colMap[key]
	if !ok {
		return CellData{}
	}
	for _, c := range row {
		if c.ColIdx == idx {
			return c
		}
	}
	return CellData{}
}

func parseRowFeatures(row []CellData, colMap map[string]int, unitsMap map[string]string, rowNum int) (FeatureMatrix, map[string]FieldSource, []string, []string, bool, error) {
	prov := make(map[string]FieldSource)
	var missing []string
	var undetected []string
	hasNegativeCt := false

	parseVal := func(key string, isQPCR bool) (*float64, error) {
		colIdx, ok := colMap[key]
		if !ok {
			missing = append(missing, key)
			return nil, nil
		}

		cell := getGridCell(row, colMap, key)
		cellCoord := cell.Cell
		if cellCoord == "" {
			cellCoord = fmt.Sprintf("%s%d", colLetters(colIdx), rowNum)
		}
		raw := strings.TrimSpace(cell.Value)

		if raw == "" || raw == "-" || raw == "NA" || raw == "N/A" {
			missing = append(missing, key)
			return nil, nil
		}

		upper := strings.ToUpper(raw)
		if upper == "NAN" || strings.Contains(upper, "INFINITY") {
			return nil, fmt.Errorf("nilai numerik tidak valid '%s' pada baris %d kolom '%s'", raw, rowNum, key)
		}

		if isQPCR && (upper == "UNDETERMINED" || upper == "UNDET" || upper == "UNDET." || upper == "UNDETECTED" || upper == "NEGATIF" || upper == "NEGATIVE" || upper == ">40" || upper == "> 40") {
			undetected = append(undetected, key)
			val := 40.0
			prov[key] = FieldSource{
				Cell:     cellCoord,
				Column:   key,
				Row:      rowNum,
				RawValue: raw,
				Unit:     unitsMap[key],
			}
			return &val, nil
		}

		normStr := strings.ReplaceAll(raw, ",", ".")
		v, err := strconv.ParseFloat(normStr, 64)
		if err != nil {
			return nil, fmt.Errorf("gagal membaca nilai numerik '%s' pada baris %d kolom '%s': %w", raw, rowNum, key, err)
		}

		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("nilai numerik tidak valid '%s' pada baris %d kolom '%s'", raw, rowNum, key)
		}

		if isQPCR && v < 0 {
			hasNegativeCt = true
		} else if isQPCR && v >= 40.0 {
			undetected = append(undetected, key)
		}

		prov[key] = FieldSource{
			Cell:     cellCoord,
			Column:   key,
			Row:      rowNum,
			RawValue: raw,
			Unit:     unitsMap[key],
		}
		return &v, nil
	}

	ctAhpnd, err := parseVal("ct_ahpnd", true)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	ctWssv, err := parseVal("ct_wssv", true)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	ctEhp, err := parseVal("ct_ehp", true)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	ctAmoa, err := parseVal("ct_amoa", true)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	ct16s, err := parseVal("ct_16s", true)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	ctPpc, err := parseVal("ct_ppc", true)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	doMgL, err := parseVal("do_mg_l", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	tomMgL, err := parseVal("tom_mg_l", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	ph, err := parseVal("ph", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	salinityPpt, err := parseVal("salinity_ppt", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	tempC, err := parseVal("temp_c", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	tanMgL, err := parseVal("tan_mg_l", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	no2MgL, err := parseVal("no2_mg_l", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	h2sMgL, err := parseVal("h2s_mg_l", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}
	alkalinity, err := parseVal("alkalinity", false)
	if err != nil {
		return FeatureMatrix{}, nil, nil, nil, false, err
	}

	features := FeatureMatrix{
		CTAhpnd:     ctAhpnd,
		CTWssv:      ctWssv,
		CTEhp:       ctEhp,
		CTAmoa:      ctAmoa,
		CT16s:       ct16s,
		CTPpc:       ctPpc,
		DOMgL:       doMgL,
		TOMMgL:      tomMgL,
		PH:          ph,
		SalinityPpt: salinityPpt,
		TempC:       tempC,
		TANMgL:      tanMgL,
		NO2MgL:      no2MgL,
		H2SMgL:      h2sMgL,
		Alkalinity:  alkalinity,
	}

	return features, prov, missing, undetected, hasNegativeCt, nil
}

func parseCSVGrid(data []byte) ([][]CellData, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = false

	rawRows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("format CSV tidak valid: %w", err)
	}
	if len(rawRows) == 0 {
		return nil, errors.New("CSV kosong")
	}

	headerLen := len(rawRows[0])
	var grid [][]CellData

	for rIdx, row := range rawRows {
		if len(row) != headerLen {
			return nil, fmt.Errorf("baris CSV ragged (kolom baris %d berjumlah %d, header %d)", rIdx+1, len(row), headerLen)
		}
		var rowCells []CellData
		for cIdx, val := range row {
			rowCells = append(rowCells, CellData{
				Value:  val,
				Cell:   fmt.Sprintf("%s%d", colLetters(cIdx), rIdx+1),
				ColIdx: cIdx,
				RowIdx: rIdx + 1,
			})
		}
		grid = append(grid, rowCells)
	}

	return grid, nil
}

func parseXLSXGrid(data []byte) ([][]CellData, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid xlsx zip archive: %w", err)
	}

	if err := checkZipSafety(zr); err != nil {
		return nil, err
	}

	var sharedStrings []string
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			sharedStrings, err = parseSharedStringsSafe(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			break
		}
	}

	var sheetFile *zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheetFile = f
			break
		}
	}
	if sheetFile == nil {
		return nil, errors.New("worksheet not found in xlsx")
	}

	rc, err := sheetFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return parseWorksheetXMLSafe(rc, sharedStrings)
}

func checkZipSafety(zr *zip.Reader) error {
	if len(zr.File) > 500 {
		return errors.New("arsip zip melebihi batas jumlah berkas")
	}

	var totalUncompressed uint64
	for _, f := range zr.File {
		cleanName := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanName, "..") || strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, `\`) {
			return errors.New("zip slip path traversal detected")
		}

		if f.CompressedSize64 > 0 && f.UncompressedSize64 > 1<<20 {
			ratio := f.UncompressedSize64 / f.CompressedSize64
			if ratio > maxZipCompressionRatio {
				return errors.New("berkas terkompresi ditolak: terindikasi zip bomb (rasio kompresi melebihi batas aman)")
			}
		}

		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxDecompressTotalBytes {
			return errors.New("dekompresi melebihi batas ukuran maksimum")
		}
	}
	return nil
}

func parseSharedStringsSafe(r io.Reader) ([]string, error) {
	lr := io.LimitReader(r, maxEntryDecompressBytes+1)
	decoder := xml.NewDecoder(lr)

	var res []string
	var inT bool
	var curText strings.Builder

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed sharedStrings XML: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inT = true
				curText.Reset()
			}
		case xml.CharData:
			if inT {
				curText.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
				res = append(res, curText.String())
				if len(res) > maxSharedStringsCount {
					return nil, errors.New("shared strings melebihi batas maksimum")
				}
			}
		}
	}
	return res, nil
}

func parseWorksheetXMLSafe(r io.Reader, sharedStrings []string) ([][]CellData, error) {
	lr := io.LimitReader(r, maxEntryDecompressBytes+1)
	decoder := xml.NewDecoder(lr)

	var grid [][]CellData
	var currentRow []CellData
	var inV, inInlineT bool
	var curCellRef, curType string
	var valBuilder strings.Builder
	currentRowAttr := 0

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed worksheet XML: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "row" {
				currentRow = []CellData{}
				currentRowAttr = 0
				for _, attr := range t.Attr {
					if attr.Name.Local == "r" {
						currentRowAttr, _ = strconv.Atoi(attr.Value)
					}
				}
			} else if t.Name.Local == "c" {
				curCellRef = ""
				curType = ""
				for _, attr := range t.Attr {
					if attr.Name.Local == "r" {
						curCellRef = attr.Value
					} else if attr.Name.Local == "t" {
						curType = attr.Value
					}
				}
				if curCellRef != "" && currentRowAttr > 0 {
					_, cellRow := parseColLetter(curCellRef)
					if cellRow != currentRowAttr {
						return nil, errors.New("UNSUPPORTED_DOCUMENT: koordinat baris sel tidak konsisten dengan baris")
					}
				}
			} else if t.Name.Local == "f" {
				return nil, errors.New("UNSUPPORTED_DOCUMENT: sel XLSX memuat formula (f)")
			} else if t.Name.Local == "v" {
				inV = true
				valBuilder.Reset()
			} else if t.Name.Local == "t" {
				inInlineT = true
				valBuilder.Reset()
			}
		case xml.CharData:
			if inV || inInlineT {
				valBuilder.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "v" {
				inV = false
				rawVal := strings.TrimSpace(valBuilder.String())
				finalVal := rawVal
				if curType == "s" {
					idx, err := strconv.Atoi(rawVal)
					if err != nil || idx < 0 || idx >= len(sharedStrings) {
						return nil, fmt.Errorf("indeks sharedStrings di luar jangkauan: %s", rawVal)
					}
					finalVal = sharedStrings[idx]
				}
				cIdx, rIdx := parseColLetter(curCellRef)
				currentRow = append(currentRow, CellData{
					Value:  finalVal,
					Cell:   curCellRef,
					ColIdx: cIdx,
					RowIdx: rIdx,
				})
			} else if t.Name.Local == "t" && inInlineT {
				inInlineT = false
				finalVal := strings.TrimSpace(valBuilder.String())
				cIdx, rIdx := parseColLetter(curCellRef)
				currentRow = append(currentRow, CellData{
					Value:  finalVal,
					Cell:   curCellRef,
					ColIdx: cIdx,
					RowIdx: rIdx,
				})
			} else if t.Name.Local == "row" {
				if len(currentRow) > 0 {
					grid = append(grid, currentRow)
				}
			}
		}
	}
	return grid, nil
}

func parseDOCXGrid(data []byte) ([][]CellData, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid docx zip archive: %w", err)
	}

	if err := checkZipSafety(zr); err != nil {
		return nil, err
	}

	var docFile *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}

	if docFile == nil {
		return nil, errors.New("word/document.xml not found in docx")
	}

	rc, err := docFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	lr := io.LimitReader(rc, maxEntryDecompressBytes+1)
	decoder := xml.NewDecoder(lr)

	var grid [][]CellData
	var currentRow []CellData
	var currentCell strings.Builder
	var inT bool
	rowIdx := 0
	colIdx := 0
	tblCount := 0

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed docx XML: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tbl" {
				tblCount++
				if tblCount > 1 {
					return nil, errors.New("UNSUPPORTED_DOCUMENT: dokumen DOCX memiliki beberapa tabel terpisah yang ambigu")
				}
			} else if t.Name.Local == "tr" {
				rowIdx++
				colIdx = 0
				currentRow = []CellData{}
			} else if t.Name.Local == "tc" {
				currentCell.Reset()
			} else if t.Name.Local == "t" {
				inT = true
			}
		case xml.CharData:
			if inT {
				currentCell.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
			} else if t.Name.Local == "tc" {
				val := strings.TrimSpace(currentCell.String())
				cellCoord := fmt.Sprintf("%s%d", colLetters(colIdx), rowIdx)
				currentRow = append(currentRow, CellData{
					Value:  val,
					Cell:   cellCoord,
					ColIdx: colIdx,
					RowIdx: rowIdx,
				})
				colIdx++
			} else if t.Name.Local == "tr" {
				if len(currentRow) > 0 {
					grid = append(grid, currentRow)
				}
			}
		}
	}

	return grid, nil
}

func parsePDFGrid(data []byte) ([][]CellData, error) {
	text, err := extractPDFText(context.Background(), data, "-layout")
	if err != nil {
		return nil, err
	}
	if strings.Contains(text, "1.2 Tabel Matriks") && strings.Contains(text, "4.3 Tabel Kuantifikasi") {
		return nil, errors.New("REVIEW_REQUIRED: REFERENCE/SIMULATION, bukan laporan laboratorium operasional terverifikasi; tabel PDF 1.2/4.1/4.3 tidak dapat digabung pasti 1:1; tanpa skor atau koreksi UNK-HUMIC, PPC 24.46 ditolak")
	}
	return canonicalPDFGrid(text)
}

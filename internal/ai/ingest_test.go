package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestIngestCSVValidGSI(t *testing.T) {
	csvData := `SampleCode,ct_ahpnd,ct_wssv,ct_ehp,ct_amoa,ct_16s,ct_ppc,do_mg_l,tom_mg_l,ph,salinity_ppt,temp_c,tan_mg_l,no2_mg_l,h2s_mg_l,alkalinity
SMPL-01,34.2,40.0,40.0,19.5,18.4,19.9,5.8,78.0,7.9,21.0,30.0,0.6,0.35,0.002,165.0
`
	res, err := Ingest(context.Background(), "gsi_run.csv", []byte(csvData), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FileType != "CSV" || len(res.Samples) != 1 || (res.Samples[0].Features.CTAmoa == nil || *res.Samples[0].Features.CTAmoa != 19.5) {
		t.Fatalf("%+v", res)
	}
}

func TestIngestWarningsAndDemoDetection(t *testing.T) {
	badCSV := `SampleCode,ct_ahpnd,ct_wssv,ct_ehp,ct_amoa,ct_16s,ct_ppc,ph
DEMO-01,40,40,40,40,40,24.5,15.0
DEMO-01,40,40,40,40,40,24.5,15.0
`
	res, err := Ingest(context.Background(), "demo_corrupted.csv", []byte(badCSV), nil)
	if err != nil {
		t.Fatal(err)
	}
	var hasPPC, hasDup, hasDemo, hasChem bool
	for _, w := range res.Warnings {
		switch w.Code {
		case "PPC_OUT_OF_BOUNDS":
			hasPPC = true
		case "DUPLICATE_SAMPLE_ID":
			hasDup = true
		case "DEMO_DATA_DETECTED":
			hasDemo = true
		case "PHYSICAL_RANGE_VIOLATION":
			hasChem = true
		}
	}
	if !hasPPC || !hasDup || !hasDemo || !hasChem {
		t.Fatalf("expected warnings missing: %+v", res.Warnings)
	}
}

func TestIngestXLSXMinimal(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, _ := zw.Create("xl/sharedStrings.xml")
	fw.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3"><si><t>SampleCode</t></si><si><t>ct_ahpnd</t></si><si><t>SMPL-X1</t></si></sst>`))
	fw, _ = zw.Create("xl/worksheets/sheet1.xml")
	fw.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>32.5</v></c></row></sheetData></worksheet>`))
	zw.Close()
	res, err := Ingest(context.Background(), "test.xlsx", buf.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Samples) != 1 || res.Samples[0].SampleCode != "SMPL-X1" || (res.Samples[0].Features.CTAhpnd == nil || *res.Samples[0].Features.CTAhpnd != 32.5) {
		t.Fatalf("%+v", res)
	}
}

func TestIngestDOCXMinimal(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, _ := zw.Create("word/document.xml")
	fw.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl><w:tr><w:tc><w:p><w:r><w:t>SampleCode</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>ct_ahpnd</w:t></w:r></w:p></w:tc></w:tr><w:tr><w:tc><w:p><w:r><w:t>SMPL-DOCX1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>33.1</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`))
	zw.Close()
	res, err := Ingest(context.Background(), "report.docx", buf.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Samples) != 1 || res.Samples[0].SampleCode != "SMPL-DOCX1" || (res.Samples[0].Features.CTAhpnd == nil || *res.Samples[0].Features.CTAhpnd != 33.1) {
		t.Fatalf("%+v", res)
	}
}

func TestIngestPDFTextAndScanned(t *testing.T) {
	pdfText := `%PDF-1.4
1 0 obj
<< /Length 50 >>
stream
BT /F1 12 Tf (SampleCode,ct_ahpnd\nSMPL-PDF,31.4) Tj ET
endstream
endobj
%%EOF`
	res, err := Ingest(context.Background(), "doc.pdf", []byte(pdfText), nil)
	if err == nil || !strings.Contains(err.Error(), "UNSUPPORTED_DOCUMENT") || len(res.Samples) != 0 {
		t.Fatalf("expected explicit PDF rejection: %+v %v", res, err)
	}

	scannedPDF := `%PDF-1.4
1 0 obj
<< /Type /XObject /Subtype /Image >>
stream
BINARY_IMAGE_DATA_NO_TEXT_STREAMS
endstream
endobj
%%EOF`
	_, err = Ingest(context.Background(), "scanned.pdf", []byte(scannedPDF), nil)
	if err == nil || !strings.Contains(err.Error(), "perlu OCR") {
		t.Fatalf("expected 'perlu OCR' error, got %v", err)
	}
}

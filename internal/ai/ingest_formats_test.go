package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

func zipped(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, text := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestXLSXSparseCellsAndSource(t *testing.T) {
	data := zipped(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="4"><c r="A4" t="inlineStr"><is><t>sample_code</t></is></c><c r="B4" t="inlineStr"><is><t>ct_wssv</t></is></c><c r="C4" t="inlineStr"><is><t>ct_ppc</t></is></c></row><row r="8"><c r="A8" t="inlineStr"><is><t>POND-A</t></is></c><c r="C8"><v>20</v></c></row></sheetData></worksheet>`})
	res, err := Ingest(context.Background(), "sparse.xlsx", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Samples) != 1 || res.Samples[0].Provenance["ct_ppc"].Cell != "C8" {
		t.Fatalf("lost associations: %+v", res)
	}
}
func TestCompressedDocumentsRejectMalformedOrBombs(t *testing.T) {
	for name, data := range map[string][]byte{
		"bomb.docx":    zipped(t, map[string]string{"word/document.xml": strings.Repeat("x", 5<<20)}),
		"xml.docx":     zipped(t, map[string]string{"word/document.xml": "<document><tbl><tr><tc><t>sample_code</t></tc></tr>"}),
		"strings.xlsx": zipped(t, map[string]string{"xl/sharedStrings.xml": "<sst>", "xl/worksheets/sheet1.xml": "<worksheet/>"}),
		"index.xlsx":   zipped(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>999</v></c></row></sheetData></worksheet>`}),
	} {
		t.Run(name, func(t *testing.T) {
			if r, e := Ingest(context.Background(), name, data, nil); e == nil || len(r.Samples) != 0 {
				t.Fatalf("accepted unsafe archive: %+v %v", r, e)
			}
		})
	}
}
func TestPDFNeverGuesses(t *testing.T) {
	for _, data := range []string{"%PDF-1.4 /Encrypt (sample_code,ct_ppc) Tj", "%PDF-1.4 /Subtype /Image", "%PDF-1.4 BT (sample_code,ct_ppc\\nPOND,20) Tj ET"} {
		r, e := Ingest(context.Background(), "document.pdf", []byte(data), nil)
		if e == nil || len(r.Samples) != 0 || (!strings.Contains(e.Error(), "UNSUPPORTED_DOCUMENT") && !strings.Contains(e.Error(), "OCR_REQUIRED")) {
			t.Fatalf("guessed PDF: %+v %v", r, e)
		}
	}
}

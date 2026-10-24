package ai

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIngestMissingDataIsNotZeroOrUndetected(t *testing.T) {
	res, err := Ingest(context.Background(), "partial.csv", []byte("sample_code,ct_ahpnd\nPOND-1,Undetermined\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Samples[0].Features)
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	if fields["ct_wssv"] != nil || fields["do_mg_l"] != nil {
		t.Fatalf("missing measurements became numbers: %s", b)
	}
	if !res.BlockPrediction || !res.Samples[0].BlockPrediction || res.Samples[0].QCStatus != "REVIEW_REQUIRED" {
		t.Fatalf("missing QC passed: %+v", res)
	}
	if len(res.Samples[0].UndetectedTargets) != 1 || res.Samples[0].Provenance["ct_ahpnd"].Cell != "B2" {
		t.Fatalf("lost observation/source: %+v", res.Samples[0])
	}
	if res.Run.LabName != "" || res.Run.Instrument != "" || res.Run.TestedAt != "" {
		t.Fatalf("fabricated metadata: %+v", res.Run)
	}
}

func TestIngestRejectsMalformedInput(t *testing.T) {
	for name, data := range map[string]string{
		"bad.csv":               "sample_code,ct_ppc\nPOND-1,NaN\n",
		"quotes.csv":            "sample_code,ct_ppc\nPOND-1,\"20\n",
		"ragged.csv":            "sample_code,ct_ppc\nPOND-1,20,30\n",
		"duplicate_columns.csv": "sample_code,ct_ppc,PPC\nPOND-1,20,21\n",
		"missing_id.csv":        "ct_ppc,ph\n20,8\n",
		"empty_id.csv":          "sample_code,ct_ppc\n,20\n",
		"units.csv":             "sample_code,do (g/L)\nPOND-1,5\n",
		"binary.doc":            "sample_code,ct_ppc\nPOND-1,20\n",
	} {
		t.Run(name, func(t *testing.T) {
			if res, err := Ingest(context.Background(), name, []byte(data), nil); err == nil || len(res.Samples) != 0 {
				t.Fatalf("unsafe input accepted: %+v %v", res, err)
			}
		})
	}
}

func TestIngestPPCReviewNeverCorrected(t *testing.T) {
	res, err := Ingest(context.Background(), "qc.csv", []byte("sample_code,ct_ppc,ct_ahpnd,ph\nPOND-1,24.46,31.24,15\nPOND-1,20,25,8\nNTC,20,18,8\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range res.Samples {
		if !sample.BlockPrediction {
			t.Fatalf("review row passed: %+v", sample)
		}
	}
	if !res.BlockPrediction {
		t.Fatal("document prediction not blocked")
	}
	if len(res.Samples[0].ReviewReasons) == 0 {
		t.Fatal("expected review reasons")
	}
}

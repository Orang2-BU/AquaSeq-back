package handler

import (
	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/sdri"
)

const (
	demoOwner      = "00000000-0000-0000-0000-000000000001"
	demoTechnician = "00000000-0000-0000-0000-000000000002"
	farmJember     = "00000000-0000-0000-0000-000000000010"
	farmSitubondo  = "00000000-0000-0000-0000-000000000020"
)

func (h *LiveHandler) seed(t tx) error {
	// Check if demo owner already seeded
	if _, err := t.Get("users", demoOwner); err == nil {
		return nil
	}

	pwHash, _ := auth.HashPassword("demo12345678")

	// 1. Seed Users
	_ = t.Put("users", rec{
		"id":            demoOwner,
		"full_name":     "Hendra Kusuma",
		"email":         "hendra@tambak-rejeki.test",
		"password_hash": pwHash,
		"phone_wa":      "6281234567890",
		"created_at":    now(),
	})

	_ = t.Put("users", rec{
		"id":            demoTechnician,
		"full_name":     "Rizky Pratama",
		"email":         "rizky@tambak-rejeki.test",
		"password_hash": pwHash,
		"phone_wa":      "6281234567891",
		"created_at":    now(),
	})

	// 2. Seed Farms (Tambak Sumber Rejeki Puger Jember & Mina Lestari Situbondo)
	_ = t.Put("farms", rec{
		"id":          farmJember,
		"name":        "Tambak Sumber Rejeki Puger Jember",
		"regency":     "Jember",
		"address":     "Jl. Pantai Puger No. 12, Jember, Jawa Timur",
		"owner_id":    demoOwner,
		"provenance":  "simulasi",
		"archived_at": nil,
		"created_at":  now(),
	})

	_ = t.Put("farms", rec{
		"id":          farmSitubondo,
		"name":        "Mina Lestari Situbondo",
		"regency":     "Situbondo",
		"address":     "Kec. Besuki, Situbondo, Jawa Timur",
		"owner_id":    demoOwner,
		"provenance":  "simulasi",
		"archived_at": nil,
		"created_at":  now(),
	})

	// Memberships
	_ = t.Put("memberships", rec{"id": generateUUID(), "farm_id": farmJember, "user_id": demoOwner, "role": "pemilik", "active": true, "created_at": now()})
	_ = t.Put("memberships", rec{"id": generateUUID(), "farm_id": farmJember, "user_id": demoTechnician, "role": "teknisi", "active": true, "created_at": now()})
	_ = t.Put("memberships", rec{"id": generateUUID(), "farm_id": farmSitubondo, "user_id": demoOwner, "role": "pemilik", "active": true, "created_at": now()})
	_ = t.Put("memberships", rec{"id": generateUUID(), "farm_id": farmSitubondo, "user_id": demoTechnician, "role": "teknisi", "active": true, "created_at": now()})

	// 3. Seed Ponds for Farm 1 (Jember)
	pondIDs := []string{
		"00000000-0000-0000-0000-000000000101",
		"00000000-0000-0000-0000-000000000102",
		"00000000-0000-0000-0000-000000000103",
		"00000000-0000-0000-0000-000000000104",
	}
	pondCodes := []string{"B1", "B2", "B3", "B4"}
	pondNames := []string{"Petak B1 Intensif", "Petak B2 Intensif", "Petak B3 Bioflok", "Petak B4 Bioflok"}
	systems := []string{"intensif", "intensif", "bioflok", "bioflok"}

	for i := 0; i < len(pondIDs); i++ {
		_ = t.Put("ponds", rec{
			"id":            pondIDs[i],
			"farm_id":       farmJember,
			"pond_code":     pondCodes[i],
			"name":          pondNames[i],
			"system":        systems[i],
			"area_sqm":      1200.0,
			"current_cycle": 2,
			"stocked_at":    "2026-09-01",
			"assignee_id":   demoTechnician,
			"wa_target":     "6281234567890",
			"status":        "ACTIVE",
			"archived_at":   nil,
			"created_at":    now(),
		})
	}

	// Seed Ponds for Farm 2 (Situbondo)
	pondSitubondo := "00000000-0000-0000-0000-000000000201"
	_ = t.Put("ponds", rec{
		"id":            pondSitubondo,
		"farm_id":       farmSitubondo,
		"pond_code":     "C1",
		"name":          "Petak C1 Tradisional Plus",
		"system":        "intensif",
		"area_sqm":      2500.0,
		"current_cycle": 1,
		"stocked_at":    "2026-09-15",
		"assignee_id":   demoTechnician,
		"wa_target":     "6281234567890",
		"status":        "ACTIVE",
		"archived_at":   nil,
		"created_at":    now(),
	})

	// 4. Seed Samples and SDRI Results for Pond B1
	sampleID := "00000000-0000-0000-0000-000000001001"
	ahpndCt := 22.0
	wssvCt := 40.0
	ehpCt := 30.0
	amoaCt := 28.22
	c16sCt := 18.44
	ppcCt := 19.95
	doVal := 2.65
	tomVal := 128.4
	phVal := 8.4
	salVal := 22.5
	tempVal := 30.0
	tanVal := 3.2
	no2Val := 4.85
	h2sVal := 0.092
	alkVal := 82.0

	feats := domain.SampleFeatures{
		CTAHPND:     &ahpndCt,
		CTWSSV:      &wssvCt,
		CTEHP:       &ehpCt,
		CTamoA:      &amoaCt,
		CT16S:       &c16sCt,
		CTPPC:       ppcCt,
		DOMgL:       &doVal,
		TOMMgL:      &tomVal,
		PH:          &phVal,
		SalinityPPT: &salVal,
		TempC:       &tempVal,
		TANMgL:      &tanVal,
		NO2MgL:      &no2Val,
		H2SMgL:      &h2sVal,
		Alkalinity:  &alkVal,
	}

	assess := sdri.Assess(feats)
	res := assess.Result

	_ = t.Put("samples", rec{
		"id":            sampleID,
		"pond_id":       pondIDs[0],
		"batch_id":      nil,
		"sample_code":   "B1-1024",
		"sampling_date": "2026-10-24",
		"cycle_day":     35,
		"source_type":   "GSI_LAB",
		"meta": rec{
			"wells": []rec{
				{"position": "C7", "target": "pirA", "ct": 22.0, "tm": 81.2, "excluded_reason": nil},
				{"position": "C8", "target": "16S", "ct": 18.44, "tm": 84.5, "excluded_reason": nil},
			},
			"standards": []rec{
				{"copies_rxn": 100000, "ct": 19.8},
			},
		},
		"created_at": now(),
	})

	_ = t.Put("sample_features", rec{
		"id":           generateUUID(),
		"sample_id":    sampleID,
		"ct_ahpnd":     ahpndCt,
		"ct_wssv":      wssvCt,
		"ct_ehp":       ehpCt,
		"ct_amoa":      amoaCt,
		"ct_16s":       c16sCt,
		"ct_ppc":       ppcCt,
		"do_mg_l":      doVal,
		"tom_mg_l":     tomVal,
		"ph":           phVal,
		"salinity_ppt": salVal,
		"temp_c":       tempVal,
		"tan_mg_l":     tanVal,
		"no2_mg_l":     no2Val,
		"h2s_mg_l":     h2sVal,
		"alkalinity":   alkVal,
		"created_at":   now(),
	})

	_ = t.Put("sdri_results", rec{
		"id":                 generateUUID(),
		"sample_id":          sampleID,
		"status":             string(res.Status),
		"z_score":            res.ZScore,
		"probability_pct":    res.ProbabilityPct,
		"risk_class":         int(res.RiskClass),
		"risk_level":         res.RiskLevel,
		"s_qpcr":             res.SQPCR,
		"s_bio":              res.SBio,
		"s_chem":             res.SChem,
		"model_version":      res.ModelVersion,
		"recommendation":     res.Recommendation,
		"predicted_disease":  res.PredictedDisease,
		"disease_name":       res.DiseaseName,
		"evidence":           res.Evidence,
		"mitigation_actions": res.MitigationActions,
		"created_at":         now(),
	})

	// 5. Seed Water Log
	_, _, freeNH3 := sdri.CalculateFreeAmmonia(tanVal, tempVal, phVal)
	_ = t.Put("water_logs", rec{
		"id":              generateUUID(),
		"pond_id":         pondIDs[0],
		"measured_at":     now(),
		"temp_c":          tempVal,
		"ph":              phVal,
		"do_mg_l":         doVal,
		"tan_mg_l":        tanVal,
		"no2_mg_l":        no2Val,
		"h2s_mg_l":        h2sVal,
		"tom_mg_l":        tomVal,
		"alkalinity":      alkVal,
		"salinity_ppt":    salVal,
		"transparency_cm": 35.0,
		"nh3_mg_l":        freeNH3,
		"recorded_by":     demoTechnician,
		"created_at":      now(),
	})

	// 6. Seed Notification
	_ = t.Put("notifications", rec{
		"id":         generateUUID(),
		"user_id":    demoTechnician,
		"kind":       "peringatan_baru",
		"title":      "Peringatan risiko baru pada Petak B1",
		"entity":     "pond",
		"entity_id":  pondIDs[0],
		"read_at":    nil,
		"created_at": now(),
	})

	// 7. Seed Activity Log
	_ = t.Put("activity_log", rec{
		"id":         generateUUID(),
		"farm_id":    farmJember,
		"actor_id":   demoOwner,
		"action":     "BUAT_TAMBAK",
		"entity":     "farm",
		"entity_id":  farmJember,
		"detail":     "Inisialisasi data simulasi tambak Sumber Rejeki Puger Jember.",
		"created_at": now(),
	})

	return nil
}

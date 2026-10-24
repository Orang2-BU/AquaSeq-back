package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"aquaseq-back/internal/ai"
	"aquaseq-back/internal/learning"
	"aquaseq-back/internal/repository"
	"aquaseq-back/internal/sdri"
)

type StorePondProvider struct {
	store repository.WebStore
}

func NewStorePondProvider(store repository.WebStore) *StorePondProvider {
	return &StorePondProvider{store: store}
}

func (p *StorePondProvider) GetTrajectoryPoints(ctx context.Context, pondID string) ([]learning.Sample, error) {
	points := []learning.Sample{}
	err := p.store.Read(ctx, func(t tx) error {
		pond, e := t.Get("ponds", pondID)
		if e != nil {
			return e
		}
		curCycle := int(num(pond, "current_cycle"))
		farm, e := t.Get("farms", str(pond, "farm_id"))
		if e != nil {
			return e
		}
		smps, err := find(t, "samples", "pond_id", pondID)
		if err != nil {
			return err
		}
		sort.Slice(smps, func(i, j int) bool {
			if num(smps[i], "cycle_day") == num(smps[j], "cycle_day") {
				return str(smps[i], "created_at") < str(smps[j], "created_at")
			}
			return num(smps[i], "cycle_day") < num(smps[j], "cycle_day")
		})
		for _, s := range smps {
			sCycle := extractCycleNumber(s)
			if curCycle < 1 || sCycle != curCycle {
				continue
			}
			res, e := first(t, "sdri_results", "sample_id", str(s, "id"))
			if e != nil {
				return e
			}
			if str(res, "status") != "VALID" {
				continue
			}
			featRec, _ := first(t, "sample_features", "sample_id", str(s, "id"))
			featMap := make(map[string]float64)
			for _, k := range featureKeys {
				if featRec != nil && featRec[k] != nil {
					featMap[k] = num(featRec, k)
				}
			}
			day := num(s, "cycle_day")
			if len(points) > 0 && day == points[len(points)-1].Day {
				points = points[:len(points)-1]
			}
			points = append(points, learning.Sample{
				Date:           str(s, "sampling_date"),
				Day:            day,
				ProbabilityPct: num(res, "probability_pct"),
				RiskClass:      int(num(res, "risk_class")),
				Synthetic:      str(farm, "provenance") == "simulasi",
				Features:       featMap,
			})
		}
		return nil
	})
	return points, err
}

func (p *StorePondProvider) GetLatestMachineFacts(ctx context.Context, pondID string) (ai.MachineFacts, error) {
	var facts ai.MachineFacts
	err := p.store.Read(ctx, func(t tx) error {
		pond, e := t.Get("ponds", pondID)
		if e != nil {
			return e
		}
		curCycle := int(num(pond, "current_cycle"))
		allSmps, err := find(t, "samples", "pond_id", pondID)
		if err != nil {
			return err
		}
		var smps []rec
		for _, s := range allSmps {
			sCycle := extractCycleNumber(s)
			if curCycle < 1 || sCycle != curCycle {
				continue
			}
			smps = append(smps, s)
		}
		if len(smps) == 0 {
			return errors.New("data sampel belum tersedia")
		}
		sort.Slice(smps, func(i, j int) bool {
			if num(smps[i], "cycle_day") == num(smps[j], "cycle_day") {
				return str(smps[i], "created_at") < str(smps[j], "created_at")
			}
			return num(smps[i], "cycle_day") < num(smps[j], "cycle_day")
		})
		latest := smps[len(smps)-1]
		res, _ := first(t, "sdri_results", "sample_id", str(latest, "id"))
		feats, _ := first(t, "sample_features", "sample_id", str(latest, "id"))

		if str(res, "status") != "VALID" {
			return errors.New("kontrol kualitas gagal; penjelasan risiko tidak tersedia")
		}
		f := parseFeatures(feats)
		a := sdri.Assess(f)
		if a.Result.Status != "VALID" {
			return errors.New("fitur belum cukup")
		}
		disease := str(res, "predicted_disease")
		if disease == "" {
			return errors.New("panel jejak penyakit belum tersedia")
		}
		nh3 := *a.NH3MgL
		facts = ai.MachineFacts{
			PondID:                 pondID,
			Disease:                disease,
			SDRIClass:              int(num(res, "risk_class")),
			RiskLevel:              str(res, "risk_level"),
			ProbabilityPct:         num(res, "probability_pct"),
			NH3MgL:                 nh3,
			TOMMgL:                 num(feats, "tom_mg_l"),
			DOMgL:                  num(feats, "do_mg_l"),
			ForecastHorizonDays:    0,
			ForecastProbabilityPct: 0,
			DataAvailable:          true,
			QCStatus:               "VALID",
			ChemistryAvailable:     true,
		}
		return nil
	})
	return facts, err
}

func extractCycleNumber(s rec) int {
	if n := int(num(s, "cycle_number")); n > 0 {
		return n
	}
	rawMeta, ok := s["meta"]
	if !ok || rawMeta == nil {
		return 0
	}
	switch m := rawMeta.(type) {
	case map[string]any:
		if v, ok := m["cycle_number"]; ok && v != nil {
			switch cv := v.(type) {
			case float64:
				return int(cv)
			case int:
				return cv
			case int32:
				return int(cv)
			case int64:
				return int(cv)
			}
		}
	case rec:
		if n := int(num(m, "cycle_number")); n > 0 {
			return n
		}
	case string:
		var parsed map[string]any
		if json.Unmarshal([]byte(m), &parsed) == nil {
			if v, ok := parsed["cycle_number"]; ok && v != nil {
				if f, ok := v.(float64); ok {
					return int(f)
				}
			}
		}
	case []byte:
		var parsed map[string]any
		if json.Unmarshal(m, &parsed) == nil {
			if v, ok := parsed["cycle_number"]; ok && v != nil {
				if f, ok := v.(float64); ok {
					return int(f)
				}
			}
		}
	}
	return 0
}

func (p *StorePondProvider) RetrieveSimilarCases(ctx context.Context, pondID string, cb *learning.CaseBase, query map[string]float64, k int) ([]learning.CaseMatch, error) {
	// Rebuild per request: cached cases can outlive QC corrections or cycle changes.
	cb = &learning.CaseBase{};
	var farmID string
	var isSynthetic bool

	err := p.store.Read(ctx, func(t tx) error {
		pond, e := t.Get("ponds", pondID)
		if e != nil {
			return e
		}
		farmID = str(pond, "farm_id")
		curCycle := int(num(pond, "current_cycle"))

		farm, e := t.Get("farms", farmID)
		if e != nil {
			return e
		}
		provenance := str(farm, "provenance")
		if provenance != "nyata" && provenance != "simulasi" {
			return errors.New("provenance belum tersedia")
		}
		isSynthetic = (provenance == "simulasi")

		ponds, err := find(t, "ponds", "farm_id", farmID)
		if err != nil {
			return err
		}

		for _, pRec := range ponds {
			pID := str(pRec, "id")
			outcomes, _ := find(t, "cycle_outcomes", "pond_id", pID)
			cycleOutcomes := make(map[int]string)
			for _, co := range outcomes {
				cNum := int(num(co, "cycle_number"))
				cOut := strings.TrimSpace(str(co, "outcome"))
				if cNum > 0 && has([]string{"HARVEST_SUCCESS", "OUTBREAK", "EMERGENCY_HARVEST"}, cOut) {
					cycleOutcomes[cNum] = cOut
				}
			}
			if len(cycleOutcomes) == 0 {
				continue
			}

			tasks, _ := find(t, "tasks", "pond_id", pID)
			taskBySample := make(map[string]rec)
			for _, tk := range tasks {
				st := str(tk, "status")
				src := str(tk, "source_sample_id")
				if str(tk, "farm_id") == farmID && src != "" && (st == "dilaporkan" || st == "selesai") {
					taskBySample[src] = tk
				}
			}
			if len(taskBySample) == 0 {
				continue
			}

			smps, _ := find(t, "samples", "pond_id", pID)
			for _, s := range smps {
				sCycle := extractCycleNumber(s)
				if sCycle <= 0 {
					continue
				}
				if pID == pondID && sCycle == curCycle {
					continue
				}
				outcome, hasOutcome := cycleOutcomes[sCycle]
				if !hasOutcome {
					continue
				}
				tk, hasTask := taskBySample[str(s, "id")]
				if !hasTask {
					continue
				}

				reports, _ := find(t, "task_reports", "task_id", str(tk, "id"))
				var action string
				if len(reports) > 0 {
					action = strings.TrimSpace(str(reports[len(reports)-1], "note"))
				}
				if action == "" {
					action = strings.TrimSpace(str(tk, "instruction"))
				}
				if action == "" {
					action = strings.TrimSpace(str(tk, "title"))
				}
				if action == "" {
					continue
				}

				res, e := first(t, "sdri_results", "sample_id", str(s, "id"))
				if e != nil || res == nil || str(res, "status") != "VALID" {
					continue
				}
				sdriClass := int(num(res, "risk_class"))
				if sdriClass < 1 || sdriClass > 4 {
					continue
				}

				featRec, _ := first(t, "sample_features", "sample_id", str(s, "id"))
				if featRec == nil {
					continue
				}
				featMap := make(map[string]float64)
				for _, k := range featureKeys {
					if featRec[k] != nil {
						n := num(featRec, k)
						if !math.IsNaN(n) && !math.IsInf(n, 0) {
							featMap[k] = n
						}
					}
				}
				if len(featMap) == 0 {
					continue
				}

				caseKey := fmt.Sprintf("%s-%s-%d", farmID, pID, sCycle)
				_ = cb.Add(learning.Case{
					ID:         caseKey,
					FarmID:     farmID,
					PondID:     pID,
					CycleID:    fmt.Sprintf("cycle-%d", sCycle),
					Scope:      learning.CaseScopePrivate,
					Provenance: provenance,
					Features:   featMap,
					SDRIClass:  sdriClass,
					Action:     action,
					Outcome:    outcome,
					Synthetic:  isSynthetic,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(query) == 0 || k < 1 {
		return []learning.CaseMatch{}, nil
	}

	matches, err := cb.RetrieveScoped(farmID, isSynthetic, query, k)
	if err != nil {
		return []learning.CaseMatch{}, nil
	}
	return matches, nil
}

// Pond authorization is separate so an authenticated outsider receives 403, not 401.
func (h *LiveHandler) Authenticate(r *http.Request) (string, string, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return "", "", unauthorized()
	}
	user, e := h.cfg.Auth.Verify(strings.TrimPrefix(header, "Bearer "))
	if e != nil {
		return "", "", unauthorized()
	}
	role := ""
	e = h.store.Read(r.Context(), func(t tx) error {
		if _, e := t.Get("users", user); e != nil {
			return unauthorized()
		}
		ms, e := find(t, "memberships", "user_id", user)
		if e != nil {
			return e
		}
		owner, tech := false, false
		for _, m := range ms {
			if boolean(m, "active") {
				if str(m, "role") == "pemilik" {
					owner = true
				} else {
					tech = true
				}
			}
		}
		if owner {
			role = "pemilik"
		} else if tech {
			role = "teknisi"
		}
		return nil
	})
	return user, role, e
}
func (h *LiveHandler) AuthorizePond(ctx context.Context, user, id string, owner bool) error {
	return h.store.Read(ctx, func(t tx) error { _, e := h.pondAccess(t, user, id, owner); return e })
}

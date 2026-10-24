package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"aquaseq-back/internal/domain"
)

type Record map[string]any

type WebStore interface {
	Read(context.Context, func(WebTx) error) error
	Write(context.Context, func(WebTx) error) error
}

type WebTx interface {
	Put(table string, r Record) error
	Get(table, id string) (Record, error)
	Delete(table, id string) error
	List(table string) ([]Record, error)
	Find(table string, match func(Record) bool) ([]Record, error)
	FindEqual(table, key, value string) ([]Record, error)
}

type MemoryStore struct {
	mu     sync.RWMutex
	tables map[string]map[string]Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{tables: make(map[string]map[string]Record)}
}

type memTx struct {
	tables map[string]map[string]Record
	err    error
}

func cloneVal(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v2 := range val {
			out[k] = cloneVal(v2)
		}
		return out
	case Record:
		out := make(Record, len(val))
		for k, v2 := range val {
			out[k] = cloneVal(v2)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, v2 := range val {
			out[i] = cloneVal(v2)
		}
		return out
	case []Record:
		out := make([]Record, len(val))
		for i, v2 := range val {
			out[i] = cloneVal(v2).(Record)
		}
		return out
	default:
		return v
	}
}

func (m *MemoryStore) copyTables() map[string]map[string]Record {
	out := make(map[string]map[string]Record, len(m.tables))
	for tbl, rows := range m.tables {
		out[tbl] = make(map[string]Record, len(rows))
		for k, v := range rows {
			rec := make(Record, len(v))
			for rk, rv := range v {
				rec[rk] = cloneVal(rv)
			}
			out[tbl][k] = rec
		}
	}
	return out
}

func (m *MemoryStore) Read(ctx context.Context, fn func(WebTx) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fn(&memTx{tables: m.copyTables()})
}

func (m *MemoryStore) Write(ctx context.Context, fn func(WebTx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := m.copyTables()
	tx := &memTx{tables: snapshot}
	if err := fn(tx); err != nil {
		return err
	}
	if tx.err != nil {
		return tx.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.tables = tx.tables
	return nil
}

func (tx *memTx) Put(table string, r Record) error {
	id, ok := r["id"].(string)
	if !ok || id == "" {
		tx.err = errors.New("record id required")
		return tx.err
	}
	if tx.tables[table] == nil {
		tx.tables[table] = make(map[string]Record)
	}
	tx.tables[table][id] = r
	return nil
}

func (tx *memTx) Get(table, id string) (Record, error) {
	rows := tx.tables[table]
	if rows == nil {
		return nil, ErrNotFound
	}
	r, ok := rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

func (tx *memTx) Delete(table, id string) error {
	if tx.tables[table] != nil {
		delete(tx.tables[table], id)
	}
	return nil
}

func (tx *memTx) List(table string) ([]Record, error) {
	rows := tx.tables[table]
	out := make([]Record, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	return out, nil
}

func (tx *memTx) Find(table string, match func(Record) bool) ([]Record, error) {
	rows := tx.tables[table]
	out := make([]Record, 0)
	for _, r := range rows {
		if match(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

var equalityKeys = map[string][]string{
	"users":             {"id", "email"},
	"refresh_tokens":    {"id", "token_hash", "user_id", "family_id"},
	"password_resets":   {"id", "token_hash", "user_id"},
	"farms":             {"id", "owner_id"},
	"memberships":       {"id", "farm_id", "user_id"},
	"ponds":             {"id", "farm_id", "assignee_id"},
	"batches":           {"id", "farm_id"},
	"samples":           {"id", "pond_id", "batch_id"},
	"sample_features":   {"id", "sample_id"},
	"sdri_results":      {"id", "sample_id"},
	"water_logs":        {"id", "pond_id"},
	"alerts":            {"id", "farm_id"},
	"tasks":             {"id", "farm_id", "pond_id", "assignee_id"},
	"task_reports":      {"id", "task_id"},
	"task_history":      {"id", "task_id"},
	"invitations":       {"id", "token_hash", "farm_id"},
	"notifications":     {"id", "user_id"},
	"activity_log":      {"id", "farm_id"},
	"cycle_outcomes":    {"id", "pond_id"},
	"files":             {"id", "farm_id", "pond_id"},
	"trajectory_points": {"id", "pond_id"},
	"mobile_readings":   {"id", "pond_id", "user_id"},
	"mobile_schedules":  {"id", "schedule_key"},
}

func validateEquality(table, key string) error {
	for _, allowed := range equalityKeys[table] {
		if key == allowed {
			return nil
		}
	}
	return errors.New("unsupported equality lookup")
}

func (tx *memTx) FindEqual(table, key, value string) ([]Record, error) {
	if err := validateEquality(table, key); err != nil {
		return nil, err
	}
	return tx.Find(table, func(r Record) bool { return r[key] != nil && fmt.Sprint(r[key]) == value })
}

// MemoryStore implements legacy Repository interface
func (m *MemoryStore) SaveFarm(ctx context.Context, farm domain.Farm) error {
	return m.Write(ctx, func(tx WebTx) error {
		if farm.CreatedAt.IsZero() {
			farm.CreatedAt = time.Now()
		}
		return tx.Put("farms", Record{
			"id":          farm.ID,
			"name":        farm.Name,
			"regency":     farm.Location,
			"owner_id":    farm.OwnerName,
			"owner_phone": farm.OwnerPhone,
			"created_at":  farm.CreatedAt,
		})
	})
}

func (m *MemoryStore) GetFarms(ctx context.Context) ([]domain.Farm, error) {
	var out []domain.Farm
	err := m.Read(ctx, func(tx WebTx) error {
		rows, e := tx.List("farms")
		if e != nil {
			return e
		}
		for _, r := range rows {
			id, _ := r["id"].(string)
			name, _ := r["name"].(string)
			loc, _ := r["regency"].(string)
			own, _ := r["owner_id"].(string)
			ph, _ := r["owner_phone"].(string)
			ca, _ := r["created_at"].(time.Time)
			out = append(out, domain.Farm{ID: id, Name: name, Location: loc, OwnerName: own, OwnerPhone: ph, CreatedAt: ca})
		}
		return nil
	})
	return out, err
}

func (m *MemoryStore) SavePond(ctx context.Context, pond domain.Pond) error {
	return m.Write(ctx, func(tx WebTx) error {
		if pond.CreatedAt.IsZero() {
			pond.CreatedAt = time.Now()
		}
		return tx.Put("ponds", Record{
			"id":            pond.ID,
			"farm_id":       pond.FarmID,
			"pond_code":     pond.PondCode,
			"area_sqm":      pond.AreaSqm,
			"current_cycle": pond.CurrentCycle,
			"status":        pond.Status,
			"created_at":    pond.CreatedAt,
		})
	})
}

func (m *MemoryStore) GetPond(ctx context.Context, id string) (domain.Pond, error) {
	var pond domain.Pond
	err := m.Read(ctx, func(tx WebTx) error {
		r, e := tx.Get("ponds", id)
		if e != nil {
			return e
		}
		fid, _ := r["farm_id"].(string)
		code, _ := r["pond_code"].(string)
		area, _ := r["area_sqm"].(float64)
		cc, _ := r["current_cycle"].(int)
		st, _ := r["status"].(string)
		ca, _ := r["created_at"].(time.Time)
		pond = domain.Pond{ID: id, FarmID: fid, PondCode: code, AreaSqm: area, CurrentCycle: cc, Status: st, CreatedAt: ca}
		return nil
	})
	return pond, err
}

func (m *MemoryStore) SaveSampleWithResult(ctx context.Context, sample domain.Sample, res domain.SDRIResult) error {
	return m.Write(ctx, func(tx WebTx) error {
		if sample.CreatedAt.IsZero() {
			sample.CreatedAt = time.Now()
		}
		_ = tx.Put("samples", Record{
			"id":            sample.ID,
			"pond_id":       sample.PondID,
			"sampling_date": sample.SamplingDate,
			"cycle_day":     sample.CycleDay,
			"created_at":    sample.CreatedAt,
		})
		_ = tx.Put("sdri_results", Record{
			"id":              "res-" + sample.ID,
			"sample_id":       sample.ID,
			"probability_pct": res.ProbabilityPct,
			"risk_class":      int(res.RiskClass),
			"status":          string(res.Status),
		})
		pointRec := Record{
			"id":              "pt-" + sample.ID,
			"pond_id":         sample.PondID,
			"sampling_date":   sample.SamplingDate,
			"cycle_day":       sample.CycleDay,
			"probability_pct": res.ProbabilityPct,
			"risk_class":      int(res.RiskClass),
			"created_at":      sample.CreatedAt,
		}
		return tx.Put("trajectory_points", pointRec)
	})
}

func (m *MemoryStore) GetTrajectoryHistory(ctx context.Context, pondID string) ([]domain.TrajectoryPoint, error) {
	var out []domain.TrajectoryPoint
	err := m.Read(ctx, func(tx WebTx) error {
		rows, e := tx.FindEqual("trajectory_points", "pond_id", pondID)
		if e != nil {
			return e
		}
		for _, r := range rows {
			sd, _ := r["sampling_date"].(string)
			cd, _ := r["cycle_day"].(int)
			prob, _ := r["probability_pct"].(float64)
			rc, _ := r["risk_class"].(int)
			out = append(out, domain.TrajectoryPoint{
				SamplingDate:   sd,
				CycleDay:       cd,
				ProbabilityPct: prob,
				RiskClass:      rc,
			})
		}
		return nil
	})
	return out, err
}

func (m *MemoryStore) SaveCycleOutcome(ctx context.Context, outcome domain.CycleOutcome) error {
	return m.Write(ctx, func(tx WebTx) error {
		if outcome.CreatedAt.IsZero() {
			outcome.CreatedAt = time.Now()
		}
		return tx.Put("cycle_outcomes", Record{
			"id":                       outcome.ID,
			"pond_id":                  outcome.PondID,
			"cycle_number":             outcome.CycleNumber,
			"outcome":                  outcome.Outcome,
			"actual_harvest_kg":        outcome.ActualHarvestKg,
			"outbreak_date":            outcome.OutbreakDate,
			"primary_suspect_pathogen": outcome.PrimarySuspectPathogen,
			"notes":                    outcome.Notes,
			"created_at":               outcome.CreatedAt,
		})
	})
}

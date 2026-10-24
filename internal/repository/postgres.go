package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"aquaseq-back/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, dsn string, maxConns int) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	store := &PostgresStore{pool: pool}
	if err := store.autoMigrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("auto-migration failed: %w", err)
	}

	return store, nil
}

func (p *PostgresStore) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

func (p *PostgresStore) autoMigrate(ctx context.Context) error {
	candidates := []string{"sql/schema.sql", "../../sql/schema.sql", "../sql/schema.sql", "schema.sql"}
	var sqlContent string
	for _, c := range candidates {
		if b, err := os.ReadFile(c); err == nil {
			sqlContent = string(b)
			break
		}
	}
	if sqlContent == "" {
		return errors.New("file sql/schema.sql tidak ditemukan untuk auto-migration")
	}
	_, err := p.pool.Exec(ctx, sqlContent)
	return err
}

type pgTx struct {
	ctx   context.Context
	tx    pgx.Tx
	write bool
	err   error
}

func (p *PostgresStore) Read(ctx context.Context, fn func(WebTx) error) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	return fn(&pgTx{tx: tx, ctx: ctx})
}

func (p *PostgresStore) Write(ctx context.Context, fn func(WebTx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	t := &pgTx{tx: tx, ctx: ctx, write: true}
	if err := fn(t); err != nil {
		return err
	}
	if t.err != nil {
		return t.err
	}
	return tx.Commit(ctx)
}

func (t *pgTx) Put(table string, r Record) (err error) {
	defer func() {
		if err != nil && t.err == nil {
			t.err = err
		}
	}()
	id, ok := r["id"].(string)
	if !ok || id == "" {
		return errors.New("record id required")
	}

	cols := make([]string, 0, len(r))
	placeholders := make([]string, 0, len(r))
	updates := make([]string, 0, len(r))
	vals := make([]any, 0, len(r))
	idx := 1

	for k, v := range r {
		cols = append(cols, pgx.Identifier{k}.Sanitize())
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx))
		if k != "id" {
			updates = append(updates, fmt.Sprintf("%s = EXCLUDED.%s", pgx.Identifier{k}.Sanitize(), pgx.Identifier{k}.Sanitize()))
		}
		// Convert map/slice to JSON bytes for jsonb columns
		switch val := v.(type) {
		case map[string]any, []any, []string:
			jb, err := json.Marshal(val)
			if err != nil {
				return err
			}
			vals = append(vals, jb)
		default:
			vals = append(vals, v)
		}
		idx++
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (id) DO UPDATE SET %s",
		pgx.Identifier{table}.Sanitize(),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		strings.Join(updates, ", "),
	)
	if len(updates) == 0 {
		query = fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (id) DO NOTHING",
			pgx.Identifier{table}.Sanitize(),
			strings.Join(cols, ", "),
			strings.Join(placeholders, ", "),
		)
	}

	_, err = t.tx.Exec(t.ctx, query, vals...)
	return err
}

func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case [16]byte:
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", val[0:4], val[4:6], val[6:8], val[8:10], val[10:16])
	case []byte:
		// Check if it's JSON or UUID bytes
		if len(val) == 16 {
			return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", val[0:4], val[4:6], val[6:8], val[8:10], val[10:16])
		}
		var parsed any
		if json.Unmarshal(val, &parsed) == nil {
			return parsed
		}
		return string(val)
	case pgtype.Numeric:
		f, _ := val.Float64Value()
		if f.Valid {
			return f.Float64
		}
		return 0.0
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	default:
		return val
	}
}

func (t *pgTx) Get(table, id string) (Record, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE id = $1 LIMIT 1", pgx.Identifier{table}.Sanitize())
	if t.write {
		query += " FOR UPDATE"
	}
	rows, err := t.tx.Query(t.ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}

	fields := rows.FieldDescriptions()
	values, err := rows.Values()
	if err != nil {
		return nil, err
	}

	rec := make(Record, len(fields))
	for i, f := range fields {
		rec[string(f.Name)] = normalizeValue(values[i])
		if f.DataTypeOID == 1082 {
			if d, ok := values[i].(time.Time); ok {
				rec[string(f.Name)] = d.Format("2006-01-02")
			}
		}
	}
	return rec, nil
}

func (t *pgTx) Delete(table, id string) error {
	_, err := t.tx.Exec(t.ctx, fmt.Sprintf("DELETE FROM %s WHERE id = $1", pgx.Identifier{table}.Sanitize()), id)
	return err
}

func (t *pgTx) List(table string) ([]Record, error) {
	return t.queryRecords(fmt.Sprintf("SELECT * FROM %s", pgx.Identifier{table}.Sanitize()))
}

func (t *pgTx) FindEqual(table, key, value string) ([]Record, error) {
	if err := validateEquality(table, key); err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s = $1 ORDER BY id", pgx.Identifier{table}.Sanitize(), pgx.Identifier{key}.Sanitize())
	if t.write && (table == "memberships" || table == "invitations" || table == "password_resets") {
		query += " FOR UPDATE"
	}
	return t.queryRecords(query, value)
}

func (t *pgTx) queryRecords(query string, args ...any) ([]Record, error) {
	rows, err := t.tx.Query(t.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Record
	for rows.Next() {
		fields := rows.FieldDescriptions()
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		rec := make(Record, len(fields))
		for i, f := range fields {
			rec[string(f.Name)] = normalizeValue(values[i])
			if f.DataTypeOID == 1082 {
				if d, ok := values[i].(time.Time); ok {
					rec[string(f.Name)] = d.Format("2006-01-02")
				}
			}
		}
		list = append(list, rec)
	}
	return list, rows.Err()
}

func (t *pgTx) Find(table string, match func(Record) bool) ([]Record, error) {
	all, err := t.List(table)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0)
	for _, r := range all {
		if match(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// Legacy repository implementation on PostgresStore
func (p *PostgresStore) SaveFarm(ctx context.Context, farm domain.Farm) error {
	return p.Write(ctx, func(tx WebTx) error {
		if farm.CreatedAt.IsZero() {
			farm.CreatedAt = time.Now()
		}
		return tx.Put("farms", Record{
			"id":         farm.ID,
			"name":       farm.Name,
			"regency":    farm.Location,
			"owner_id":   farm.OwnerName,
			"created_at": farm.CreatedAt,
		})
	})
}

func (p *PostgresStore) GetFarms(ctx context.Context) ([]domain.Farm, error) {
	var out []domain.Farm
	err := p.Read(ctx, func(tx WebTx) error {
		rows, e := tx.List("farms")
		if e != nil {
			return e
		}
		for _, r := range rows {
			id, _ := r["id"].(string)
			name, _ := r["name"].(string)
			reg, _ := r["regency"].(string)
			own, _ := r["owner_id"].(string)
			ca, _ := r["created_at"].(time.Time)
			out = append(out, domain.Farm{ID: id, Name: name, Location: reg, OwnerName: own, CreatedAt: ca})
		}
		return nil
	})
	return out, err
}

func (p *PostgresStore) SavePond(ctx context.Context, pond domain.Pond) error {
	return p.Write(ctx, func(tx WebTx) error {
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

func (p *PostgresStore) GetPond(ctx context.Context, id string) (domain.Pond, error) {
	var pond domain.Pond
	err := p.Read(ctx, func(tx WebTx) error {
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

func (p *PostgresStore) SaveSampleWithResult(ctx context.Context, sample domain.Sample, res domain.SDRIResult) error {
	return p.Write(ctx, func(tx WebTx) error {
		if sample.CreatedAt.IsZero() {
			sample.CreatedAt = time.Now()
		}
		_ = tx.Put("samples", Record{
			"id":            sample.ID,
			"pond_id":       sample.PondID,
			"sample_code":   sample.ID,
			"sampling_date": sample.SamplingDate,
			"cycle_day":     sample.CycleDay,
			"created_at":    sample.CreatedAt,
		})
		return tx.Put("sdri_results", Record{
			"id":              "res-" + sample.ID,
			"sample_id":       sample.ID,
			"probability_pct": res.ProbabilityPct,
			"risk_class":      int(res.RiskClass),
			"risk_level":      res.RiskLevel,
			"status":          string(res.Status),
			"model_version":   res.ModelVersion,
		})
	})
}

func (p *PostgresStore) GetTrajectoryHistory(ctx context.Context, pondID string) ([]domain.TrajectoryPoint, error) {
	rows, err := p.pool.Query(ctx, "SELECT s.sampling_date, s.cycle_day, r.probability_pct, r.risk_class FROM samples s JOIN sdri_results r ON s.id = r.sample_id WHERE s.pond_id = $1 ORDER BY s.sampling_date ASC", pondID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.TrajectoryPoint
	for rows.Next() {
		var sd string
		var cd, rc int
		var prob float64
		var tDate time.Time
		// handle date parsing
		if err := rows.Scan(&tDate, &cd, &prob, &rc); err == nil {
			sd = tDate.Format("2006-01-02")
		} else {
			_ = rows.Scan(&sd, &cd, &prob, &rc)
		}
		out = append(out, domain.TrajectoryPoint{
			SamplingDate:   sd,
			CycleDay:       cd,
			ProbabilityPct: prob,
			RiskClass:      rc,
		})
	}
	return out, rows.Err()
}

func (p *PostgresStore) SaveCycleOutcome(ctx context.Context, outcome domain.CycleOutcome) error {
	return p.Write(ctx, func(tx WebTx) error {
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

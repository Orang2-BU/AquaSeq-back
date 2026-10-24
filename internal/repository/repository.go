package repository

import (
	"context"
	"errors"
	"sync"
	"time"

	"aquaseq-back/internal/domain"
)

var (
	ErrNotFound = errors.New("resource not found")
)

type Repository interface {
	SaveFarm(ctx context.Context, farm domain.Farm) error
	GetFarms(ctx context.Context) ([]domain.Farm, error)
	SavePond(ctx context.Context, pond domain.Pond) error
	GetPond(ctx context.Context, id string) (domain.Pond, error)
	SaveSampleWithResult(ctx context.Context, sample domain.Sample, res domain.SDRIResult) error
	GetTrajectoryHistory(ctx context.Context, pondID string) ([]domain.TrajectoryPoint, error)
	SaveCycleOutcome(ctx context.Context, outcome domain.CycleOutcome) error
}

// InMemRepository provides a concurrent-safe in-memory store for unit testing & zero-setup demos.
// ponytail: in-memory store for development/testing; real postgres repository handles production queries.
type InMemRepository struct {
	mu       sync.RWMutex
	farms    map[string]domain.Farm
	ponds    map[string]domain.Pond
	samples  map[string]domain.Sample
	results  map[string]domain.SDRIResult        // sampleID -> result
	history  map[string][]domain.TrajectoryPoint // pondID -> points
	outcomes map[string]domain.CycleOutcome
}

func NewInMemRepository() *InMemRepository {
	return &InMemRepository{
		farms:    make(map[string]domain.Farm),
		ponds:    make(map[string]domain.Pond),
		samples:  make(map[string]domain.Sample),
		results:  make(map[string]domain.SDRIResult),
		history:  make(map[string][]domain.TrajectoryPoint),
		outcomes: make(map[string]domain.CycleOutcome),
	}
}

func (r *InMemRepository) SaveFarm(ctx context.Context, farm domain.Farm) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if farm.CreatedAt.IsZero() {
		farm.CreatedAt = time.Now()
	}
	r.farms[farm.ID] = farm
	return nil
}

func (r *InMemRepository) GetFarms(ctx context.Context) ([]domain.Farm, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]domain.Farm, 0, len(r.farms))
	for _, f := range r.farms {
		list = append(list, f)
	}
	return list, nil
}

func (r *InMemRepository) SavePond(ctx context.Context, pond domain.Pond) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pond.CreatedAt.IsZero() {
		pond.CreatedAt = time.Now()
	}
	r.ponds[pond.ID] = pond
	return nil
}

func (r *InMemRepository) GetPond(ctx context.Context, id string) (domain.Pond, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.ponds[id]
	if !ok {
		return domain.Pond{}, ErrNotFound
	}
	return p, nil
}

func (r *InMemRepository) SaveSampleWithResult(ctx context.Context, sample domain.Sample, res domain.SDRIResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sample.CreatedAt.IsZero() {
		sample.CreatedAt = time.Now()
	}
	r.samples[sample.ID] = sample
	r.results[sample.ID] = res

	// Append to trajectory history for the pond
	point := domain.TrajectoryPoint{
		SamplingDate:   sample.SamplingDate,
		CycleDay:       sample.CycleDay,
		ProbabilityPct: res.ProbabilityPct,
		RiskClass:      int(res.RiskClass),
	}
	r.history[sample.PondID] = append(r.history[sample.PondID], point)
	return nil
}

func (r *InMemRepository) GetTrajectoryHistory(ctx context.Context, pondID string) ([]domain.TrajectoryPoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pts, ok := r.history[pondID]
	if !ok {
		return []domain.TrajectoryPoint{}, nil
	}
	// Return a copy
	out := make([]domain.TrajectoryPoint, len(pts))
	copy(out, pts)
	return out, nil
}

func (r *InMemRepository) SaveCycleOutcome(ctx context.Context, outcome domain.CycleOutcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if outcome.CreatedAt.IsZero() {
		outcome.CreatedAt = time.Now()
	}
	r.outcomes[outcome.ID] = outcome
	return nil
}

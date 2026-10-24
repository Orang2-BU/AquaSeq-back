package learning

import (
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
)

const (
	CaseScopePrivate = "PRIVATE"
	CaseScopePublic  = "PUBLIC_KNOWLEDGE"
)

type Case struct {
	ID         string             `json:"id"`
	FarmID     string             `json:"farm_id"`
	PondID     string             `json:"pond_id"`
	CycleID    string             `json:"cycle_id"`
	Scope      string             `json:"scope"`
	Provenance string             `json:"provenance"`
	Features   map[string]float64 `json:"features"`
	SDRIClass  int                `json:"sdri_class"`
	Action     string             `json:"action"`
	Outcome    string             `json:"outcome"`
	Synthetic  bool               `json:"synthetic"`
}

type CaseMatch struct {
	Case      Case    `json:"case"`
	Distance  float64 `json:"distance"`
	Synthetic bool    `json:"synthetic"`
}

type CaseBase struct {
	mu    sync.RWMutex
	cases []Case
}

func cloneCase(c Case) Case {
	m := make(map[string]float64, len(c.Features))
	for k, v := range c.Features {
		m[k] = v
	}
	c.Features = m
	return c
}

func (b *CaseBase) Add(c Case) error {
	if strings.TrimSpace(c.ID) == "" || len(c.Features) == 0 || strings.TrimSpace(c.Action) == "" || strings.TrimSpace(c.Outcome) == "" || strings.TrimSpace(c.Provenance) == "" || c.SDRIClass < 0 || c.SDRIClass > 4 {
		return errors.New("incomplete case or provenance")
	}
	switch c.Scope {
	case CaseScopePrivate:
		if strings.TrimSpace(c.FarmID) == "" || strings.TrimSpace(c.PondID) == "" || strings.TrimSpace(c.CycleID) == "" {
			return errors.New("private case requires farm, pond and cycle")
		}
	case CaseScopePublic:
		// Only explicitly curated public knowledge belongs here, never anonymized private cases.
		if c.FarmID != "" || c.PondID != "" || c.CycleID != "" {
			return errors.New("public knowledge cannot contain tenant identifiers")
		}
	default:
		return errors.New("explicit case scope required")
	}
	for key, v := range c.Features {
		if strings.TrimSpace(key) == "" || !finite(v) {
			return errors.New("invalid case feature")
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, old := range b.cases {
		if old.ID == c.ID && old.FarmID == c.FarmID && old.Scope == c.Scope {
			return errors.New("duplicate case ID in scope")
		}
	}
	b.cases = append(b.cases, cloneCase(c))
	return nil
}

// Deprecated: compilation compatibility only. Unscoped retrieval always fails closed.
func (b *CaseBase) Retrieve(features map[string]float64, k int) ([]CaseMatch, error) {
	return nil, errors.New("explicit farm and provenance required; use RetrieveScoped")
}

// RetrieveScoped requires a server-authorized farm ID and explicit real/synthetic provenance.
// Neither results nor normalization ranges include another farm or provenance.
func (b *CaseBase) RetrieveScoped(farmID string, synthetic bool, query map[string]float64, k int) ([]CaseMatch, error) {
	return b.retrieve(farmID, synthetic, CaseScopePrivate, query, k)
}

// RetrievePublic searches only explicitly published knowledge, never private cases.
func (b *CaseBase) RetrievePublic(farmID string, synthetic bool, query map[string]float64, k int) ([]CaseMatch, error) {
	return b.retrieve(farmID, synthetic, CaseScopePublic, query, k)
}

func (b *CaseBase) retrieve(farmID string, synthetic bool, scope string, query map[string]float64, k int) ([]CaseMatch, error) {
	if strings.TrimSpace(farmID) == "" {
		return nil, errors.New("authorized farm ID required")
	}
	if k < 1 || k > 100 || len(query) == 0 {
		return nil, errors.New("invalid query or k (1..100)")
	}
	keys := make([]string, 0, len(query))
	for key, v := range query {
		if strings.TrimSpace(key) == "" || !finite(v) {
			return nil, errors.New("invalid query feature")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	b.mu.RLock()
	defer b.mu.RUnlock()
	var allowed []Case
	for _, c := range b.cases {
		if c.Scope == scope && c.Synthetic == synthetic && (scope == CaseScopePublic || c.FarmID == farmID) {
			allowed = append(allowed, c)
		}
	}
	if len(allowed) == 0 {
		return []CaseMatch{}, nil
	}
	scales := make(map[string]float64, len(keys))
	for _, key := range keys {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, c := range allowed {
			if v, ok := c.Features[key]; ok {
				lo = math.Min(lo, v)
				hi = math.Max(hi, v)
			}
		}
		if math.IsInf(lo, 1) {
			return nil, errors.New("query feature absent from authorized case scope")
		}
		scales[key] = math.Max(hi-lo, math.Max(math.Abs(lo)*.05, 1e-6))
		if !finite(scales[key]) {
			return nil, errors.New("feature range overflow")
		}
	}
	hits := make([]CaseMatch, 0)
	for _, c := range allowed {
		sum, complete := 0.0, true
		for _, key := range keys {
			v, ok := c.Features[key]
			if !ok {
				complete = false
				break
			}
			d := (query[key] - v) / scales[key]
			sum += d * d
		}
		if !finite(sum) {
			return nil, errors.New("case distance overflow")
		}
		if complete {
			hits = append(hits, CaseMatch{Case: cloneCase(c), Distance: math.Sqrt(sum / float64(len(keys))), Synthetic: c.Synthetic})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Distance == hits[j].Distance {
			return hits[i].Case.ID < hits[j].Case.ID
		}
		return hits[i].Distance < hits[j].Distance
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }


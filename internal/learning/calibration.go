package learning

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
 "strings"
	"sync"
)

const (V1B0 = -6.50; V1W1 = 1.20; V1W2 = 0.85; V1W3 = 0.70)
func V1Coefficients() Coef { return Coef{B0:V1B0,W1:V1W1,W2:V1W2,W3:V1W3} }

const (
	V1VersionName = "V1-GSI"
	MinTotalN     = 30
	MinPositiveN  = 5
	MinNegativeN  = 5
)

type Coef struct {
	B0 float64 `json:"b0"`
	W1 float64 `json:"w1"`
	W2 float64 `json:"w2"`
	W3 float64 `json:"w3"`
}

func (c Coef) array() [4]float64 {
	return [4]float64{c.B0, c.W1, c.W2, c.W3}
}

type ModelMetrics struct {
	AUC     float64 `json:"auc"`
	Brier   float64 `json:"brier"`
	LogLoss float64 `json:"log_loss"`
}

type CompareMetrics struct {
	Candidate ModelMetrics `json:"candidate"`
	V1        ModelMetrics `json:"v1"`
	Holdout   ModelMetrics `json:"holdout"`
	Improved  bool         `json:"improved"`
}

type Outcome struct {
	FarmID string `json:"farm_id"`
	PondID string `json:"pond_id"`
	Day float64 `json:"day"` // Absolute elapsed days on one shared study timeline, never cycle age.
	CycleID   string     `json:"cycle_id"`
	Scores    [3]float64 `json:"scores"` // [S_qPCR, S_Bio, S_Chem]
	Label     string     `json:"label"`  // OUTBREAK, EMERGENCY_HARVEST (1) or HARVEST_SUCCESS (0)
	Synthetic bool       `json:"synthetic"`
}

type CalibrationCandidate struct {
	Version   string         `json:"version"`
	Coef      Coef           `json:"coef"`
	N         int            `json:"n"`
	Metrics   CompareMetrics `json:"metrics"`
	Synthetic bool           `json:"synthetic"`
	Status    string         `json:"status"` // always "kandidat" initially
	Eligible  bool           `json:"eligible"`
}

type ModelRecord struct {
	Candidate  CalibrationCandidate `json:"candidate"`
	ApprovedBy string               `json:"approved_by,omitempty"`
	Active     bool                 `json:"active"`
}

type Store interface {
	Save(ctx context.Context, candidate CalibrationCandidate) error
	Get(ctx context.Context, version string) (ModelRecord, error)
	List(ctx context.Context) ([]ModelRecord, error)
	SetActive(ctx context.Context, version, approvedBy string) error
	Active(ctx context.Context) (string, error)
}

type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]ModelRecord
	active  string
}

func NewMemoryStore() *MemoryStore {
	ms := &MemoryStore{
		records: make(map[string]ModelRecord),
		active:  V1VersionName,
	}
	ms.records[V1VersionName] = ModelRecord{
		Candidate: CalibrationCandidate{
			Version:   V1VersionName,
			Coef:      V1Coefficients(),
			N:         0,
			Synthetic: false,
			Status:    "active",
			Eligible:  true,
		},
		ApprovedBy: "GSI_PRIOR",
		Active:     true,
	}
	return ms
}

func (m *MemoryStore) Save(ctx context.Context, c CalibrationCandidate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Version == "" || c.Version == V1VersionName {return errors.New("frozen or empty version")}
 if _,exists:=m.records[c.Version];exists {return errors.New("immutable model version already exists")}
 c.Status="kandidat"
 m.records[c.Version] = ModelRecord{Candidate: c, Active: false}
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, version string) (ModelRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.records[version]
	if !ok {
		return ModelRecord{}, fmt.Errorf("model version %s not found", version)
	}
	return rec, nil
}

func (m *MemoryStore) List(ctx context.Context) ([]ModelRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]ModelRecord, 0, len(m.records))
	for _, r := range m.records {
		list = append(list, r)
	}
	return list, nil
}

func (m *MemoryStore) SetActive(ctx context.Context, version, approvedBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[version]
	if !ok {
		return fmt.Errorf("version %s not registered", version)
	}
	if rec.Candidate.Synthetic {
		return errors.New("synthetic model candidates cannot be activated in production")
	}
	if strings.TrimSpace(approvedBy) == "" || !qualified(rec.Candidate) {
		return errors.New("explicit approval and qualification required")
	}
 // Deactivate all
	for k, v := range m.records {
		v.Active = false
		m.records[k] = v
	}
	rec.Active = true
	rec.ApprovedBy = approvedBy
	rec.Candidate.Status = "active"
	m.records[version] = rec
	m.active = version
	return nil
}

func (m *MemoryStore) Active(ctx context.Context) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active, nil
}

func qualified(c CalibrationCandidate) bool {
	if c.Synthetic { return false }
	for _, v := range c.Coef.array() {
		if !finite(v) {
			return false
		}
	}
	m, v := c.Metrics.Holdout, c.Metrics.V1
	return c.N >= MinTotalN && c.Eligible && c.Metrics.Improved && finite(m.Brier) && finite(m.LogLoss) && m.Brier < v.Brier && m.LogLoss < v.LogLoss
}

type Registry struct {
	Store Store
}

func (r *Registry) Add(ctx context.Context, candidate CalibrationCandidate) error {
	if candidate.Version == V1VersionName {
		return errors.New("cannot overwrite frozen V1 prior")
	}
	candidate.Status = "kandidat"
	return r.Store.Save(ctx, candidate)
}

func (r *Registry) Promote(ctx context.Context, version, approvedBy string) error {
	if strings.TrimSpace(approvedBy) == "" {
		return errors.New("promotion requires explicit auditor approval")
	}
	rec, err := r.Store.Get(ctx, version)
	if err != nil {
		return err
	}
	if rec.Candidate.Synthetic {
		return errors.New("synthetic model candidates cannot be promoted to production")
	}
	if !qualified(rec.Candidate) {
		return fmt.Errorf("version %s failed qualification gates and cannot be promoted", version)
	}
	return r.Store.SetActive(ctx, version, approvedBy)
}

func Calibrate(version string, outcomes []Outcome) (CalibrationCandidate, error) {
	if strings.TrimSpace(version)=="" || version == V1VersionName {
		return CalibrationCandidate{}, errors.New("V1-GSI is frozen and cannot be recalibrated")
	}
	if len(outcomes) < MinTotalN {
		return CalibrationCandidate{}, fmt.Errorf("insufficient data: %d < %d required", len(outcomes), MinTotalN)
	}

	synth := false
	posCount := 0
	negCount := 0
	type item struct {
		x [3]float64
		y float64
	}

	for _, o := range outcomes {
		if o.CycleID == "" && o.PondID == "" && o.FarmID == "" {
			return CalibrationCandidate{}, errors.New("cycle ID or pond ID required")
		}
		for _, v := range o.Scores {
			if !finite(v) {
				return CalibrationCandidate{}, errors.New("non-finite subscore")
			}
		}
		if o.Label != "OUTBREAK" && o.Label != "EMERGENCY_HARVEST" && o.Label != "HARVEST_SUCCESS" {
			return CalibrationCandidate{}, errors.New("unsupported cycle outcome")
		}
		if o.Synthetic {
			synth = true
		}
		if o.Label == "OUTBREAK" || o.Label == "EMERGENCY_HARVEST" {
			posCount++
		} else {
			negCount++
		}
	}

	if posCount < MinPositiveN || negCount < MinNegativeN {
		return CalibrationCandidate{}, fmt.Errorf("insufficient class representation: pos=%d, neg=%d (min %d each)", posCount, negCount, MinPositiveN)
	}

	// Grouping: pond+cycle or farm grouping to prevent random sample leakage
	farms := make(map[string]bool)
	for _, o := range outcomes {
		if o.FarmID != "" {
			farms[o.FarmID] = true
		}
	}
	useFarmGrouping := len(farms) > 1

	type group struct {
		key    string
		maxDay float64
		items  []item
	}
	groupMap := make(map[string]*group)
	var groupOrder []string

	for idx, o := range outcomes {
		var key string
		if useFarmGrouping {
			key = o.FarmID
		} else if o.PondID != "" && o.CycleID != "" {
			key = o.PondID + ":" + o.CycleID
		} else if o.PondID != "" {
			key = o.PondID
		} else {
			key = o.CycleID
		}

		var y float64
		if o.Label == "OUTBREAK" || o.Label == "EMERGENCY_HARVEST" {
			y = 1.0
		}

		day := o.Day
		if day == 0 {
			day = float64(idx)
		}

		g, exists := groupMap[key]
		if !exists {
			g = &group{key: key, maxDay: day}
			groupMap[key] = g
			groupOrder = append(groupOrder, key)
		} else if day > g.maxDay {
			g.maxDay = day
		}
		g.items = append(g.items, item{x: o.Scores, y: y})
	}

	groups := make([]*group, 0, len(groupMap))
	for _, key := range groupOrder {
		groups = append(groups, groupMap[key])
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].maxDay < groups[j].maxDay
	})

	// Chronological holdout: latest chronological groups held out, no sample leakage
	numGroups := len(groups)
	targetSplit := int(math.Floor(float64(numGroups) * 0.8))
	if targetSplit >= numGroups {
		targetSplit = numGroups - 1
	}

	bestSplit := -1
	for delta := 0; delta < numGroups; delta++ {
		for _, candidateSplit := range []int{targetSplit - delta, targetSplit + delta} {
			if candidateSplit >= 1 && candidateSplit < numGroups {
				trainPos, trainNeg, holdoutPos, holdoutNeg := 0, 0, 0, 0
				for i := 0; i < candidateSplit; i++ {
					for _, it := range groups[i].items {
						if it.y == 1.0 {
							trainPos++
						} else {
							trainNeg++
						}
					}
				}
				for i := candidateSplit; i < numGroups; i++ {
					for _, it := range groups[i].items {
						if it.y == 1.0 {
							holdoutPos++
						} else {
							holdoutNeg++
						}
					}
				}
				if trainPos > 0 && trainNeg > 0 && holdoutPos > 0 && holdoutNeg > 0 {
					bestSplit = candidateSplit
					break
				}
			}
		}
		if bestSplit != -1 {
			break
		}
	}

	if bestSplit == -1 {
		return CalibrationCandidate{}, errors.New("chronological holdout lacks class representation (need both positive and negative outcomes in holdout window)")
	}

	var train, holdout []item
	for i := 0; i < bestSplit; i++ {
		train = append(train, groups[i].items...)
	}
	for i := bestSplit; i < numGroups; i++ {
		holdout = append(holdout, groups[i].items...)
	}

	// Fit logistic regression via Newton-Raphson with L2 prior regularization
	// Loss(w) = -LogLikelihood/N + 0.5 * (lambda/N) * ||w - w_prior||^2
	lambda := 1.0 // prior weight
	weights := [4]float64{V1Coefficients().B0, V1Coefficients().W1, V1Coefficients().W2, V1Coefficients().W3}
	priorArr := weights
	nTrain := float64(len(train))

	for iter := 0; iter < 20; iter++ {
		var grad [4]float64
		var hess [4][4]float64

		for _, it := range train {
			x := [4]float64{1.0, it.x[0], it.x[1], it.x[2]}
			p := sigmoid(weights[0]*x[0] + weights[1]*x[1] + weights[2]*x[2] + weights[3]*x[3])
			diff := p - it.y
			wVal := p * (1.0 - p)

			for j := 0; j < 4; j++ {
				grad[j] += diff * x[j]
				for k := 0; k < 4; k++ {
					hess[j][k] += wVal * x[j] * x[k]
				}
			}
		}

		for j := 0; j < 4; j++ {
			grad[j] = grad[j]/nTrain + (lambda/nTrain)*(weights[j]-priorArr[j])
			for k := 0; k < 4; k++ {
				hess[j][k] /= nTrain
				if j == k {
					hess[j][k] += (lambda / nTrain) + 1e-6 // diagonal damping for numerical stability
				}
			}
		}

		step, ok := solve4(hess, grad)
		if !ok {
			break
		}

		maxStep := 0.0
		for k := 0; k < 4; k++ {
			weights[k] -= math.Max(-1,math.Min(1,step[k]))
			if math.Abs(step[k]) > maxStep {
				maxStep = math.Abs(step[k])
			}
		}
		if maxStep < 1e-5 {
			break
		}
	}

	candCoef := Coef{
		B0: math.Round(weights[0]*1000) / 1000,
		W1: math.Round(weights[1]*1000) / 1000,
		W2: math.Round(weights[2]*1000) / 1000,
		W3: math.Round(weights[3]*1000) / 1000,
	}

	// Evaluate candidate vs V1 on holdout
	var candHoldoutY, v1HoldoutY, actualHoldoutY []float64
	for _, it := range holdout {
		actualHoldoutY = append(actualHoldoutY, it.y)
		candHoldoutY = append(candHoldoutY, probability(candCoef, it.x))
		v1HoldoutY = append(v1HoldoutY, probability(V1Coefficients(), it.x))
	}

	candMetrics := computeMetrics(actualHoldoutY, candHoldoutY)
	v1Metrics := computeMetrics(actualHoldoutY, v1HoldoutY)

	// Gate: Improvement over V1 (lower Brier score or lower LogLoss)
	improved := candMetrics.Brier < v1Metrics.Brier-1e-4 && candMetrics.LogLoss < v1Metrics.LogLoss-1e-4 && candMetrics.AUC>=v1Metrics.AUC
	eligible := improved && len(outcomes) >= MinTotalN && posCount >= MinPositiveN && negCount >= MinNegativeN

	return CalibrationCandidate{
		Version:   version,
		Coef:      candCoef,
		N:         len(outcomes),
		Synthetic: synth,
		Status:    "kandidat",
		Eligible:  eligible,
		Metrics: CompareMetrics{
			Candidate: candMetrics,
			V1:        v1Metrics,
			Holdout:   candMetrics,
			Improved:  improved,
		},
	}, nil
}

func probability(c Coef, scores [3]float64) float64 {
	z := c.B0 + c.W1*scores[0] + c.W2*scores[1] + c.W3*scores[2]
	return sigmoid(z)
}

func sigmoid(z float64) float64 {
	if z > 35 {
		return 1.0
	}
	if z < -35 {
		return 0.0
	}
	return 1.0 / (1.0 + math.Exp(-z))
}

func solve4(A [4][4]float64, b [4]float64) ([4]float64, bool) {
	// Gaussian elimination with partial pivoting for 4x4 system
	M := [4][5]float64{}
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			M[i][j] = A[i][j]
		}
		M[i][4] = b[i]
	}

	for col := 0; col < 4; col++ {
		// find pivot
		pivotRow := col
		maxVal := math.Abs(M[col][col])
		for row := col + 1; row < 4; row++ {
			if val := math.Abs(M[row][col]); val > maxVal {
				maxVal = val
				pivotRow = row
			}
		}
		if maxVal < 1e-12 {
			return [4]float64{}, false
		}
		if pivotRow != col {
			M[col], M[pivotRow] = M[pivotRow], M[col]
		}

		for row := col + 1; row < 4; row++ {
			factor := M[row][col] / M[col][col]
			for c := col; c <= 4; c++ {
				M[row][c] -= factor * M[col][c]
			}
		}
	}

	// Back-substitution
	var x [4]float64
	for i := 3; i >= 0; i-- {
		sum := M[i][4]
		for j := i + 1; j < 4; j++ {
			sum -= M[i][j] * x[j]
		}
		if math.Abs(M[i][i]) < 1e-12 {
			return [4]float64{}, false
		}
		x[i] = sum / M[i][i]
	}
	return x, true
}

func computeMetrics(actual, preds []float64) ModelMetrics {
	n := float64(len(actual))
	if n == 0 {
		return ModelMetrics{}
	}

	var brier, logloss float64
	for i := 0; i < len(actual); i++ {
		y := actual[i]
		p := preds[i]
		diff := p - y
		brier += diff * diff

		pClamp := math.Max(1e-12, math.Min(1.0-1e-12, p))
		logloss += -(y*math.Log(pClamp) + (1.0-y)*math.Log(1.0-pClamp))
	}
	brier /= n
	logloss /= n

	// Fast AUC via trapezoidal rule
	type pair struct {
		y float64
		p float64
	}
	pairs := make([]pair, len(actual))
	for i := range actual {
		pairs[i] = pair{y: actual[i], p: preds[i]}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].p > pairs[j].p
	})

	var nPos, nNeg float64
	for _, p := range pairs {
		if p.y == 1 {
			nPos++
		} else {
			nNeg++
		}
	}

	var auc float64
	if nPos > 0 && nNeg > 0 {
		var rankSum float64
		for i, p := range pairs {
			if p.y == 1 {
				// rank from lowest (0 is lowest rank 1, highest is rank n)
				rankSum += float64(len(pairs) - i)
			}
		}
		auc = (rankSum - (nPos*(nPos+1.0))/2.0) / (nPos * nNeg)
	}

	return ModelMetrics{
		AUC:     math.Round(auc*1000) / 1000,
		Brier:   math.Round(brier*1000) / 1000,
		LogLoss: math.Round(logloss*1000) / 1000,
	}
}

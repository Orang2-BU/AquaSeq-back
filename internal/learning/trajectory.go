package learning

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

type Sample struct {
	Date string // Optional ISO 8601 date; when supplied, all points must use dates.
	LogitZ *float64 // Optional pure deterministic logit, not a laboratory composite percentage.
	Day            float64
	ProbabilityPct float64
	RiskClass      int
	Synthetic      bool
	Features       map[string]float64
}

type Contributor struct {
	Feature   string  `json:"feature"`
	Value     float64 `json:"value"`
	Baseline  float64 `json:"baseline"`
	Deviation float64 `json:"deviation"`
}

type AnomalyReport struct {
	PondID                string        `json:"pond_id"`
	DatasetName           string        `json:"dataset_name"`
	Score                 float64       `json:"score"`
	Flagged               bool          `json:"flagged"`
	Contributors          []Contributor `json:"contributors"`
	EarlierThanClassSteps *int          `json:"earlier_than_class_steps,omitempty"`
	FirstAnomalyStep      *int          `json:"first_anomaly_step,omitempty"`
	ClassFourStep         *int          `json:"class_four_step,omitempty"`
	Synthetic             bool          `json:"synthetic"`
}

type EscalationPolicy struct {
	SDRIClass    int    `json:"sdri_class"`
	Handling     string `json:"handling"`
	AlertMessage string `json:"alert_message"`
	Synthetic    bool   `json:"synthetic"`
}

type Forecast struct {
	Horizon                int      `json:"horizon_days"`
	PredictedProbabilityPct *float64 `json:"predicted_probability_pct"`
	PredictedLogitZ         *float64 `json:"predicted_logit_z"`
	Disclaimer             string   `json:"disclaimer"`
	Lower                  *float64 `json:"lower,omitempty"` // Reserved; no validated prediction interval available.
	Upper                  *float64 `json:"upper,omitempty"`
	Method                 string   `json:"method"`
	Confidence             float64  `json:"-"` // Deprecated: compatibility only, always zero.
	DataPoints             int      `json:"data_points"`
	Status                 string   `json:"status"`
	Synthetic              bool     `json:"synthetic"`
}

type ClusterRisk struct {
	PondID         string  `json:"pond_id"`
	Cluster        string  `json:"cluster"`
	ProbabilityPct float64 `json:"probability_pct"`
	Synthetic      bool    `json:"synthetic"`
}

type ClusterPropagation struct {
	PondID                   string  `json:"pond_id"`
	Cluster                  string  `json:"cluster"`
	OriginalProbabilityPct   float64 `json:"original_probability_pct"`
	ContextualProbabilityPct float64 `json:"contextual_probability_pct"`
	ClusterAverage           float64 `json:"cluster_average"`
	Neighbors                int     `json:"neighbors"`
	Weight                   float64 `json:"weight"`
	Synthetic                bool    `json:"synthetic"`
}

func Analyze(pondID, datasetName string, points []Sample, baselinePoints int) (AnomalyReport, error) {
	if len(points) == 0 {
		return AnomalyReport{}, errors.New("empty trajectory points")
	}
	if baselinePoints < 2 {
		baselinePoints = 2
	}
	if baselinePoints > len(points) {
		baselinePoints = len(points)
	}

	synth := false
	for i, p := range points {
		if math.IsNaN(p.ProbabilityPct) || math.IsInf(p.ProbabilityPct, 0) || p.ProbabilityPct < 0 || p.ProbabilityPct > 100 {
			return AnomalyReport{}, fmt.Errorf("invalid probability at step %d", i)
		}
		if i > 0 && p.Day <= points[i-1].Day {
			return AnomalyReport{}, fmt.Errorf("non-increasing day at step %d", i)
		}
		if p.Synthetic {
			synth = true
		}
	}

	// Compute median & MAD per feature from baseline window
	baseSlice := points[:baselinePoints]
	keys := make([]string, 0)
	for k := range baseSlice[0].Features {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	stats := make(map[string]struct{ med, mad float64 }, len(keys))
	for _, k := range keys {
		vals := make([]float64, 0, len(baseSlice))
		for _, b := range baseSlice {
			if v, ok := b.Features[k]; ok && !math.IsNaN(v) {
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			continue
		}
		m := median(vals)
		diffs := make([]float64, len(vals))
		for i, v := range vals {
			diffs[i] = math.Abs(v - m)
		}
		madVal := median(diffs) * 1.4826
		if madVal < 1e-4 {
			madVal = 1e-4
		}
		stats[k] = struct{ med, mad float64 }{med: m, mad: madVal}
	}

	var firstAnomaly *int
	var classFour *int

	// Scan through points
	for i := 0; i < len(points); i++ {
		p := points[i]
		if p.RiskClass >= 4 || p.ProbabilityPct >= 60.0 {
			if classFour == nil {
				idx := i
				classFour = &idx
			}
		}

		// Calculate anomaly at step i
		score, _ := stepAnomaly(points, i, stats)
		if score >= 2.0 && firstAnomaly == nil && i >= baselinePoints-1 {
			idx := i
			firstAnomaly = &idx
		}
	}

	// Calculate final anomaly report for the latest point
	latestIdx := len(points) - 1
	finalScore, contribs := stepAnomaly(points, latestIdx, stats)
	flagged := finalScore >= 2.0

	var lead *int
	if firstAnomaly != nil && classFour != nil && *classFour >= *firstAnomaly {
		diff := *classFour - *firstAnomaly
		lead = &diff
	}

	return AnomalyReport{
		PondID:                pondID,
		DatasetName:           datasetName,
		Score:                 math.Round(finalScore*100) / 100,
		Flagged:               flagged,
		Contributors:          contribs,
		EarlierThanClassSteps: lead,
		FirstAnomalyStep:      firstAnomaly,
		ClassFourStep:         classFour,
		Synthetic:             synth,
	}, nil
}

func stepAnomaly(points []Sample, idx int, stats map[string]struct{ med, mad float64 }) (float64, []Contributor) {
	curr := points[idx]
	var sumSq float64
	var count float64
	contribs := make([]Contributor, 0, len(curr.Features))

	for k, st := range stats {
		val, ok := curr.Features[k]
		if !ok || math.IsNaN(val) {
			continue
		}
		z := (val - st.med) / st.mad
		sumSq += z * z
		count++
		contribs = append(contribs, Contributor{
			Feature:   k,
			Value:     math.Round(val*100) / 100,
			Baseline:  math.Round(st.med*100) / 100,
			Deviation: math.Round(z*100) / 100,
		})
	}

	sort.Slice(contribs, func(i, j int) bool {
		return math.Abs(contribs[i].Deviation) > math.Abs(contribs[j].Deviation)
	})

	var mahalanobis float64
	if count > 0 {
		mahalanobis = math.Sqrt(sumSq / count)
	}

	// Slope & acceleration of risk probability
	var slope, accel float64
	if idx >= 1 {
		dt := curr.Day - points[idx-1].Day
		if dt > 0 {
			slope = (curr.ProbabilityPct - points[idx-1].ProbabilityPct) / dt
		}
	}
	if idx >= 2 {
		dt1 := curr.Day - points[idx-1].Day
		dt0 := points[idx-1].Day - points[idx-2].Day
		if dt1 > 0 && dt0 > 0 {
			v1 := (curr.ProbabilityPct - points[idx-1].ProbabilityPct) / dt1
			v0 := (points[idx-1].ProbabilityPct - points[idx-2].ProbabilityPct) / dt0
			accel = (v1 - v0) / ((dt1 + dt0) / 2.0)
		}
	}

	score := mahalanobis + math.Max(0, slope*12.0) + math.Max(0, accel*30.0)
	return score, contribs
}

func Escalate(sdriClass int, anomaly AnomalyReport) EscalationPolicy {
	// Rule: never overwrite SDRI class. Return both side-by-side.
	handling := "normal"
	msg := fmt.Sprintf("Kondisi kolam sesuai kelas SDRI %d.", sdriClass)

	if anomaly.Flagged {
		if sdriClass == 2 {
			handling = "waspada tinggi"
			msg = "Eskalasi Terdeteksi: Kelas SDRI 2 dengan deviasi trajektori menanjak. Rekomendasi perlakuan waspada tinggi (cek sirkulasi, pakan -20%, siphon)."
		} else if sdriClass == 1 {
			handling = "waspada ringan"
			msg = "Perhatian Pola: Kelas SDRI 1 namun kurva bioindikator menyimpang dari baseline kolam."
		} else if sdriClass >= 3 {
			handling = "kritis"
			msg = "Wabah Akut: Kelas SDRI tinggi diperparah deviasi multivariat tajam."
		} else {
			handling = "waspada pola"
			msg = "Deteksi Dini: SDRI aman namun deviasi multivariat terdeteksi sebelum ambang terlampaui."
		}
	} else {
		if sdriClass == 2 {
			handling = "waspada normal"
			msg = "Kelas SDRI 2 dengan trajektori stabil."
		}
	}

	return EscalationPolicy{
		SDRIClass:    sdriClass,
		Handling:     handling,
		AlertMessage: msg,
		Synthetic:    anomaly.Synthetic,
	}
}

func ForecastRisk(points []Sample, stepsAhead int) (Forecast, error) {
	if stepsAhead != 1 && stepsAhead != 2 {
		return Forecast{}, errors.New("stepsAhead must be 1 (7 days) or 2 (14 days)")
	}
	synth := false
	for i, p := range points {
		if math.IsNaN(p.ProbabilityPct) || math.IsInf(p.ProbabilityPct, 0) || p.ProbabilityPct < 0 || p.ProbabilityPct > 100 {
			return Forecast{}, fmt.Errorf("invalid probability at index %d", i)
		}
		if p.LogitZ != nil && (math.IsNaN(*p.LogitZ) || math.IsInf(*p.LogitZ, 0)) {
			return Forecast{}, fmt.Errorf("invalid logit_z at index %d", i)
		}
		if p.Synthetic {
			synth = true
		}
	}

	horizon := stepsAhead * 7
	disclaimer := "Proyeksi statistik eksperimental; bukan diagnosis tervalidasi klinis."

	// Robust actual elapsed date/day handling
	elapsed := make([]float64, len(points))
	hasDates := len(points) > 0 && points[0].Date != ""
	if hasDates {
		var t0 time.Time
		for i, p := range points {
			if p.Date == "" {
				return Forecast{}, fmt.Errorf("missing date at index %d", i)
			}
			t, err := parseDate(p.Date)
			if err != nil {
				return Forecast{}, fmt.Errorf("invalid date at index %d: %w", i, err)
			}
			if i == 0 {
				t0 = t
				elapsed[0] = 0
			} else {
				if t.Before(t0) {
					return Forecast{}, fmt.Errorf("non-chronological date at index %d", i)
				}
				elapsed[i] = t.Sub(t0).Hours() / 24.0
				if elapsed[i] < elapsed[i-1] {
					return Forecast{}, fmt.Errorf("non-increasing date at index %d", i)
				}
			}
		}
	} else {
		for i, p := range points {
			if i > 0 && p.Day < points[i-1].Day {
				return Forecast{}, fmt.Errorf("non-increasing day at index %d", i)
			}
			elapsed[i] = p.Day
		}
	}

	// Distinct dates/days check: null for insufficient data (<4 dates)
	uniqueDays := make(map[float64]bool)
	for _, d := range elapsed {
		uniqueDays[d] = true
	}
	if len(uniqueDays) < 4 {
		return Forecast{
			Horizon:    horizon,
			Method:     "THEIL_SEN_LOGIT",
			Confidence: 0.0,
			DataPoints: len(points),
			Status:     "INSUFFICIENT_DATA",
			Synthetic:  synth,
			Disclaimer: disclaimer,
		}, nil
	}

	// Convert points to logit space: pure-logit if provided, else bounded probability mapped to logit
	logits := make([]float64, len(points))
	for i, p := range points {
		if p.LogitZ != nil {
			logits[i] = *p.LogitZ
		} else {
			prob := p.ProbabilityPct / 100.0
			prob = math.Max(1e-4, math.Min(1.0-1e-4, prob))
			logits[i] = math.Log(prob / (1.0 - prob))
		}
	}

	// Theil-Sen estimator on logit Z vs elapsed days
	var slopes []float64
	n := len(points)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dt := elapsed[j] - elapsed[i]
			if dt > 0 {
				slopes = append(slopes, (logits[j]-logits[i])/dt)
			}
		}
	}
	if len(slopes) == 0 {
		return Forecast{
			Horizon:    horizon,
			Method:     "THEIL_SEN_LOGIT",
			Confidence: 0.0,
			DataPoints: len(points),
			Status:     "INSUFFICIENT_DATA",
			Synthetic:  synth,
			Disclaimer: disclaimer,
		}, nil
	}
	mZ := median(slopes)

	var intercepts []float64
	for i := 0; i < n; i++ {
		intercepts = append(intercepts, logits[i]-mZ*elapsed[i])
	}
	bZ := median(intercepts)

	predDay := elapsed[n-1] + float64(stepsAhead*7)
	predLogit := bZ + mZ*predDay
	predP := sigmoid(predLogit) * 100.0

	// Residuals in logit space
	var res []float64
	for i := 0; i < n; i++ {
		fit := bZ + mZ*elapsed[i]
		res = append(res, math.Abs(logits[i]-fit))
	}
	madRes := median(res) * 1.4826
	if madRes < 1e-4 {
		madRes = 1e-4
	}

	// Prediction interval mapped through sigmoid back to percentage
	deltaZ := 1.96 * madRes * math.Sqrt(1.0+1.0/float64(n))
	lower := sigmoid(predLogit-deltaZ) * 100.0
	upper := sigmoid(predLogit+deltaZ) * 100.0

	pVal := math.Round(predP*100) / 100
	lVal := math.Round(lower*100) / 100
	uVal := math.Round(upper*100) / 100
	zVal := math.Round(predLogit*10000) / 10000

	return Forecast{
		Horizon:                 horizon,
		PredictedProbabilityPct: &pVal,
		PredictedLogitZ:         &zVal,
		Lower:                   &lVal,
		Upper:                   &uVal,
		Method:                  "THEIL_SEN_LOGIT",
		Confidence:              0.0, // No arbitrary 1-MAD/35 claimed; uncalibrated.
		DataPoints:              n,
		Status:                  "OK",
		Synthetic:               synth,
		Disclaimer:              disclaimer,
	}, nil
}

func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func Propagate(own ClusterRisk, cluster []ClusterRisk) (ClusterPropagation, error) {
	var neighbors []ClusterRisk
	synth := own.Synthetic
	for _, c := range cluster {
		if c.Cluster == own.Cluster && c.PondID != own.PondID {
			neighbors = append(neighbors, c)
			if c.Synthetic {
				synth = true
			}
		}
	}
	if len(neighbors) == 0 {
		return ClusterPropagation{
			PondID:                   own.PondID,
			Cluster:                  own.Cluster,
			OriginalProbabilityPct:   own.ProbabilityPct,
			ContextualProbabilityPct: own.ProbabilityPct,
			ClusterAverage:           own.ProbabilityPct,
			Neighbors:                0,
			Weight:                   0,
			Synthetic:                synth,
		}, nil
	}

	var sum float64
	for _, nb := range neighbors {
		sum += nb.ProbabilityPct
	}
	avg := sum / float64(len(neighbors))
	weight := 0.25 // documented cluster propagation weight
	contextual := (1.0-weight)*own.ProbabilityPct + weight*avg

	return ClusterPropagation{
		PondID:                   own.PondID,
		Cluster:                  own.Cluster,
		OriginalProbabilityPct:   math.Round(own.ProbabilityPct*100) / 100,
		ContextualProbabilityPct: math.Round(contextual*100) / 100,
		ClusterAverage:           math.Round(avg*100) / 100,
		Neighbors:                len(neighbors),
		Weight:                   weight,
		Synthetic:                synth,
	}, nil
}

func median(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	cp := append([]float64(nil), s...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2.0
}

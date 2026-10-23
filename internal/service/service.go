package service

import (
	"context"
	"fmt"
	"time"

	"aquaseq-back/internal/cache"
	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/report"
	"aquaseq-back/internal/repository"
	"aquaseq-back/internal/sdri"
	"aquaseq-back/internal/trajectory"
)

type AppService struct {
	repo  repository.Repository
	cache *cache.Cache
}

func NewAppService(repo repository.Repository, c *cache.Cache) *AppService {
	if c == nil {
		c = cache.New()
	}
	return &AppService{repo: repo, cache: c}
}

// CalculateSDRI runs pure deterministic computation.
func (s *AppService) CalculateSDRI(input domain.SDRIInput) domain.SDRIResult {
	return sdri.Calculate(input)
}

// IngestSample computes subscores from 12 features via exact lab stoichiometry, checks PPC, and persists.
// ponytail: deterministic sub-scores calculated directly from lab stoichiometry; calibrate weights per cycle outcome.
func (s *AppService) IngestSample(ctx context.Context, sample domain.Sample) (domain.SDRIResult, error) {
	if sample.ID == "" {
		sample.ID = fmt.Sprintf("smp-%d", time.Now().UnixNano())
	}

	// 1. Calculate un-ionized toxic free ammonia NH3-N via Emerson et al.
	temp := 30.0
	if sample.Features.TempC != nil && *sample.Features.TempC > 0 {
		temp = *sample.Features.TempC
	}
	ph := 7.8
	if sample.Features.PH != nil && *sample.Features.PH > 0 {
		ph = *sample.Features.PH
	}
	tan := 0.0
	if sample.Features.TANMgL != nil {
		tan = *sample.Features.TANMgL
	}
	_, _, freeNH3 := sdri.CalculateFreeAmmonia(tan, temp, ph)

	// 2. Calculate copy numbers per mL for pathogens & bioindicators
	var copyAHPND, copyWSSV, copyEHP, copyAmoA float64
	if sample.Features.CTAHPND != nil {
		copyAHPND = sdri.CTToCopiesPerML(*sample.Features.CTAHPND)
	}
	if sample.Features.CTWSSV != nil {
		copyWSSV = sdri.CTToCopiesPerML(*sample.Features.CTWSSV)
	}
	if sample.Features.CTEHP != nil {
		copyEHP = sdri.CTToCopiesPerML(*sample.Features.CTEHP)
	}
	totalPathogenCopy := copyAHPND + copyWSSV + copyEHP

	if sample.Features.CTamoA != nil {
		copyAmoA = sdri.CTToCopiesPerML(*sample.Features.CTamoA)
		if copyAmoA <= 0 && *sample.Features.CTamoA > 0 && *sample.Features.CTamoA < 40 {
			copyAmoA = 100.0 // Baseline
		}
	} else {
		copyAmoA = 100.0
	}

	// 3. Derive S_qPCR, S_Bio, S_Chem using lab formula
	no2 := 0.0
	if sample.Features.NO2MgL != nil {
		no2 = *sample.Features.NO2MgL
	}
	h2s := 0.0
	if sample.Features.H2SMgL != nil {
		h2s = *sample.Features.H2SMgL
	}
	tom := 0.0
	if sample.Features.TOMMgL != nil {
		tom = *sample.Features.TOMMgL
	}

	sQPCR, sBio, sChem := sdri.CalculateSubScores(totalPathogenCopy, copyAmoA, freeNH3, no2, h2s, tom)

	input := domain.SDRIInput{
		CTPPC: sample.Features.CTPPC,
		SQPCR: sQPCR,
		SBio:  sBio,
		SChem: sChem,
	}

	result := sdri.Calculate(input)

	if err := s.repo.SaveSampleWithResult(ctx, sample, result); err != nil {
		return domain.SDRIResult{}, fmt.Errorf("failed to save sample: %w", err)
	}

	// Invalidate trajectory cache for this pond on new sample ingestion
	s.cache.Delete("traj:" + sample.PondID)

	return result, nil
}

// GetPondTrajectory returns the time-series trajectory and early warnings (cached via stdlib in-memory TTL).
func (s *AppService) GetPondTrajectory(ctx context.Context, pondID string) (domain.TrajectoryReport, error) {
	cacheKey := "traj:" + pondID
	if val, ok := s.cache.Get(cacheKey); ok {
		if report, valid := val.(domain.TrajectoryReport); valid {
			return report, nil
		}
	}

	history, err := s.repo.GetTrajectoryHistory(ctx, pondID)
	if err != nil {
		return domain.TrajectoryReport{}, err
	}
	report := trajectory.EvaluateTrajectory(pondID, history)
	s.cache.Set(cacheKey, report, 30*time.Second)
	return report, nil
}

// FormatWhatsAppReport creates a farmer-friendly message text.
func (s *AppService) FormatWhatsAppReport(p report.WhatsAppPayload) string {
	return report.GenerateWhatsAppMessage(p)
}

// RecordCycleOutcome stores harvest or outbreak ground-truth labels.
func (s *AppService) RecordCycleOutcome(ctx context.Context, outcome domain.CycleOutcome) error {
	if outcome.ID == "" {
		outcome.ID = fmt.Sprintf("out-%d", time.Now().UnixNano())
	}
	return s.repo.SaveCycleOutcome(ctx, outcome)
}

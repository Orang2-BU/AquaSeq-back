package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"aquaseq-back/internal/domain"
	"aquaseq-back/internal/report"
	"aquaseq-back/internal/repository"
	"aquaseq-back/internal/sdri"
	"aquaseq-back/internal/trajectory"
)

type AppService struct {
	repo repository.Repository
}

func NewAppService(repo repository.Repository) *AppService {
	return &AppService{repo: repo}
}

// CalculateSDRI runs pure deterministic computation.
func (s *AppService) CalculateSDRI(input domain.SDRIInput) domain.SDRIResult {
	return sdri.Calculate(input)
}

// IngestSample computes subscores, checks PPC, calculates SDRI, and persists the record.
// ponytail: deterministic sub-score heuristic from 12 features; calibrate against GSI lab curves in Q1 pilot.
func (s *AppService) IngestSample(ctx context.Context, sample domain.Sample) (domain.SDRIResult, error) {
	if sample.ID == "" {
		sample.ID = fmt.Sprintf("smp-%d", time.Now().UnixNano())
	}

	// Derive S_qPCR, S_Bio, S_Chem from raw features
	sQPCR := deriveQPCRScore(sample.Features)
	sBio := deriveBioScore(sample.Features)
	sChem := deriveChemScore(sample.Features)

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

	return result, nil
}

// GetPondTrajectory returns the time-series trajectory and early warnings.
func (s *AppService) GetPondTrajectory(ctx context.Context, pondID string) (domain.TrajectoryReport, error) {
	history, err := s.repo.GetTrajectoryHistory(ctx, pondID)
	if err != nil {
		return domain.TrajectoryReport{}, err
	}
	return trajectory.EvaluateTrajectory(pondID, history), nil
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

func deriveQPCRScore(f domain.SampleFeatures) float64 {
	// Lower Ct = higher pathogen concentration. Baseline clean is around -0.5 to -0.8.
	score := -0.5
	if f.CTAHPND != nil && *f.CTAHPND > 0 && *f.CTAHPND < 35.0 {
		score += (35.0 - *f.CTAHPND) * 0.8
	}
	if f.CTWSSV != nil && *f.CTWSSV > 0 && *f.CTWSSV < 35.0 {
		score += (35.0 - *f.CTWSSV) * 1.2
	}
	if f.CTEHP != nil && *f.CTEHP > 0 && *f.CTEHP < 35.0 {
		score += (35.0 - *f.CTEHP) * 0.5
	}
	return math.Round(score*1000) / 1000
}

func deriveBioScore(f domain.SampleFeatures) float64 {
	// amoA indicator: high Ct (>30) means nitrifiers depleted -> higher risk.
	score := -0.6
	if f.CTamoA != nil && *f.CTamoA > 28.0 {
		score += (*f.CTamoA - 28.0) * 0.4
	}
	return math.Round(score*1000) / 1000
}

func deriveChemScore(f domain.SampleFeatures) float64 {
	// TOM > 60 mg/L or DO < 4.0 mg/L increases risk.
	score := -0.3
	if f.TOMMgL != nil && *f.TOMMgL > 50.0 {
		score += (*f.TOMMgL - 50.0) * 0.05
	}
	if f.DOMgL != nil && *f.DOMgL < 4.5 {
		score += (4.5 - *f.DOMgL) * 0.8
	}
	return math.Round(score*1000) / 1000
}

# PRD & Technical Plan: AquaSeq Backend Engine (Go + PostgreSQL)

## 1. Product Overview
AquaSeq decision support engine for shrimp pond health via water eDNA qPCR and chemistry.
Core focus:
- Layer A: Deterministic core (SDRI logit + PPC gate + 5 golden archetypes).
- Layer B: Trajectory anomaly tracking (slope, rate of change, baseline deviation).
- Layer C: Data ingestion validation + localized WhatsApp report output.
- Flywheel: End-of-cycle ground truth collection (harvest/outbreak) for weight recalibration.

## 2. Mathematical Specification
### SDRI Logit Model (V1-GSI Prior)
Z = -6.50 + (1.20 * S_qPCR) + (0.85 * S_Bio) + (0.70 * S_Chem)
Probability P = 1 / (1 + exp(-Z))
Risk Percentage = P * 100%

### Sub-Score Derivations:
- S_qPCR: Pathogen load derived from Ct values (AHPND pirA, WSSV vp28, EHP swp1).
- S_Bio: Microbial balance derived from amoA (ammonia oxidizing) & 16S total bacteria.
- S_Chem: Physicochemical water risk (DO, TOM, pH, Salinity, Temp, TAN).

### PPC Gate (Positive Process Control):
- Target: 25.0 <= Ct_PPC <= 31.5.
- If Ct_PPC > 31.5 or not detected: Abort scoring -> REJECTED_INHIBITOR.
- Never force invalid qPCR data into risk probability.

### Risk Classes:
- Class 1 (P < 5.0%): Normal / Aman
- Class 2 (5.0% <= P < 25.0%): Waspada Ringan
- Class 3 (25.0% <= P < 60.0%): Waspada Tinggi
- Class 4 (P >= 60.0%): Bahaya / Outbreak Imminent

### 5 Golden Archetypes Verification (GSI Lab Benchmark):
1. Pristine Pond: P ≈ 0.04% (Class 1)
2. High Microbial Stability: P ≈ 0.02% (Class 1)
3. Acute Outbreak: P ≈ 99.998% (Class 4)
4. Borderline Dysbiosis: P ≈ 34.8% (Class 3)
5. Inhibited Sample: REJECTED_INHIBITOR (Gated by PPC)

## 3. Concurrency & Scaling (1000 Concurrent Users Target)
- Go `net/http` native goroutine-per-request model.
- Stateless SDRI calculation logic: pure functions, 0 mutex contention.
- PostgreSQL pool parameters:
  - MaxOpenConns: 80
  - MaxIdleConns: 30
  - ConnMaxLifetime: 30m
  - ConnMaxIdleTime: 5m
- Composite database indexes on (pond_id, sampling_date DESC) for sub-millisecond trajectory lookups.

## 4. API Endpoints
- POST /api/v1/sdri/calculate - Direct deterministic calculation & PPC gating
- POST /api/v1/samples - Ingest water sample with 12 features
- GET  /api/v1/ponds/{id}/trajectory - Time-series trend and anomaly alerts
- POST /api/v1/reports/whatsapp - Formatted farmer advice in Bahasa Indonesia
- POST /api/v1/cycles/outcome - Register cycle outcome for calibration flywheel
- GET  /api/v1/health - Readiness & liveness probe

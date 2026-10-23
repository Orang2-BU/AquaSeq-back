-- AquaSeq PostgreSQL Production Schema
-- Designed for 1000 concurrent writes/reads, indexed by pond and date.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. Farms (Tambak)
CREATE TABLE IF NOT EXISTS farms (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(150) NOT NULL,
    location VARCHAR(255) NOT NULL,
    owner_name VARCHAR(120) NOT NULL,
    owner_phone VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Ponds (Petak Kolam)
CREATE TABLE IF NOT EXISTS ponds (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    pond_code VARCHAR(50) NOT NULL,
    area_sqm NUMERIC(10, 2) NOT NULL DEFAULT 1000.00,
    current_cycle INT NOT NULL DEFAULT 1,
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_farm_pond UNIQUE (farm_id, pond_code)
);
CREATE INDEX IF NOT EXISTS idx_ponds_farm_id ON ponds(farm_id);

-- 3. Samples (Titik Sampling eDNA Air)
CREATE TABLE IF NOT EXISTS samples (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    sampling_date DATE NOT NULL,
    cycle_day INT NOT NULL,
    source_type VARCHAR(50) NOT NULL DEFAULT 'GSI_LAB', -- GSI_LAB, FIELD_MANUAL, IOT
    batch_run_id VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_samples_pond_date ON samples(pond_id, sampling_date DESC);

-- 4. Sample Features (12 Core Biomarkers & Quality Controls)
CREATE TABLE IF NOT EXISTS sample_features (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sample_id UUID NOT NULL UNIQUE REFERENCES samples(id) ON DELETE CASCADE,
    -- qPCR Pathogen Ct (AHPND pirA, WSSV vp28, EHP swp1)
    ct_ahpnd NUMERIC(6, 2),
    ct_wssv NUMERIC(6, 2),
    ct_ehp NUMERIC(6, 2),
    -- Bioindicators
    ct_amoa NUMERIC(6, 2),
    ct_16s NUMERIC(6, 2),
    -- Process Quality Control
    ct_ppc NUMERIC(6, 2) NOT NULL,
    ppc_valid BOOLEAN NOT NULL DEFAULT TRUE,
    -- Water Chemistry
    do_mg_l NUMERIC(5, 2),
    tom_mg_l NUMERIC(6, 2),
    ph NUMERIC(4, 2),
    salinity_ppt NUMERIC(5, 2),
    temp_c NUMERIC(4, 2),
    tan_mg_l NUMERIC(5, 2),
    -- Metadata / raw documents
    meta JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sample_features_sample_id ON sample_features(sample_id);

-- 5. SDRI Assessment Results (Deterministic Engine Output)
CREATE TABLE IF NOT EXISTS sdri_results (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sample_id UUID NOT NULL UNIQUE REFERENCES samples(id) ON DELETE CASCADE,
    status VARCHAR(30) NOT NULL, -- VALID, REJECTED_INHIBITOR, REJECTED_INVALID_QC
    z_score NUMERIC(8, 4),
    probability_pct NUMERIC(7, 4),
    risk_class INT, -- 1: Aman, 2: Waspada Ringan, 3: Waspada Tinggi, 4: Bahaya
    s_qpcr NUMERIC(8, 4),
    s_bio NUMERIC(8, 4),
    s_chem NUMERIC(8, 4),
    model_version VARCHAR(20) NOT NULL DEFAULT 'V1-GSI',
    action_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sdri_results_sample_id ON sdri_results(sample_id);

-- 6. Cycle Outcomes (Flywheel Ground Truth Labels for Recalibration)
CREATE TABLE IF NOT EXISTS cycle_outcomes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    cycle_number INT NOT NULL,
    outcome VARCHAR(50) NOT NULL, -- HARVEST_SUCCESS, OUTBREAK, EMERGENCY_HARVEST
    actual_harvest_kg NUMERIC(10, 2),
    outbreak_date DATE,
    primary_suspect_pathogen VARCHAR(50),
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_pond_cycle UNIQUE (pond_id, cycle_number)
);
CREATE INDEX IF NOT EXISTS idx_cycle_outcomes_pond ON cycle_outcomes(pond_id);

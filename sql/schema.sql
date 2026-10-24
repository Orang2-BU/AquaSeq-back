-- AquaSeq PostgreSQL Production Schema
-- Designed for concurrent writes/reads, indexed by farm, pond, user, and date.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. Users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name VARCHAR(150) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    phone_wa VARCHAR(50),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- 2. Refresh Tokens (hashed sha256)
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS family_id UUID;
UPDATE refresh_tokens SET family_id = id WHERE family_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens(family_id);

-- 3. Password Resets (hashed sha256)
CREATE TABLE IF NOT EXISTS password_resets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_password_resets_hash ON password_resets(token_hash);

-- Migrasi aditif mempertahankan data skema lama.
ALTER TABLE IF EXISTS farms ADD COLUMN IF NOT EXISTS regency VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS farms ADD COLUMN IF NOT EXISTS address TEXT;
ALTER TABLE IF EXISTS farms ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id);
ALTER TABLE IF EXISTS farms ADD COLUMN IF NOT EXISTS provenance VARCHAR(20) NOT NULL DEFAULT 'nyata';
ALTER TABLE IF EXISTS farms ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS farms ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
DO $$ BEGIN
IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='farms' AND column_name='location') THEN
 ALTER TABLE farms ALTER COLUMN location SET DEFAULT '';
 ALTER TABLE farms ALTER COLUMN owner_name SET DEFAULT '';
 ALTER TABLE farms ALTER COLUMN owner_phone SET DEFAULT '';
END IF;
END $$;
ALTER TABLE IF EXISTS ponds ADD COLUMN IF NOT EXISTS name VARCHAR(150);
ALTER TABLE IF EXISTS ponds ADD COLUMN IF NOT EXISTS system VARCHAR(30) NOT NULL DEFAULT 'intensif';
ALTER TABLE IF EXISTS ponds ADD COLUMN IF NOT EXISTS stocked_at DATE;
ALTER TABLE IF EXISTS ponds ADD COLUMN IF NOT EXISTS assignee_id UUID REFERENCES users(id);
ALTER TABLE IF EXISTS ponds ADD COLUMN IF NOT EXISTS wa_target VARCHAR(50);
ALTER TABLE IF EXISTS ponds ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS ponds ALTER COLUMN area_sqm DROP NOT NULL;
ALTER TABLE IF EXISTS samples ADD COLUMN IF NOT EXISTS sample_code VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS samples ADD COLUMN IF NOT EXISTS meta JSONB;
ALTER TABLE IF EXISTS sample_features ADD COLUMN IF NOT EXISTS no2_mg_l NUMERIC(5,2);
ALTER TABLE IF EXISTS sample_features ADD COLUMN IF NOT EXISTS h2s_mg_l NUMERIC(6,4);
ALTER TABLE IF EXISTS sample_features ADD COLUMN IF NOT EXISTS alkalinity NUMERIC(6,2);
ALTER TABLE IF EXISTS sdri_results ADD COLUMN IF NOT EXISTS risk_level VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS sdri_results ADD COLUMN IF NOT EXISTS recommendation TEXT;
ALTER TABLE IF EXISTS sdri_results ADD COLUMN IF NOT EXISTS predicted_disease VARCHAR(100);
ALTER TABLE IF EXISTS sdri_results ADD COLUMN IF NOT EXISTS disease_name VARCHAR(255);
ALTER TABLE IF EXISTS sdri_results ADD COLUMN IF NOT EXISTS evidence JSONB;
ALTER TABLE IF EXISTS sdri_results ADD COLUMN IF NOT EXISTS mitigation_actions JSONB;

-- 4. Farms (Tambak)
CREATE TABLE IF NOT EXISTS farms (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(150) NOT NULL,
    regency VARCHAR(100) NOT NULL,
    address TEXT,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    provenance VARCHAR(20) NOT NULL DEFAULT 'nyata', -- 'simulasi' | 'nyata'
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_farms_owner ON farms(owner_id);

-- 5. Memberships
CREATE TABLE IF NOT EXISTS memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL, -- 'pemilik' | 'teknisi'
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_farm_user UNIQUE (farm_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_memberships_farm ON memberships(farm_id);
CREATE INDEX IF NOT EXISTS idx_memberships_user ON memberships(user_id);

-- 6. Ponds (Petak Kolam)
CREATE TABLE IF NOT EXISTS ponds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    pond_code VARCHAR(50) NOT NULL,
    name VARCHAR(150),
    system VARCHAR(30) NOT NULL DEFAULT 'intensif', -- 'intensif' | 'bioflok'
    area_sqm NUMERIC(10, 2),
    current_cycle INT NOT NULL DEFAULT 1,
    stocked_at DATE,
    assignee_id UUID REFERENCES users(id) ON DELETE SET NULL,
    wa_target VARCHAR(50),
    status VARCHAR(30) NOT NULL DEFAULT 'ACTIVE',
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_farm_pond UNIQUE (farm_id, pond_code)
);
CREATE INDEX IF NOT EXISTS idx_ponds_farm_id ON ponds(farm_id);
CREATE INDEX IF NOT EXISTS idx_ponds_assignee ON ponds(assignee_id);

-- 7. Batches (Unggahan Hasil eDNA Lab)
CREATE TABLE IF NOT EXISTS batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    lab_name VARCHAR(150) NOT NULL,
    instrument VARCHAR(100) NOT NULL,
    tested_at TIMESTAMPTZ NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    sample_count INT NOT NULL DEFAULT 0,
    rejected_count INT NOT NULL DEFAULT 0,
    meta JSONB,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_batches_farm ON batches(farm_id);

-- 8. Samples (Titik Sampling eDNA Air)
CREATE TABLE IF NOT EXISTS samples (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    batch_id UUID REFERENCES batches(id) ON DELETE SET NULL,
    sample_code VARCHAR(100) NOT NULL,
    sampling_date DATE NOT NULL,
    cycle_day INT NOT NULL,
    source_type VARCHAR(50) NOT NULL DEFAULT 'GSI_LAB',
    meta JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_samples_pond_date ON samples(pond_id, sampling_date DESC);
ALTER TABLE samples ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES batches(id);
CREATE INDEX IF NOT EXISTS idx_samples_batch ON samples(batch_id);

-- 9. Sample Features (12 Core Biomarkers & Quality Controls)
CREATE TABLE IF NOT EXISTS sample_features (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sample_id UUID NOT NULL UNIQUE REFERENCES samples(id) ON DELETE CASCADE,
    ct_ahpnd NUMERIC(6, 2),
    ct_wssv NUMERIC(6, 2),
    ct_ehp NUMERIC(6, 2),
    ct_amoa NUMERIC(6, 2),
    ct_16s NUMERIC(6, 2),
    ct_ppc NUMERIC(6, 2) NOT NULL,
    ppc_valid BOOLEAN NOT NULL DEFAULT TRUE,
    do_mg_l NUMERIC(5, 2),
    tom_mg_l NUMERIC(6, 2),
    ph NUMERIC(4, 2),
    salinity_ppt NUMERIC(5, 2),
    temp_c NUMERIC(4, 2),
    tan_mg_l NUMERIC(5, 2),
    no2_mg_l NUMERIC(5, 2),
    h2s_mg_l NUMERIC(6, 4),
    alkalinity NUMERIC(6, 2),
    meta JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sample_features_sample_id ON sample_features(sample_id);

-- 10. SDRI Assessment Results (Deterministic Engine Output)
CREATE TABLE IF NOT EXISTS sdri_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sample_id UUID NOT NULL UNIQUE REFERENCES samples(id) ON DELETE CASCADE,
    status VARCHAR(30) NOT NULL, -- VALID, REJECTED_INHIBITOR, REJECTED_INVALID_QC
    z_score NUMERIC(8, 4),
    probability_pct NUMERIC(7, 4),
    risk_class INT NOT NULL, -- 0: Ditolak, 1: Aman, 2: Waspada Ringan, 3: Waspada Tinggi, 4: Bahaya
    risk_level VARCHAR(50) NOT NULL,
    s_qpcr NUMERIC(8, 4),
    s_bio NUMERIC(8, 4),
    s_chem NUMERIC(8, 4),
    model_version VARCHAR(20) NOT NULL DEFAULT 'V1-GSI',
    recommendation TEXT,
    predicted_disease VARCHAR(100),
    disease_name VARCHAR(255),
    evidence JSONB,
    mitigation_actions JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sdri_results_sample_id ON sdri_results(sample_id);

-- 11. Water Logs (Pencatatan Kimia Air)
CREATE TABLE IF NOT EXISTS water_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    measured_at TIMESTAMPTZ NOT NULL,
    temp_c NUMERIC(4, 2) NOT NULL,
    ph NUMERIC(4, 2) NOT NULL,
    do_mg_l NUMERIC(5, 2) NOT NULL,
    tan_mg_l NUMERIC(5, 2) NOT NULL,
    no2_mg_l NUMERIC(5, 2),
    h2s_mg_l NUMERIC(6, 4),
    tom_mg_l NUMERIC(6, 2),
    alkalinity NUMERIC(6, 2),
    salinity_ppt NUMERIC(5, 2),
    transparency_cm NUMERIC(6, 2),
    nh3_mg_l NUMERIC(6, 4) NOT NULL,
    recorded_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_water_logs_pond ON water_logs(pond_id, measured_at DESC);

-- 12. Alerts (Peringatan Risiko)
CREATE TABLE IF NOT EXISTS alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    kind VARCHAR(30) NOT NULL, -- 'RISK_CLASS' | 'REJECTED' | 'TRAJECTORY'
    risk_class INT NOT NULL,
    probability_pct NUMERIC(7, 4) NOT NULL,
    message TEXT NOT NULL,
    sample_id UUID REFERENCES samples(id) ON DELETE SET NULL,
    open_task_id UUID,
    acked_at TIMESTAMPTZ,
    acked_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_alerts_farm ON alerts(farm_id, created_at DESC);

-- 13. Tasks (Tindakan / Tugas Teknisi)
CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    assignee_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    kind VARCHAR(30) NOT NULL, -- 'tindakan' | 'ambil_sampel' | 'catat_air'
    title VARCHAR(255) NOT NULL,
    instruction TEXT NOT NULL,
    due_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'baru', -- 'baru' | 'dikerjakan' | 'dilaporkan' | 'selesai' | 'diminta_ulang' | 'batal'
    owner_note TEXT,
    source_sample_id UUID REFERENCES samples(id) ON DELETE SET NULL,
    source_alert_id UUID REFERENCES alerts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tasks_farm ON tasks(farm_id);
CREATE INDEX IF NOT EXISTS idx_tasks_assignee ON tasks(assignee_id);
CREATE INDEX IF NOT EXISTS idx_tasks_pond ON tasks(pond_id);

-- 14. Task Reports
CREATE TABLE IF NOT EXISTS task_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    note TEXT NOT NULL,
    measured JSONB,
    photos JSONB,
    reported_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_task_reports_task ON task_reports(task_id);

-- 15. Task History
CREATE TABLE IF NOT EXISTS task_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    status VARCHAR(30) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_task_history_task ON task_history(task_id);

-- 16. Invitations (hashed token sha256)
CREATE TABLE IF NOT EXISTS invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    pond_ids JSONB NOT NULL DEFAULT '[]',
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_invitations_hash ON invitations(token_hash);
CREATE INDEX IF NOT EXISTS idx_invitations_farm ON invitations(farm_id);

-- 17. Notifications
CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(50) NOT NULL, -- 'tugas_baru' | 'laporan_masuk' | 'peringatan_baru' | 'tugas_selesai' | 'tugas_terlambat'
    title VARCHAR(255) NOT NULL,
    entity VARCHAR(50) NOT NULL, -- 'task' | 'pond' | 'sample' | 'batch'
    entity_id UUID NOT NULL,
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, created_at DESC);

-- 18. Activity Log
CREATE TABLE IF NOT EXISTS activity_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    action VARCHAR(100) NOT NULL,
    entity VARCHAR(50) NOT NULL,
    entity_id UUID NOT NULL,
    detail TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_activity_farm ON activity_log(farm_id, created_at DESC);

-- 19. Cycle Outcomes (Kalibrasi V2 Ground Truth)
CREATE TABLE IF NOT EXISTS cycle_outcomes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    cycle_number INT NOT NULL,
    outcome VARCHAR(50) NOT NULL, -- 'HARVEST_SUCCESS' | 'OUTBREAK' | 'EMERGENCY_HARVEST'
    actual_harvest_kg NUMERIC(10, 2),
    outbreak_date DATE,
    primary_suspect_pathogen VARCHAR(50),
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_pond_cycle UNIQUE (pond_id, cycle_number)
);
CREATE INDEX IF NOT EXISTS idx_cycle_outcomes_pond ON cycle_outcomes(pond_id);

-- 20. Files (Penyimpanan Foto Laporan)
CREATE TABLE IF NOT EXISTS files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    farm_id UUID NOT NULL REFERENCES farms(id) ON DELETE CASCADE,
    uploader_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    file_name VARCHAR(255) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    file_size INT NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_files_farm ON files(farm_id);
-- Old photos without a known pond remain inaccessible; never infer authorization from farm alone.
ALTER TABLE files ADD COLUMN IF NOT EXISTS pond_id UUID REFERENCES ponds(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_files_pond ON files(pond_id);

-- 21. Placeholder Lapis B (Model Versions, Baselines, Anomaly, Forecast, AI Explanations)
CREATE TABLE IF NOT EXISTS model_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version VARCHAR(50) NOT NULL UNIQUE,
    weights JSONB NOT NULL,
    trained_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pond_baselines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pond_id UUID NOT NULL UNIQUE REFERENCES ponds(id) ON DELETE CASCADE,
    baseline_features JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS anomaly_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    details JSONB,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS forecast_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    forecast_7d JSONB,
    forecast_14d JSONB,
    run_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_explanations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sample_id UUID NOT NULL REFERENCES samples(id) ON DELETE CASCADE,
    explanation TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 22. Mobile Visual Observations
CREATE TABLE IF NOT EXISTS mobile_readings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pond_id UUID NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id VARCHAR(128) NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    values JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_mobile_readings_user_client UNIQUE (user_id, client_id)
);
CREATE INDEX IF NOT EXISTS idx_mobile_readings_pond_at ON mobile_readings(pond_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_mobile_readings_user ON mobile_readings(user_id);

-- 23. Mobile Recommendation Schedules
CREATE TABLE IF NOT EXISTS mobile_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_key VARCHAR(255) NOT NULL UNIQUE,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sample_id UUID NOT NULL REFERENCES samples(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scheduled_date DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_mobile_schedules_key ON mobile_schedules(schedule_key);

-- 24. Kelimpahan taksa per sampel (klasifikasi 16S dari lab mitra) dan metrik komunitas (internal/microbiome)
CREATE TABLE IF NOT EXISTS sample_microbiome (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sample_id UUID NOT NULL UNIQUE REFERENCES samples(id) ON DELETE CASCADE,
    source VARCHAR(255) NOT NULL,
    taxa JSONB NOT NULL,
    metrics JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sample_microbiome_sample ON sample_microbiome(sample_id);

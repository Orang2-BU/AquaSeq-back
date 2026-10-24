# AquaSeq REST API Documentation

Base URL: `http://localhost:8080/api/v1`

Standard Response Envelope:
```json
{
  "success": true,
  "data": {},
  "error": null
}
```

Error Response:
```json
{
  "success": false,
  "data": null,
  "error": {
    "code": "BAD_REQUEST",
    "message": "Ct_PPC is required"
  }
}
```

---

## 1. Calculate SDRI Score
- **URL**: `/api/v1/sdri/calculate`
- **Method**: `POST`
- **Description**: Deterministic scoring engine for qPCR + Chemistry. Validates PPC gate first.

### Request Body
```json
{
  "ct_ppc": 28.5,
  "s_qpcr": -0.5,
  "s_bio": -0.6,
  "s_chem": -0.3
}
```

### Success Response (200 OK)
```json
{
  "success": true,
  "data": {
    "status": "VALID",
    "z_score": -7.82,
    "probability_pct": 0.0401,
    "risk_class": 1,
    "risk_level": "AMAN",
    "model_version": "V1-GSI",
    "recommendation": "Kondisi optimal. Lanjutkan sampling rutin."
  }
}
```

### Inhibitor / Rejection Response (200 OK with rejection status)
```json
{
  "success": true,
  "data": {
    "status": "REJECTED_INHIBITOR",
    "z_score": 0,
    "probability_pct": 0,
    "risk_class": 0,
    "risk_level": "INVALID",
    "model_version": "V1-GSI",
    "recommendation": "Uji PCR terhambat (PPC abnormal). Sampel wajib diulang/ekstraksi ulang."
  }
}
```

---

## 2. Ingest Water Sample
- **URL**: `/api/v1/samples`
- **Method**: `POST`
- **Description**: Ingests water sample with full 12 features, calculates SDRI, and stores in database.

### Request Body
```json
{
  "pond_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "sampling_date": "2026-10-24",
  "cycle_day": 45,
  "source_type": "GSI_LAB",
  "features": {
    "ct_ahpnd": 38.0,
    "ct_wssv": 40.0,
    "ct_ehp": 37.5,
    "ct_amoa": 26.0,
    "ct_16s": 18.0,
    "ct_ppc": 28.2,
    "do_mg_l": 5.8,
    "tom_mg_l": 45.0,
    "ph": 7.9,
    "salinity_ppt": 22.0,
    "temp_c": 29.5,
    "tan_mg_l": 0.05
  }
}
```

### Success Response (201 Created)
```json
{
  "success": true,
  "data": {
    "sample_id": "...",
    "sdri_result": {
      "status": "VALID",
      "probability_pct": 0.0401,
      "risk_class": 1
    }
  }
}
```

---

## 3. Pond Trajectory & Anomaly Assessment
- **URL**: `/api/v1/ponds/{id}/trajectory`
- **Method**: `GET`
- **Description**: Returns historical sampling curves, trajectory slope, and early warning anomaly alert.

### Success Response (200 OK)
```json
{
  "success": true,
  "data": {
    "pond_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
    "data_points": 5,
    "slope": 0.082,
    "trend": "RAPID_INCREASE",
    "trajectory_alert": true,
    "alert_message": "Deviasi kurva terdeteksi: amoA menurun dan TOM meningkat sebelum SDRI menyentuh Kelas 4."
  }
}
```

---

## 4. WhatsApp Farmer Report Preview
- **URL**: `/api/v1/reports/whatsapp`
- **Method**: `POST`
- **Description**: Generates concise, understandable WhatsApp alert formatted for shrimp farm owners.

### Request Body
```json
{
  "pond_code": "Kolam C3",
  "probability_pct": 34.8,
  "risk_class": 3,
  "primary_driver": "AmoA menurun, TOM tinggi",
  "action": "Kurangi pakan 20% dan siphon dasar petak hari ini."
}
```

### Success Response (200 OK)
```json
{
  "success": true,
  "data": {
    "whatsapp_message": "🚨 *PERINGATAN KESEHATAN AIR AQUASEQ*\nPetak: *Kolam C3*\nStatus: *Waspada Tinggi (34.8%)*\nPenyebab: AmoA menurun, TOM tinggi\nTindakan: Kurangi pakan 20% dan siphon dasar petak hari ini."
  }
}
```

---

## 5. Record Cycle Outcome (Calibration Flywheel)
- **URL**: `/api/v1/cycles/outcome`
- **Method**: `POST`
- **Description**: Records final cycle result (Harvest / Outbreak) for model recalibration.

### Request Body
```json
{
  "pond_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "cycle_number": 3,
  "outcome": "OUTBREAK",
  "actual_harvest_kg": 450.0,
  "outbreak_date": "2026-10-20",
  "primary_suspect_pathogen": "WSSV"
}
```

### Success Response (200 OK)
```json
{
  "success": true,
  "data": {
    "status": "RECORDED",
    "message": "Hasil siklus berhasil disimpan untuk kalibrasi bobot V2."
  }
}
```

---

## 6. Health Check
- **URL**: `/api/v1/health`
- **Method**: `GET`
- **Description**: Liveness and readiness probe.

---

## 7. Microbiome Abundance (16S taxa table from partner lab)
Turns a taxa abundance table (output of the lab's 16S classifier) into community metrics, a shift versus the pond baseline, and a 3-5 marker qPCR panel. Nanopore processing itself stays at the lab. Thresholds are heuristic (`MB-V0-heuristik`), not validated against outbreak data.

### Upload: `POST /api/v1/samples/{id}/microbiome` (owner only)
```json
{ "csv": "taxon,genus,reads\nVibrio harveyi,Vibrio,2000\nBacillus subtilis,Bacillus,5000\n", "source": "Lab mitra (16S)" }
```
- Required columns: `taxon`, `reads` (integer >= 0). `genus` optional (defaults to the first word of `taxon`). Comma or semicolon separator; duplicate taxa are summed.
- Baseline = earliest VALID abundance sample in the same pond cycle with an equal or lower `cycle_day`.
- Response `data`: `{ "sample_id", "microbiome": Metrics }`. Invalid CSV returns `400 VALIDATION_ERROR` with `fields.csv`.

`Metrics`:
| Field | Meaning |
|---|---|
| `version` | `MB-V0-heuristik` |
| `status` | `VALID`, or `BELUM_CUKUP` when `total_reads` < 5000 |
| `total_reads`, `observed_taxa` | depth and richness |
| `shannon`, `evenness` | Shannon H (ln) and Pielou evenness |
| `chao1` | bias-corrected Chao1: S_obs + F1(F1-1)/(2(F2+1)) |
| `vibrio_pct` | relative share of genus Vibrio (%) |
| `bray_curtis`, `baseline_sample_id`, `shannon_drop_pct` | shift versus baseline on relative proportions; null without baseline |
| `signals` | `[{ code, value, status, threshold }]`: `porsi_vibrio` (warn > 10%, critical > 30%), `penurunan_keragaman` (warn > 20%, critical > 40%), `pergeseran_komunitas` (warn > 0.4, critical > 0.6), or `kedalaman_baca` when shallow |
| `top_taxa` | up to 8 `{ taxon, genus, pct }` |

### Pond summary: `GET /api/v1/ponds/{id}/microbiome` (owner or assigned technician)
Current cycle only. Response `data`: `{ "pond", "version", "series": [{ sample_id, sample_code, sampling_date, cycle_day, source, uploaded_at, status, shannon, chao1, vibrio_pct, bray_curtis }], "latest": Metrics | null, "markers": [{ target, reason }] }`.
- `markers` always holds `16S` (normalisation) and `amoA` (nitrifiers) plus `pirA`; `vp28` and `swp1` are added when the latest valid qPCR shows WSSV or EHP traces (3-5 markers).

### Sample detail additions: `GET /api/v1/samples/{id}`
- `engineered_features`: all X1-X12 from `sdri.Assess` `[{ code, name, value, status, threshold }]`.
- `microbiome`: `Metrics` or null.

---

## 8. Scientific Explanation (writer LLM + Jev picker, checked by code)
Writes a short Bahasa Indonesia explanation for one sample from machine facts (SDRI result, X1-X12, microbiome metrics, markers) and the curated knowledge pack `internal/science/knowledge.json`. The LLM never computes scores: every number must exist in the facts or the pack, every reason must cite a known source id (`FAKTA` or a pack id), and absolute words ("pasti", "dijamin", "bebas penyakit") are rejected. On any failure the deterministic template is returned.

### `GET /api/v1/samples/{id}/explanation` (owner or assigned technician)
Response `data`: `{ "sample_id", "explanation": Explanation | null, "ai": { "writer": "claude" | "openai" | "template", "writer_model"?, "picker": "jev" | "" } }`.

### `POST /api/v1/samples/{id}/explanation` (owner only)
Generates (or regenerates) and stores the explanation. Same response shape.

`Explanation`: `{ summary, reasons: [{ text, sources[] }], actions: [{ text, source }], limitations[], writer, writer_model?, fallback_reason?, priority_action: { action, confidence, needs_review, picker } | null, sources: [{ id, title, source, doi, text, limits }], generated_at }`. `priority_action` is chosen by Jev from the machine's `mitigation_actions` only; `needs_review` is true below confidence 0.6.

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

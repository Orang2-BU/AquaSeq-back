package domain

import "time"

type RiskStatus string

const (
	StatusValid             RiskStatus = "VALID"
	StatusRejectedInhibitor RiskStatus = "REJECTED_INHIBITOR"
	StatusRejectedInvalidQC RiskStatus = "REJECTED_INVALID_QC"
)

type RiskClass int

const (
	ClassInvalid RiskClass = 0
	ClassAman    RiskClass = 1
	ClassWaspada RiskClass = 2
	ClassSiaga   RiskClass = 3
	ClassBahaya  RiskClass = 4
)

type Farm struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Location   string    `json:"location"`
	OwnerName  string    `json:"owner_name"`
	OwnerPhone string    `json:"owner_phone"`
	CreatedAt  time.Time `json:"created_at"`
}

type Pond struct {
	ID           string    `json:"id"`
	FarmID       string    `json:"farm_id"`
	PondCode     string    `json:"pond_code"`
	AreaSqm      float64   `json:"area_sqm"`
	CurrentCycle int       `json:"current_cycle"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type SampleFeatures struct {
	CTAHPND     *float64 `json:"ct_ahpnd,omitempty"`
	CTWSSV      *float64 `json:"ct_wssv,omitempty"`
	CTEHP       *float64 `json:"ct_ehp,omitempty"`
	CTamoA      *float64 `json:"ct_amoa,omitempty"`
	CT16S       *float64 `json:"ct_16s,omitempty"`
	CTPPC       float64  `json:"ct_ppc"`
	DOMgL       *float64 `json:"do_mg_l,omitempty"`
	TOMMgL      *float64 `json:"tom_mg_l,omitempty"`
	PH          *float64 `json:"ph,omitempty"`
	SalinityPPT *float64 `json:"salinity_ppt,omitempty"`
	TempC       *float64 `json:"temp_c,omitempty"`
	TANMgL      *float64 `json:"tan_mg_l,omitempty"`
	NO2MgL      *float64 `json:"no2_mg_l,omitempty"`
	H2SMgL      *float64 `json:"h2s_mg_l,omitempty"`
	Alkalinity  *float64 `json:"alkalinity,omitempty"`
}

type Sample struct {
	ID           string         `json:"id"`
	PondID       string         `json:"pond_id"`
	SamplingDate string         `json:"sampling_date"`
	CycleDay     int            `json:"cycle_day"`
	SourceType   string         `json:"source_type"`
	Features     SampleFeatures `json:"features"`
	CreatedAt    time.Time      `json:"created_at"`
}

type SDRIInput struct {
	CTPPC float64 `json:"ct_ppc"`
	SQPCR float64 `json:"s_qpcr"`
	SBio  float64 `json:"s_bio"`
	SChem float64 `json:"s_chem"`
}

type EvidenceItem struct {
	Feature   string  `json:"feature"`
	Value     float64 `json:"value"`
	Threshold string  `json:"threshold"`
	Status    string  `json:"status"`
}

type SDRIResult struct {
	Status            RiskStatus     `json:"status"`
	ZScore            float64        `json:"z_score"`
	ProbabilityPct    float64        `json:"probability_pct"`
	RiskClass         RiskClass      `json:"risk_class"`
	RiskLevel         string         `json:"risk_level"`
	SQPCR             float64        `json:"s_qpcr"`
	SBio              float64        `json:"s_bio"`
	SChem             float64        `json:"s_chem"`
	ModelVersion      string         `json:"model_version"`
	Recommendation    string         `json:"recommendation"`
	PredictedDisease  string         `json:"predicted_disease,omitempty"`
	DiseaseName       string         `json:"disease_name,omitempty"`
	Evidence          []EvidenceItem `json:"evidence,omitempty"`
	MitigationActions []string       `json:"mitigation_actions,omitempty"`
}

type TrajectoryPoint struct {
	SamplingDate   string  `json:"sampling_date"`
	CycleDay       int     `json:"cycle_day"`
	ProbabilityPct float64 `json:"probability_pct"`
	RiskClass      int     `json:"risk_class"`
}

type TrajectoryReport struct {
	PondID          string            `json:"pond_id"`
	DataPoints      int               `json:"data_points"`
	Slope           float64           `json:"slope"`
	Trend           string            `json:"trend"`
	TrajectoryAlert bool              `json:"trajectory_alert"`
	AlertMessage    string            `json:"alert_message"`
	History         []TrajectoryPoint `json:"history"`
}

type CycleOutcome struct {
	ID                     string    `json:"id"`
	PondID                 string    `json:"pond_id"`
	CycleNumber            int       `json:"cycle_number"`
	Outcome                string    `json:"outcome"` // HARVEST_SUCCESS, OUTBREAK, EMERGENCY_HARVEST
	ActualHarvestKg        float64   `json:"actual_harvest_kg"`
	OutbreakDate           string    `json:"outbreak_date,omitempty"`
	PrimarySuspectPathogen string    `json:"primary_suspect_pathogen,omitempty"`
	Notes                  string    `json:"notes,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
}

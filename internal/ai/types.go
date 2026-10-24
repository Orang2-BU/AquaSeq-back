package ai

type FeatureMatrix struct {
	CTAhpnd     *float64 `json:"ct_ahpnd,omitempty"`
	CTWssv      *float64 `json:"ct_wssv,omitempty"`
	CTEhp       *float64 `json:"ct_ehp,omitempty"`
	CTAmoa      *float64 `json:"ct_amoa,omitempty"`
	CT16s       *float64 `json:"ct_16s,omitempty"`
	CTPpc       *float64 `json:"ct_ppc,omitempty"`
	DOMgL       *float64 `json:"do_mg_l,omitempty"`
	TOMMgL      *float64 `json:"tom_mg_l,omitempty"`
	PH          *float64 `json:"ph,omitempty"`
	SalinityPpt *float64 `json:"salinity_ppt,omitempty"`
	TempC       *float64 `json:"temp_c,omitempty"`
	TANMgL      *float64 `json:"tan_mg_l,omitempty"`
	NO2MgL      *float64 `json:"no2_mg_l,omitempty"`
	H2SMgL      *float64 `json:"h2s_mg_l,omitempty"`
	Alkalinity  *float64 `json:"alkalinity,omitempty"`
}

type WellRecord struct {
	Well   string  `json:"well"`
	Target string  `json:"target"`
	CT     float64 `json:"ct"`
	Tm     float64 `json:"tm,omitempty"`
	Status string  `json:"status"`
}

type FieldSource struct {
	Cell     string `json:"cell,omitempty"`
	Column   string `json:"column"`
	Row      int    `json:"row"`
	RawValue string `json:"raw_value"`
	Unit     string `json:"unit,omitempty"`
}

type SampleRecord struct {
	SampleCode        string                 `json:"sample_code"`
	Features          FeatureMatrix          `json:"features"`
	Wells             []WellRecord           `json:"wells,omitempty"`
	Confidence        float64                `json:"confidence"`
	QCStatus          string                 `json:"qc_status"`
	BlockPrediction   bool                   `json:"block_prediction"`
	ReviewReasons     []string               `json:"review_reasons,omitempty"`
	MissingFeatures   []string               `json:"missing_features,omitempty"`
	UndetectedTargets []string               `json:"undetected_targets,omitempty"`
	Provenance        map[string]FieldSource `json:"provenance,omitempty"`
}

type RunMeta struct {
	LabName    string `json:"lab_name"`
	Instrument string `json:"instrument"`
	TestedAt   string `json:"tested_at"`
}

type Warning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location"`
}

type IngestResult struct {
	FileName        string         `json:"file_name"`
	FileType        string         `json:"file_type"`
	Run             RunMeta        `json:"run"`
	Samples         []SampleRecord `json:"samples"`
	Wells           []WellRecord   `json:"wells,omitempty"`
	Confidence      float64        `json:"confidence"`
	QCStatus        string         `json:"qc_status"`
	BlockPrediction bool           `json:"block_prediction"`
	Warnings        []Warning      `json:"warnings"`
}

type MachineFacts struct {
	PondID                 string  `json:"pond_id"`
	Disease                string  `json:"disease"`
	SDRIClass              int     `json:"sdri_class"`
	RiskLevel              string  `json:"risk_level"`
	ProbabilityPct         float64 `json:"probability_pct"`
	NH3MgL                 float64 `json:"nh3_mg_l"`
	TOMMgL                 float64 `json:"tom_mg_l"`
	DOMgL                  float64 `json:"do_mg_l"`
	AnomalyFlagged         bool    `json:"anomaly_flagged"`
	ForecastHorizonDays    int     `json:"forecast_horizon_days"`
	ForecastProbabilityPct float64 `json:"forecast_probability_pct"`
	TopSimilarCase         string  `json:"top_similar_case"`
	QCStatus               string  `json:"qc_status"`
	DataAvailable          bool    `json:"data_available"`
	ChemistryAvailable     bool    `json:"chemistry_available"`
	ForecastAvailable      bool    `json:"forecast_available"`
}

type ActionItem struct {
	Action string `json:"action"`
	Source string `json:"source"`
}

type ExplanationReport struct {
	DiseasePotential string       `json:"disease_potential"`
	EvidenceReason   string       `json:"evidence_reason"`
	Actions          []ActionItem `json:"actions"`
	SevenDayOutlook  string       `json:"seven_day_outlook"`
	FullNarrative    string       `json:"full_narrative"`
}

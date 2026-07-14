package app

type Document struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Source      string `json:"source"`
	Status      string `json:"status"`
	PageCount   int    `json:"page_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Tags        []Tag  `json:"tags,omitempty"`
}

type Asset struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	SHA256       string `json:"sha256"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	ByteSize     int64  `json:"byte_size"`
	StoragePath  string `json:"storage_path"`
	CreatedAt    string `json:"created_at"`
}

type DocumentAsset struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Role         string `json:"role"`
	SHA256       string `json:"sha256"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	ByteSize     int64  `json:"byte_size"`
	StoragePath  string `json:"storage_path"`
	DownloadURL  string `json:"download_url"`
	CreatedAt    string `json:"created_at"`
}

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// PromptVersion is an immutable snapshot of a recognition prompt. The active
// snapshot is mirrored to config.json and prompt_path by the API layer.
type PromptVersion struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Content     string `json:"content"`
	SHA256      string `json:"sha256"`
	IsActive    bool   `json:"is_active"`
	CreatedAt   string `json:"created_at"`
	ActivatedAt string `json:"activated_at"`
}

type Page struct {
	ID           string `json:"id"`
	DocumentID   string `json:"document_id"`
	PageNo       int    `json:"page_no"`
	ImageAssetID string `json:"image_asset_id"`
	ThumbAssetID string `json:"thumb_asset_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type PageDetail struct {
	PageID           string   `json:"page_id"`
	DocumentID       string   `json:"document_id"`
	PageNo           int      `json:"page_no"`
	PageStatus       string   `json:"page_status"`
	Width            int      `json:"width"`
	Height           int      `json:"height"`
	ImageAssetID     string   `json:"image_asset_id"`
	ThumbAssetID     string   `json:"thumb_asset_id"`
	RecognitionCount int      `json:"recognition_count"`
	BestConfidence   *float64 `json:"best_confidence"`
	LastProvider     string   `json:"last_provider"`
	LastModel        string   `json:"last_model"`
	LastRecognizedAt string   `json:"last_recognized_at"`
	HasCandidate     bool     `json:"has_candidate"`
	HasManual        bool     `json:"has_manual"`
	HasFinal         bool     `json:"has_final"`
	UpdatedAt        string   `json:"updated_at"`
	ImageURL         string   `json:"image_url"`
	ThumbnailURL     string   `json:"thumbnail_url"`
}

type RecognitionRun struct {
	ID                  string `json:"id"`
	DocumentID          string `json:"document_id"`
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	PromptVersion       string `json:"prompt_version"`
	ConfigJSON          string `json:"config_json"`
	ProfileID           string `json:"recognizer_profile_id,omitempty"`
	Driver              string `json:"recognizer_driver,omitempty"`
	ProfileSnapshotJSON string `json:"profile_snapshot_json,omitempty"`
	ProviderAdapterID   string `json:"provider_adapter_id,omitempty"`
	InputSource         string `json:"input_source"`
	Status              string `json:"status"`
	TotalPages          int    `json:"total_pages"`
	DonePages           int    `json:"done_pages"`
	FailedPages         int    `json:"failed_pages"`
	Error               string `json:"error"`
	StartedAt           string `json:"started_at"`
	FinishedAt          string `json:"finished_at"`
	CreatedAt           string `json:"created_at"`
}

// ProviderAdapter is a user-defined, data-only manifest consumed by one of the
// built-in engines. Secret is internal-only and never serialized to API clients.
type ProviderAdapter struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Engine             string `json:"engine"`
	Endpoint           string `json:"endpoint"`
	Model              string `json:"model"`
	AuthType           string `json:"auth_type"`
	Secret             string `json:"-"`
	SecretSet          bool   `json:"secret_set"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	RequestConfigJSON  string `json:"request_config_json"`
	ResponseConfigJSON string `json:"response_config_json"`
	IsEnabled          bool   `json:"is_enabled"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type RecognitionExperiment struct {
	ID              string                         `json:"id"`
	DocumentID      string                         `json:"document_id"`
	JobID           string                         `json:"job_id"`
	Name            string                         `json:"name"`
	PageIDs         []string                       `json:"page_ids"`
	Status          string                         `json:"status"`
	WinnerVariantID string                         `json:"winner_variant_id,omitempty"`
	Error           string                         `json:"error"`
	CreatedAt       string                         `json:"created_at"`
	StartedAt       string                         `json:"started_at"`
	FinishedAt      string                         `json:"finished_at"`
	Variants        []RecognitionExperimentVariant `json:"variants"`
}

type RecognitionExperimentVariant struct {
	ID                 string   `json:"id"`
	ExperimentID       string   `json:"experiment_id"`
	Name               string   `json:"name"`
	ProfileID          string   `json:"recognizer_profile_id,omitempty"`
	ProviderAdapterID  string   `json:"provider_adapter_id,omitempty"`
	PromptVersionID    string   `json:"prompt_version_id,omitempty"`
	SnapshotJSON       string   `json:"-"`
	ImageSource        string   `json:"image_source"`
	Position           int      `json:"position"`
	Status             string   `json:"status"`
	RunIDs             []string `json:"run_ids"`
	CurrentRunIDs      []string `json:"-"`
	AverageConfidence  *float64 `json:"avg_confidence"`
	DurationMS         int64    `json:"duration_ms"`
	ManualEditDistance int      `json:"manual_edit_distance"`
	SelectedWinner     bool     `json:"selected_winner"`
	Error              string   `json:"error"`
	CreatedAt          string   `json:"created_at"`
	StartedAt          string   `json:"started_at"`
	FinishedAt         string   `json:"finished_at"`
}

// RecognizerProfile is a data-only configuration for an allow-listed driver.
// APIKey is used internally and is never serialized back to clients.
type RecognizerProfile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Driver          string `json:"driver"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"-"`
	APIKeySet       bool   `json:"api_key_set"`
	Model           string `json:"model"`
	ParamsJSON      string `json:"params_json"`
	PromptVersionID string `json:"prompt_version_id"`
	PromptVersion   string `json:"prompt_version,omitempty"`
	PromptSHA256    string `json:"prompt_sha256,omitempty"`
	IsDefault       bool   `json:"is_default"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type CandidateMerge struct {
	ID                  string                  `json:"id"`
	PageID              string                  `json:"page_id"`
	TextVersionID       string                  `json:"text_version_id"`
	SourceResultIDs     []string                `json:"source_result_ids"`
	RecognizerProfileID string                  `json:"recognizer_profile_id,omitempty"`
	Driver              string                  `json:"driver"`
	PromptVersion       string                  `json:"prompt_version"`
	PromptHash          string                  `json:"prompt_hash"`
	RawResponse         string                  `json:"raw_response"`
	CreatedAt           string                  `json:"created_at"`
	TextVersion         TextVersion             `json:"text_version"`
	Sources             []RecognitionResult     `json:"sources"`
	Segments            []CandidateMergeSegment `json:"segments"`
}

type CandidateMergeSegment struct {
	ID               string `json:"id"`
	CandidateMergeID string `json:"candidate_merge_id"`
	Ordinal          int    `json:"ordinal"`
	SourceResultID   string `json:"source_result_id"`
	SourceStart      int    `json:"source_start"`
	SourceEnd        int    `json:"source_end"`
	OutputStart      int    `json:"output_start"`
	OutputEnd        int    `json:"output_end"`
	Text             string `json:"text"`
}

// CrossCheck runs the same pages through several recognizer profiles, compares
// the outputs page by page, and routes disagreements to human review. It never
// finalizes text on its own: the user has the last word.
type CrossCheck struct {
	ID                 string              `json:"id"`
	DocumentID         string              `json:"document_id"`
	JobID              string              `json:"job_id"`
	Name               string              `json:"name"`
	PageIDs            []string            `json:"page_ids"`
	MergeProfileID     string              `json:"merge_profile_id,omitempty"`
	Status             string              `json:"status"`
	Error              string              `json:"error"`
	ConsensusPages     int                 `json:"consensus_pages"`
	DisagreementPages  int                 `json:"disagreement_pages"`
	FailedPages        int                 `json:"failed_pages"`
	CreatedAt          string              `json:"created_at"`
	StartedAt          string              `json:"started_at"`
	FinishedAt         string              `json:"finished_at"`
	Variants           []CrossCheckVariant `json:"variants"`
	Pages              []CrossCheckPage    `json:"pages,omitempty"`
}

type CrossCheckVariant struct {
	ID                string `json:"id"`
	CrossCheckID      string `json:"cross_check_id"`
	Name              string `json:"name"`
	ProfileID         string `json:"recognizer_profile_id,omitempty"`
	ProviderAdapterID string `json:"provider_adapter_id,omitempty"`
	PromptVersionID   string `json:"prompt_version_id,omitempty"`
	SnapshotJSON      string `json:"-"`
	ImageSource       string `json:"image_source"`
	Position          int    `json:"position"`
	Status            string `json:"status"`
	RunID             string `json:"run_id,omitempty"`
	Error             string `json:"error"`
	CreatedAt         string `json:"created_at"`
	StartedAt         string `json:"started_at"`
	FinishedAt        string `json:"finished_at"`
}

type CrossCheckPage struct {
	CrossCheckID       string               `json:"cross_check_id"`
	PageID             string               `json:"page_id"`
	PageNo             int                  `json:"page_no"`
	Status             string               `json:"status"`
	Agreement          *float64             `json:"agreement"`
	ResultIDs          []string             `json:"result_ids"`
	ConsensusVersionID string               `json:"consensus_version_id,omitempty"`
	MergedVersionID    string               `json:"merged_version_id,omitempty"`
	AnnotationID       string               `json:"annotation_id,omitempty"`
	Conflicts          []CrossCheckConflict `json:"conflicts"`
	AdoptedVersionID   string               `json:"adopted_version_id,omitempty"`
	AdoptedAt          string               `json:"adopted_at,omitempty"`
	Error              string               `json:"error"`
	EffectiveKind      string               `json:"effective_kind,omitempty"`
}

// CrossCheckConflict is one line-level divergence between model outputs.
// kind: "omitted" (dropped by the conservative merge), "partial" (kept in the
// merge but missing from some models), "divergent" (no merge available).
type CrossCheckConflict struct {
	Text       string   `json:"text"`
	Kind       string   `json:"kind"`
	PresentIn  []string `json:"present_in"`
	AbsentFrom []string `json:"absent_from,omitempty"`
}

type PageProcessingRun struct {
	ID          string `json:"id"`
	DocumentID  string `json:"document_id"`
	JobID       string `json:"job_id"`
	ConfigJSON  string `json:"config_json"`
	Status      string `json:"status"`
	TotalPages  int    `json:"total_pages"`
	DonePages   int    `json:"done_pages"`
	FailedPages int    `json:"failed_pages"`
	LastError   string `json:"last_error"`
	CreatedAt   string `json:"created_at"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
}

type PageProcessingResult struct {
	ID            string `json:"id"`
	RunID         string `json:"run_id"`
	PageID        string `json:"page_id"`
	PageNo        int    `json:"page_no"`
	SourceAssetID string `json:"source_asset_id"`
	OutputAssetID string `json:"output_asset_id"`
	Status        string `json:"status"`
	ConfigJSON    string `json:"config_json"`
	MetadataJSON  string `json:"metadata_json"`
	LastError     string `json:"last_error"`
	CreatedAt     string `json:"created_at"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	OriginalURL   string `json:"original_url,omitempty"`
	EnhancedURL   string `json:"enhanced_url,omitempty"`
}

type PageSegment struct {
	ID                 string `json:"id"`
	PageID             string `json:"page_id"`
	ProcessingResultID string `json:"processing_result_id"`
	Kind               string `json:"kind"`
	Position           int    `json:"position"`
	X                  int    `json:"x"`
	Y                  int    `json:"y"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	Label              string `json:"label"`
	MetadataJSON       string `json:"metadata_json"`
	CreatedAt          string `json:"created_at"`
}

type PageProcessingPreview struct {
	PageID          string                `json:"page_id"`
	OriginalAssetID string                `json:"original_asset_id"`
	OriginalURL     string                `json:"original_url"`
	Result          *PageProcessingResult `json:"result,omitempty"`
	Segments        []PageSegment         `json:"segments"`
}

type RunPage struct {
	RunID      string `json:"run_id"`
	PageID     string `json:"page_id"`
	PageNo     int    `json:"page_no"`
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	Error      string `json:"error"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

type RecognitionResult struct {
	ID            string   `json:"id"`
	RunID         string   `json:"run_id"`
	PageID        string   `json:"page_id"`
	Text          string   `json:"text"`
	Confidence    *float64 `json:"confidence"`
	RawJSON       string   `json:"raw_json"`
	MetadataJSON  string   `json:"metadata_json"`
	CreatedAt     string   `json:"created_at"`
	Provider      string   `json:"provider,omitempty"`
	Model         string   `json:"model,omitempty"`
	PromptVersion string   `json:"prompt_version,omitempty"`
	ConfigJSON    string   `json:"config_json,omitempty"`
}

type TextVersion struct {
	ID             string `json:"id"`
	DocumentID     string `json:"document_id"`
	PageID         string `json:"page_id"`
	Kind           string `json:"kind"`
	BaseVersionID  string `json:"base_version_id"`
	SourceResultID string `json:"source_result_id"`
	Text           string `json:"text"`
	Status         string `json:"status"`
	CreatedBy      string `json:"created_by"`
	CreatedAt      string `json:"created_at"`
}

type Job struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	TargetType      string `json:"target_type"`
	TargetID        string `json:"target_id"`
	PayloadJSON     string `json:"payload_json"`
	Attempts        int    `json:"attempts"`
	MaxAttempts     int    `json:"max_attempts"`
	LastError       string `json:"last_error"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	ProgressMessage string `json:"progress_message"`
	ResultJSON      string `json:"result_json"`
	CreatedAt       string `json:"created_at"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at"`
}

type JobEvent struct {
	ID        string `json:"id"`
	JobID     string `json:"job_id"`
	Attempt   int    `json:"attempt"`
	Level     string `json:"level"`
	Stage     string `json:"stage"`
	Message   string `json:"message"`
	DataJSON  string `json:"data_json"`
	CreatedAt string `json:"created_at"`
}

type Annotation struct {
	ID            string `json:"id"`
	DocumentID    string `json:"document_id"`
	PageID        string `json:"page_id"`
	TextVersionID string `json:"text_version_id"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Body          string `json:"body"`
	AnchorJSON    string `json:"anchor_json"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type SearchResult struct {
	DocumentID    string `json:"document_id"`
	DocumentTitle string `json:"document_title"`
	PageID        string `json:"page_id"`
	PageNo        int    `json:"page_no"`
	TextVersionID string `json:"text_version_id"`
	Snippet       string `json:"snippet"`
}

type ExportFile struct {
	ID                 string `json:"id"`
	DocumentID         string `json:"document_id"`
	JobID              string `json:"job_id,omitempty"`
	AssetID            string `json:"asset_id,omitempty"`
	Format             string `json:"format"`
	IncludePageNumbers bool   `json:"include_page_numbers"`
	TextScope          string `json:"text_scope"`
	IncludeAnnotations bool   `json:"include_annotations"`
	IncludeUncertain   bool   `json:"include_uncertain"`
	Status             string `json:"status,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	DownloadURL        string `json:"download_url,omitempty"`
	StoragePath        string `json:"storage_path,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	FinishedAt         string `json:"finished_at,omitempty"`
}

// ExportOptions is persisted with the export row and background-job payload,
// so a retry always produces the same snapshot requested by the user.
type ExportOptions struct {
	Format             string `json:"format"`
	IncludePageNumbers bool   `json:"include_page_numbers"`
	TextScope          string `json:"text_scope"`
	IncludeAnnotations bool   `json:"include_annotations"`
	IncludeUncertain   bool   `json:"include_uncertain"`
}

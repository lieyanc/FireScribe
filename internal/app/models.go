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
	ID            string `json:"id"`
	DocumentID    string `json:"document_id"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	ConfigJSON    string `json:"config_json"`
	Status        string `json:"status"`
	TotalPages    int    `json:"total_pages"`
	DonePages     int    `json:"done_pages"`
	FailedPages   int    `json:"failed_pages"`
	Error         string `json:"error"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	CreatedAt     string `json:"created_at"`
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
	ID         string   `json:"id"`
	RunID      string   `json:"run_id"`
	PageID     string   `json:"page_id"`
	Text       string   `json:"text"`
	Confidence *float64 `json:"confidence"`
	RawJSON    string   `json:"raw_json"`
	CreatedAt  string   `json:"created_at"`
	Provider   string   `json:"provider,omitempty"`
	Model      string   `json:"model,omitempty"`
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
	ID          string `json:"id"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id"`
	PayloadJSON string `json:"payload_json"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
	LastError   string `json:"last_error"`
	CreatedAt   string `json:"created_at"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
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
	ID          string `json:"id"`
	DocumentID  string `json:"document_id"`
	Format      string `json:"format"`
	DownloadURL string `json:"download_url"`
	StoragePath string `json:"storage_path"`
}

package app

// Project groups documents into an explicitly ordered collection.
type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	DocumentCount int    `json:"document_count"`
	PageCount     int    `json:"page_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type ProjectDocument struct {
	Document
	Position int    `json:"position"`
	AddedAt  string `json:"added_at"`
}

type ProjectDetail struct {
	Project
	Documents []ProjectDocument `json:"documents"`
}

type ProjectExport struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	JobID              string `json:"job_id,omitempty"`
	AssetID            string `json:"asset_id,omitempty"`
	Format             string `json:"format"`
	IncludePageNumbers bool   `json:"include_page_numbers"`
	TextScope          string `json:"text_scope"`
	IncludeAnnotations bool   `json:"include_annotations"`
	IncludeUncertain   bool   `json:"include_uncertain"`
	Status             string `json:"status"`
	LastError          string `json:"last_error,omitempty"`
	DownloadURL        string `json:"download_url,omitempty"`
	StoragePath        string `json:"storage_path,omitempty"`
	CreatedAt          string `json:"created_at"`
	FinishedAt         string `json:"finished_at,omitempty"`
}

type ProjectExportStart struct {
	ProjectExport
	Job Job `json:"job"`
}

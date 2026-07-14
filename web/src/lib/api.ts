export type Document = {
  id: string;
  title: string;
  description: string;
  author: string;
  source: string;
  status: string;
  page_count: number;
  created_at: string;
  updated_at: string;
  tags?: Tag[];
};

export type Tag = {
  id: string;
  name: string;
  color: string;
};

export type PageDetail = {
  page_id: string;
  document_id: string;
  page_no: number;
  page_status: string;
  width: number;
  height: number;
  image_asset_id: string;
  thumb_asset_id: string;
  recognition_count: number;
  best_confidence: number | null;
  last_provider: string;
  last_model: string;
  last_recognized_at: string;
  has_candidate: boolean;
  has_manual: boolean;
  has_final: boolean;
  updated_at: string;
  image_url: string;
  thumbnail_url: string;
};

export type PageProcessingConfig = {
  auto_crop: boolean;
  normalize_background: boolean;
  deskew: boolean;
  enhance_contrast: boolean;
  detect_segments: boolean;
  crop_padding: number;
  deskew_max_angle: number;
  deskew_step: number;
};

export type PageProcessingRun = {
  id: string;
  document_id: string;
  job_id: string;
  config_json: string;
  status: string;
  total_pages: number;
  done_pages: number;
  failed_pages: number;
  last_error: string;
  created_at: string;
  started_at: string;
  finished_at: string;
};

export type PageProcessingResult = {
  id: string;
  run_id: string;
  page_id: string;
  page_no: number;
  source_asset_id: string;
  output_asset_id: string;
  status: string;
  config_json: string;
  metadata_json: string;
  last_error: string;
  created_at: string;
  started_at: string;
  finished_at: string;
  original_url?: string;
  enhanced_url?: string;
};

export type PageSegment = {
  id: string;
  page_id: string;
  processing_result_id: string;
  kind: string;
  position: number;
  x: number;
  y: number;
  width: number;
  height: number;
  label: string;
  metadata_json: string;
  created_at: string;
};

export type PageProcessingPreview = {
  page_id: string;
  original_asset_id: string;
  original_url: string;
  result?: PageProcessingResult;
  segments: PageSegment[];
};

export type RecognitionRun = {
  id: string;
  document_id: string;
  provider: string;
  model: string;
  prompt_version: string;
  config_json: string;
  recognizer_profile_id?: string;
  provider_adapter_id?: string;
  recognizer_driver?: string;
  profile_snapshot_json?: string;
  input_source: "original" | "enhanced";
  status: string;
  total_pages: number;
  done_pages: number;
  failed_pages: number;
  error: string;
  started_at: string;
  finished_at: string;
  created_at: string;
};

export type RecognizerProfile = {
  id: string;
  name: string;
  driver: "openai-compatible" | "mock";
  base_url: string;
  api_key_set: boolean;
  model: string;
  params_json: string;
  prompt_version_id: string;
  prompt_version?: string;
  prompt_sha256?: string;
  is_default: boolean;
  created_at: string;
  updated_at: string;
};

export type RecognizerProfileInput = {
  name: string;
  driver: "openai-compatible" | "mock";
  base_url?: string;
  api_key?: string;
  model?: string;
  params_json?: string;
  prompt_version_id?: string;
  is_default?: boolean;
};

export type ProviderAdapter = {
  id: string;
  name: string;
  engine: string;
  endpoint: string;
  model: string;
  auth_type: string;
  secret_set: boolean;
  timeout_seconds: number;
  request_config_json: string;
  response_config_json: string;
  is_enabled: boolean;
  created_at: string;
  updated_at: string;
};

export type ProviderAdapterInput = Omit<
  ProviderAdapter,
  "id" | "secret_set" | "created_at" | "updated_at"
> & {
  secret?: string;
};

export type ExperimentVariant = {
  id: string;
  name: string;
  profile_id?: string;
  recognizer_profile_id?: string;
  provider_adapter_id?: string;
  prompt_version_id?: string;
  image_source: "original" | "enhanced";
  position: number;
  status?: string;
  run_ids: string[];
  avg_confidence: number | null;
  duration_ms: number;
  manual_edit_distance: number | null;
  selected_winner: boolean;
  error?: string;
  created_at?: string;
  started_at?: string;
  finished_at?: string;
};

export type RecognitionExperiment = {
  id: string;
  document_id: string;
  name: string;
  status: string;
  page_ids: string[];
  variants: ExperimentVariant[];
  job_id: string;
  winner_variant_id?: string;
  error?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
};

export type RecognitionExperimentInput = {
  name: string;
  page_ids: string[];
  variants: Array<{
    name: string;
    recognizer_profile_id?: string;
    provider_adapter_id?: string;
    prompt_version_id?: string;
    image_source: "original" | "enhanced";
  }>;
};

export type RunPage = {
  run_id: string;
  page_id: string;
  page_no: number;
  status: string;
  attempts: number;
  error: string;
  started_at: string;
  finished_at: string;
};

export type OpenAISettings = {
  base_url: string;
  model: string;
  api_key_set: boolean;
  prompt_version: string;
  temperature: number;
  max_tokens: number;
  max_image_edge: number;
  retry_attempts: number;
};

export type PromptVersion = {
  id: string;
  version: string;
  content: string;
  sha256: string;
  is_active: boolean;
  created_at: string;
  activated_at: string;
};

export type AuthorProfile = {
  id: string;
  name: string;
  notes: string;
  document_count: number;
  term_count: number;
  correction_count: number;
  created_at: string;
  updated_at: string;
};

export type AuthorTerm = {
  id: string;
  author_profile_id: string;
  term: string;
  replacement: string;
  note: string;
  weight: number;
  created_at: string;
  updated_at: string;
};

export type AuthorCorrection = {
  id: string;
  author_profile_id: string;
  document_id: string;
  document_title: string;
  page_id: string;
  page_no: number;
  image_asset_id: string;
  text_version_id: string;
  source_result_id?: string;
  provider?: string;
  model?: string;
  prompt_version?: string;
  source_text: string;
  corrected_text: string;
  kind: string;
  created_at: string;
};

export type AuthorMetricGroup = {
  provider: string;
  model: string;
  prompt_version: string;
  sample_count: number;
  reference_char_count: number;
  edit_distance: number;
  cer: number;
  substitution_count: number;
  omission_count: number;
  addition_count: number;
};

export type AuthorMetricTrend = {
  date: string;
  sample_count: number;
  reference_char_count: number;
  edit_distance: number;
  cer: number;
};

export type AuthorCommonError = {
  type: "substitution" | "omission" | "addition";
  source: string;
  corrected: string;
  count: number;
};

export type AuthorRecognitionMetrics = {
  sample_count: number;
  source_char_count: number;
  reference_char_count: number;
  edit_distance: number;
  cer: number;
  substitution_count: number;
  omission_count: number;
  addition_count: number;
  groups: AuthorMetricGroup[];
  trend: AuthorMetricTrend[];
  common_errors: AuthorCommonError[];
};

export type Settings = {
  use_mock_ocr: boolean;
  request_timeout_seconds: number;
  pdf_render_dpi: number;
  prompt_path: string;
  prompt: string;
  openai: OpenAISettings;
};

export type SettingsInput = {
  use_mock_ocr?: boolean;
  request_timeout_seconds?: number;
  pdf_render_dpi?: number;
  prompt?: string;
  openai?: Partial<Omit<OpenAISettings, "api_key_set">> & { api_key?: string };
};

export type RecognitionResult = {
  id: string;
  run_id: string;
  page_id: string;
  text: string;
  confidence: number | null;
  raw_json: string;
  created_at: string;
  provider?: string;
  model?: string;
  prompt_version?: string;
  config_json?: string;
  metadata_json?: string;
};

export type TextVersion = {
  id: string;
  document_id: string;
  page_id: string;
  kind: string;
  base_version_id: string;
  source_result_id: string;
  text: string;
  status: string;
  created_by: string;
  created_at: string;
};

export type CandidateMerge = {
  id: string;
  page_id: string;
  text_version_id: string;
  source_result_ids: string[];
  recognizer_profile_id?: string;
  driver: string;
  prompt_version: string;
  prompt_hash: string;
  raw_response: string;
  created_at: string;
  text_version: TextVersion;
  sources: RecognitionResult[];
  segments: CandidateMergeSegment[];
};

export type CandidateMergeSegment = {
  id: string;
  candidate_merge_id: string;
  ordinal: number;
  source_result_id: string;
  source_start: number;
  source_end: number;
  output_start: number;
  output_end: number;
  text: string;
};

export type Job = {
  id: string;
  type: string;
  status: string;
  target_type: string;
  target_id: string;
  payload_json: string;
  attempts: number;
  max_attempts: number;
  last_error: string;
  progress_current: number;
  progress_total: number;
  progress_message: string;
  result_json: string;
  created_at: string;
  started_at: string;
  finished_at: string;
};

export type JobEvent = {
  id: string;
  job_id: string;
  attempt: number;
  level: string;
  stage: string;
  message: string;
  data_json: string;
  created_at: string;
};

export type Annotation = {
  id: string;
  document_id: string;
  page_id: string;
  text_version_id: string;
  kind: string;
  status: string;
  body: string;
  anchor_json: string;
  created_at: string;
  updated_at: string;
};

export type SearchResult = {
  document_id: string;
  document_title: string;
  page_id: string;
  page_no: number;
  text_version_id: string;
  snippet: string;
};

export type ReviewQueueItem = {
  document_id: string;
  document_title: string;
  page_id: string;
  page_no: number;
  page_status: string;
  thumbnail_url: string;
  confidence: number | null;
  recognition_count: number;
  open_uncertain_count: number;
  last_provider: string;
  last_model: string;
  updated_at: string;
  low_confidence_segments: Array<{
    text: string;
    start: number;
    end: number;
    confidence: number;
    level: "token" | "word" | "line" | "paragraph";
    source: string;
  }>;
};

export type EvaluationMetrics = {
  benchmark_only: boolean;
  sample_count: number;
  truncated: boolean;
  reference_char_count: number;
  edit_distance: number;
  cer: number;
  substitution_count: number;
  omission_count: number;
  addition_count: number;
  missed_line_count: number;
  guessed_line_count: number;
  reordered_line_count: number;
  low_confidence_item_count: number;
  low_confidence_hit_count: number;
  low_confidence_hit_rate: number;
  average_candidate_seconds: number;
  average_review_seconds: number;
  average_turnaround_seconds: number;
  review_sample_count: number;
  confirmed_last_hour: number;
  pages_per_active_hour: number;
  groups: Array<{ provider: string; model: string; prompt_version: string; sample_count: number; reference_char_count: number; edit_distance: number; cer: number }>;
  trend: Array<{ date: string; sample_count: number; reference_char_count: number; edit_distance: number; cer: number }>;
  samples: Array<{
    document_id: string;
    document_title: string;
    page_id: string;
    page_no: number;
    provider: string;
    model: string;
    prompt_version: string;
    cer: number;
    edit_distance: number;
    reference_char_count: number;
    candidate_seconds: number;
    review_seconds: number;
    turnaround_seconds: number;
    missed_lines: number;
    guessed_lines: number;
    reordered_lines: number;
    low_confidence_item_count: number;
    low_confidence_hit_count: number;
    finalized_at: string;
  }>;
};

export type ExportFile = {
  id: string;
  document_id: string;
  job_id?: string;
  asset_id?: string;
  format: string;
  include_page_numbers?: boolean;
  text_scope?: "current" | "final";
  include_annotations?: boolean;
  include_uncertain?: boolean;
  status?: string;
  last_error?: string;
  download_url: string;
  storage_path: string;
  created_at?: string;
  finished_at?: string;
};

export type Project = {
  id: string;
  name: string;
  description: string;
  document_count: number;
  page_count: number;
  created_at: string;
  updated_at: string;
};

export type ProjectDocument = Document & {
  position: number;
  added_at: string;
};

export type ProjectDetail = Project & {
  documents: ProjectDocument[];
};

export type ProjectExport = {
  id: string;
  project_id: string;
  job_id?: string;
  asset_id?: string;
  format: "md" | "txt" | "docx" | "pdf";
  include_page_numbers: boolean;
  text_scope: "current" | "final";
  include_annotations: boolean;
  include_uncertain: boolean;
  status: string;
  last_error?: string;
  download_url?: string;
  storage_path?: string;
  created_at: string;
  finished_at?: string;
};

export type ExportAnnotationSnapshot = {
  id: string;
  kind: string;
  status: string;
  body: string;
  anchor_json: string;
  rendered_as: "note" | "inline_marker";
};

export type ExportPageSnapshot = {
  ordinal: number;
  document_id: string;
  document_title: string;
  document_position: number;
  page_id: string;
  page_no: number;
  text_version_id: string;
  text_version_kind: string;
  annotations: ExportAnnotationSnapshot[];
  created_at: string;
};

export type DocumentAsset = {
  id: string;
  kind: string;
  role: string;
  sha256: string;
  original_name: string;
  mime_type: string;
  byte_size: number;
  storage_path: string;
  download_url: string;
  created_at: string;
};

export type VersionInfo = {
  version: string;
  commit: string;
  build_time: string;
  update_channel?: string;
  update_repo?: string;
  update_source?: string;
};

export type UpdateStatus = {
  state: string;
  current_version: string;
  latest_version?: string;
  is_prerelease?: boolean;
  progress?: number;
  download_progress?: number;
  error?: string;
  last_check?: string;
  release_notes?: string;
};

export type UpdateCheckResult = {
  has_update: boolean;
  current_version: string;
  latest_version?: string;
  is_prerelease?: boolean;
  release_notes?: string;
  channel: string;
  error?: string;
};

const ADMIN_TOKEN_STORAGE_KEY = "firescribe.admin_token";

export function getAdminToken(): string {
  return localStorage.getItem(ADMIN_TOKEN_STORAGE_KEY) ?? "";
}

export function setAdminToken(token: string) {
  if (token) {
    localStorage.setItem(ADMIN_TOKEN_STORAGE_KEY, token);
  } else {
    localStorage.removeItem(ADMIN_TOKEN_STORAGE_KEY);
  }
}

function adminHeaders(): Record<string, string> {
  const token = getAdminToken();
  return token ? { "X-Admin-Token": token } : {};
}

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    let message = response.statusText;
    try {
      const body = (await response.json()) as { error?: string };
      message = body.error ?? message;
    } catch {
      // keep status text
    }
    throw new ApiError(message, response.status);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export function listDocuments(params: { q?: string; status?: string; tag?: string }) {
  const search = new URLSearchParams();
  if (params.q) search.set("q", params.q);
  if (params.status) search.set("status", params.status);
  if (params.tag) search.set("tag", params.tag);
  return apiFetch<Document[]>(`/api/documents?${search.toString()}`);
}

export function getDocument(id: string) {
  return apiFetch<Document>(`/api/documents/${id}`);
}

export function patchDocument(
  id: string,
  input: Partial<Pick<Document, "title" | "description" | "author" | "source" | "status">>,
) {
  return apiFetch<Document>(`/api/documents/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function deleteDocument(id: string) {
  return apiFetch<void>(`/api/documents/${id}`, { method: "DELETE" });
}

export function listAuthorProfiles() {
  return apiFetch<AuthorProfile[]>("/api/author-profiles");
}

export function createAuthorProfile(input: { name: string; notes?: string }) {
  return apiFetch<AuthorProfile>("/api/author-profiles", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function getAuthorProfile(id: string) {
  return apiFetch<AuthorProfile>(`/api/author-profiles/${id}`);
}

export function patchAuthorProfile(id: string, input: { name?: string; notes?: string }) {
  return apiFetch<AuthorProfile>(`/api/author-profiles/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function deleteAuthorProfile(id: string) {
  return apiFetch<void>(`/api/author-profiles/${id}`, { method: "DELETE" });
}

export function listAuthorTerms(profileID: string) {
  return apiFetch<AuthorTerm[]>(`/api/author-profiles/${profileID}/terms`);
}

export function createAuthorTerm(profileID: string, input: Pick<AuthorTerm, "term" | "replacement" | "note" | "weight">) {
  return apiFetch<AuthorTerm>(`/api/author-profiles/${profileID}/terms`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function patchAuthorTerm(id: string, input: Pick<AuthorTerm, "term" | "replacement" | "note" | "weight">) {
  return apiFetch<AuthorTerm>(`/api/author-terms/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function deleteAuthorTerm(id: string) {
  return apiFetch<void>(`/api/author-terms/${id}`, { method: "DELETE" });
}

export function listAuthorProfileDocuments(profileID: string) {
  return apiFetch<Document[]>(`/api/author-profiles/${profileID}/documents`);
}

export function listAuthorCorrections(profileID: string, limit = 50) {
  return apiFetch<AuthorCorrection[]>(`/api/author-profiles/${profileID}/corrections?limit=${limit}`);
}

export function getAuthorRecognitionMetrics(profileID: string) {
  return apiFetch<AuthorRecognitionMetrics>(`/api/author-profiles/${profileID}/metrics`);
}

export function syncAuthorCorrections(profileID: string) {
  return apiFetch<{ added: number }>(`/api/author-profiles/${profileID}/corrections/sync`, { method: "POST" });
}

export function getDocumentAuthorProfile(documentID: string) {
  return apiFetch<AuthorProfile | null>(`/api/documents/${documentID}/author-profile`);
}

export function setDocumentAuthorProfile(documentID: string, profileID: string) {
  return apiFetch<AuthorProfile | null>(`/api/documents/${documentID}/author-profile`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ profile_id: profileID }),
  });
}

export function listProjects() {
  return apiFetch<Project[]>("/api/projects");
}

export function createProject(input: { name: string; description?: string }) {
  return apiFetch<Project>("/api/projects", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function getProject(id: string) {
  return apiFetch<ProjectDetail>(`/api/projects/${id}`);
}

export function patchProject(id: string, input: { name?: string; description?: string }) {
  return apiFetch<Project>(`/api/projects/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function deleteProject(id: string) {
  return apiFetch<void>(`/api/projects/${id}`, { method: "DELETE" });
}

export function addProjectDocument(projectID: string, documentID: string, position?: number) {
  return apiFetch<ProjectDocument[]>(`/api/projects/${projectID}/documents`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ document_id: documentID, ...(position === undefined ? {} : { position }) }),
  });
}

export function removeProjectDocument(projectID: string, documentID: string) {
  return apiFetch<ProjectDocument[]>(`/api/projects/${projectID}/documents/${documentID}`, { method: "DELETE" });
}

export function reorderProjectDocuments(projectID: string, documentIDs: string[]) {
  return apiFetch<ProjectDocument[]>(`/api/projects/${projectID}/documents/order`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ document_ids: documentIDs }),
  });
}

export function startProjectExport(projectID: string, input: {
  format: "md" | "txt" | "docx" | "pdf";
  include_page_numbers: boolean;
  text_scope?: "current" | "final";
  include_annotations?: boolean;
  include_uncertain?: boolean;
}) {
  return apiFetch<ProjectExport & { job: Job }>(`/api/projects/${projectID}/exports`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function getProjectExport(exportID: string) {
  return apiFetch<ProjectExport>(`/api/project-exports/${exportID}`);
}

export function listProjectExports(projectID: string) {
  return apiFetch<ProjectExport[]>(`/api/projects/${projectID}/exports`);
}

export function getProjectExportSnapshot(exportID: string) {
  return apiFetch<ExportPageSnapshot[]>(`/api/project-exports/${exportID}/snapshot`);
}

export function importDocument(input: {
  files: File[];
  title?: string;
  author?: string;
  source?: string;
  description?: string;
}) {
  const form = new FormData();
  for (const file of input.files) {
    form.append("files", file);
  }
  if (input.title) form.set("title", input.title);
  if (input.author) form.set("author", input.author);
  if (input.source) form.set("source", input.source);
  if (input.description) form.set("description", input.description);
  return apiFetch<Document & { job: Job }>("/api/documents/import", { method: "POST", body: form });
}

export function listPages(documentID: string) {
  return apiFetch<PageDetail[]>(`/api/documents/${documentID}/pages`);
}

export function listPageProcessingRuns(documentID: string) {
  return apiFetch<PageProcessingRun[]>(`/api/documents/${documentID}/page-processing-runs`);
}

export function startPageProcessing(documentID: string, input: { page_ids?: string[]; config: PageProcessingConfig }) {
  return apiFetch<{ run: PageProcessingRun; job: Job }>(`/api/documents/${documentID}/page-processing-runs`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function listPageProcessingResults(runID: string) {
  return apiFetch<PageProcessingResult[]>(`/api/page-processing-runs/${runID}/results`);
}

export function getPageProcessingPreview(pageID: string) {
  return apiFetch<PageProcessingPreview>(`/api/pages/${pageID}/processing-preview`);
}

export function listDocumentAssets(documentID: string) {
  return apiFetch<DocumentAsset[]>(`/api/documents/${documentID}/assets`);
}

export function listTags() {
  return apiFetch<Tag[]>("/api/tags");
}

export function setDocumentTags(documentID: string, names: string[]) {
  return apiFetch<Tag[]>(`/api/documents/${documentID}/tags`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ names }),
  });
}

export function getPage(pageID: string) {
  return apiFetch<PageDetail>(`/api/pages/${pageID}`);
}

export function startRecognition(documentID: string, options?: {
  page_ids?: string[];
  image_source?: "original" | "enhanced";
  recognizer_profile_id?: string;
  provider_adapter_id?: string;
  prompt_version_id?: string;
}) {
  const hasOptions = options && Object.values(options).some((value) => Array.isArray(value) ? value.length > 0 : Boolean(value));
  return apiFetch<{ run: RecognitionRun; job: Job }>(`/api/documents/${documentID}/recognition-runs`, {
    method: "POST",
    headers: hasOptions ? { "Content-Type": "application/json" } : undefined,
    body: hasOptions ? JSON.stringify(options) : undefined,
  });
}

export function listRecognizerProfiles() {
  return apiFetch<RecognizerProfile[]>("/api/recognizer-profiles");
}

export function createRecognizerProfile(input: RecognizerProfileInput) {
  return apiFetch<RecognizerProfile>("/api/recognizer-profiles", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...adminHeaders() },
    body: JSON.stringify(input),
  });
}

export function updateRecognizerProfile(id: string, input: RecognizerProfileInput) {
  return apiFetch<RecognizerProfile>(`/api/recognizer-profiles/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", ...adminHeaders() },
    body: JSON.stringify(input),
  });
}

export function deleteRecognizerProfile(id: string) {
  return apiFetch<void>(`/api/recognizer-profiles/${id}`, { method: "DELETE", headers: adminHeaders() });
}

export function listProviderAdapters() {
  return apiFetch<ProviderAdapter[]>("/api/provider-adapters");
}

export function createProviderAdapter(input: ProviderAdapterInput) {
  return apiFetch<ProviderAdapter>("/api/provider-adapters", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...adminHeaders() },
    body: JSON.stringify(input),
  });
}

export function updateProviderAdapter(id: string, input: ProviderAdapterInput) {
  return apiFetch<ProviderAdapter>(`/api/provider-adapters/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", ...adminHeaders() },
    body: JSON.stringify(input),
  });
}

export function deleteProviderAdapter(id: string) {
  return apiFetch<void>(`/api/provider-adapters/${id}`, { method: "DELETE", headers: adminHeaders() });
}

export async function createRecognitionExperiment(documentID: string, input: RecognitionExperimentInput) {
  const result = await apiFetch<RecognitionExperiment | { experiment: RecognitionExperiment }>(
    `/api/documents/${documentID}/recognition-experiments`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
    },
  );
  return "experiment" in result ? result.experiment : result;
}

export function listRecognitionExperiments(documentID: string) {
  return apiFetch<RecognitionExperiment[]>(`/api/documents/${documentID}/recognition-experiments`);
}

export function getRecognitionExperiment(id: string) {
  return apiFetch<RecognitionExperiment>(`/api/recognition-experiments/${id}`);
}

export function selectRecognitionExperimentWinner(id: string, variantID: string) {
  return apiFetch<RecognitionExperiment>(`/api/recognition-experiments/${id}/winner`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ variant_id: variantID }),
  });
}

export function listRecognitionRuns(documentID: string) {
  return apiFetch<RecognitionRun[]>(`/api/documents/${documentID}/recognition-runs`);
}

export function getRecognitionRun(runID: string) {
  return apiFetch<RecognitionRun>(`/api/recognition-runs/${runID}`);
}

export function listRunPages(runID: string) {
  return apiFetch<RunPage[]>(`/api/recognition-runs/${runID}/pages`);
}

export function retryRun(runID: string) {
  return apiFetch<{ run: RecognitionRun; job: Job }>(`/api/recognition-runs/${runID}/retry`, { method: "POST" });
}

export function cancelRun(runID: string) {
  return apiFetch<{ status: string }>(`/api/recognition-runs/${runID}/cancel`, { method: "POST" });
}

export function listRecognitionResults(pageID: string) {
  return apiFetch<RecognitionResult[]>(`/api/pages/${pageID}/recognition-results`);
}

export function mergeRecognitionCandidates(pageID: string, input: {
  result_ids?: string[];
  recognizer_profile_id?: string;
  segments?: Array<{ source_result_id: string; source_start: number; source_end: number; text: string }>;
}) {
  return apiFetch<CandidateMerge>(`/api/pages/${pageID}/candidate-merges`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function getCandidateMerge(textVersionID: string) {
  return apiFetch<CandidateMerge>(`/api/text-versions/${textVersionID}/candidate-merge`);
}

export function listTextVersions(pageID: string) {
  return apiFetch<TextVersion[]>(`/api/pages/${pageID}/text-versions`);
}

export function createTextVersion(
  pageID: string,
  input: { kind: string; text: string; status?: string; source_result_id?: string; base_version_id?: string },
) {
  return apiFetch<TextVersion>(`/api/pages/${pageID}/text-versions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function searchText(q: string) {
  return apiFetch<SearchResult[]>(`/api/search?q=${encodeURIComponent(q)}`);
}

export function listReviewQueue(maxConfidence = 0.8, documentID?: string) {
  const search = new URLSearchParams({ max_confidence: String(maxConfidence) });
  if (documentID) search.set("document_id", documentID);
  return apiFetch<ReviewQueueItem[]>(`/api/review-queue?${search.toString()}`);
}

export function getEvaluationMetrics(benchmarkOnly = true) {
  return apiFetch<EvaluationMetrics>(`/api/evaluation?benchmark_only=${benchmarkOnly}`);
}

export function recordReviewActivity(pageID: string, input: { session_id: string; active_seconds: number; finished: boolean }, keepalive = false) {
  return apiFetch(`/api/pages/${pageID}/review-activity`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
    keepalive,
  });
}

export async function exportDocument(documentID: string, input: {
  format: string;
  include_page_numbers: boolean;
  text_scope: "current" | "final";
  include_annotations: boolean;
  include_uncertain: boolean;
}) {
  const start = await apiFetch<ExportFile & { job: Job }>(`/api/documents/${documentID}/exports`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  await waitForJob(start.job.id);
  return getExport(start.id);
}

export function listAnnotations(documentID: string, pageID?: string) {
  const search = new URLSearchParams();
  if (pageID) search.set("page_id", pageID);
  return apiFetch<Annotation[]>(`/api/documents/${documentID}/annotations?${search.toString()}`);
}

export function createAnnotation(
  documentID: string,
  input: {
    page_id?: string;
    text_version_id?: string;
    kind: string;
    status?: string;
    body: string;
    anchor_json?: string;
  },
) {
  return apiFetch<Annotation>(`/api/documents/${documentID}/annotations`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function patchAnnotation(annotationID: string, input: { status?: string; body?: string }) {
  return apiFetch<Annotation>(`/api/annotations/${annotationID}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function listJobs() {
  return apiFetch<Job[]>("/api/jobs");
}

export function getJob(jobID: string) {
  return apiFetch<Job>(`/api/jobs/${jobID}`);
}

export function listJobEvents(jobID: string) {
  return apiFetch<JobEvent[]>(`/api/jobs/${jobID}/events`);
}

export function cancelJob(jobID: string) {
  return apiFetch<void>(`/api/jobs/${jobID}/cancel`, { method: "POST" });
}

export function retryJob(jobID: string) {
  return apiFetch<{ run?: RecognitionRun; job: Job }>(`/api/jobs/${jobID}/retry`, { method: "POST" });
}

export function rebuildSearchIndex() {
  return apiFetch<Job>("/api/search/rebuild", { method: "POST" });
}

export function getExport(exportID: string) {
  return apiFetch<ExportFile>(`/api/exports/${exportID}`);
}

export function listDocumentExports(documentID: string) {
  return apiFetch<ExportFile[]>(`/api/documents/${documentID}/exports`);
}

export function getExportSnapshot(exportID: string) {
  return apiFetch<ExportPageSnapshot[]>(`/api/exports/${exportID}/snapshot`);
}

async function waitForJob(jobID: string): Promise<Job> {
  const deadline = Date.now() + 10 * 60 * 1000;
  while (Date.now() < deadline) {
    const job = await getJob(jobID);
    if (job.status === "succeeded") return job;
    if (job.status === "failed" || job.status === "canceled") {
      throw new ApiError(job.last_error || `任务已${job.status === "failed" ? "失败" : "取消"}`, 409);
    }
    await new Promise((resolve) => window.setTimeout(resolve, 500));
  }
  throw new ApiError("等待后台任务超时，可前往任务页查看状态", 408);
}

export function getVersion() {
  return apiFetch<VersionInfo>("/api/version");
}

export function getUpdateStatus() {
  return apiFetch<UpdateStatus>("/api/update/status");
}

export function checkUpdate() {
  return apiFetch<UpdateCheckResult>("/api/update/check", { method: "POST", headers: adminHeaders() });
}

export function applyUpdate() {
  return apiFetch<{ status: string }>("/api/update/apply", { method: "POST", headers: adminHeaders() });
}

export function dismissUpdate() {
  return apiFetch<{ status: string }>("/api/update/dismiss", { method: "POST", headers: adminHeaders() });
}

export function getSettings() {
  return apiFetch<Settings>("/api/settings");
}

export function updateSettings(input: SettingsInput) {
  return apiFetch<Settings>("/api/settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json", ...adminHeaders() },
    body: JSON.stringify(input),
  });
}

export function listPromptVersions() {
  return apiFetch<PromptVersion[]>("/api/prompts");
}

export function createPromptVersion(input: { version: string; content: string }) {
  return apiFetch<PromptVersion>("/api/prompts", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...adminHeaders() },
    body: JSON.stringify(input),
  });
}

export function activatePromptVersion(id: string) {
  return apiFetch<PromptVersion>(`/api/prompts/${id}/activate`, {
    method: "POST",
    headers: adminHeaders(),
  });
}

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

export type RecognitionRun = {
  id: string;
  document_id: string;
  provider: string;
  model: string;
  prompt_version: string;
  config_json: string;
  status: string;
  total_pages: number;
  done_pages: number;
  failed_pages: number;
  error: string;
  started_at: string;
  finished_at: string;
  created_at: string;
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
  created_at: string;
  started_at: string;
  finished_at: string;
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

export type ExportFile = {
  id: string;
  document_id: string;
  format: string;
  download_url: string;
  storage_path: string;
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
  return apiFetch<Document>("/api/documents/import", { method: "POST", body: form });
}

export function listPages(documentID: string) {
  return apiFetch<PageDetail[]>(`/api/documents/${documentID}/pages`);
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

export function startRecognition(documentID: string, pageIDs?: string[]) {
  const hasPages = pageIDs && pageIDs.length > 0;
  return apiFetch<{ run: RecognitionRun; job: Job }>(`/api/documents/${documentID}/recognition-runs`, {
    method: "POST",
    headers: hasPages ? { "Content-Type": "application/json" } : undefined,
    body: hasPages ? JSON.stringify({ page_ids: pageIDs }) : undefined,
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

export function exportDocument(documentID: string, input: { format: string; include_page_numbers: boolean }) {
  return apiFetch<ExportFile>(`/api/documents/${documentID}/exports`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
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

export function cancelJob(jobID: string) {
  return apiFetch<void>(`/api/jobs/${jobID}/cancel`, { method: "POST" });
}

export function retryJob(jobID: string) {
  return apiFetch<{ run: RecognitionRun; job: Job }>(`/api/jobs/${jobID}/retry`, { method: "POST" });
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

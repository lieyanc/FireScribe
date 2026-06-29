import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Check, ChevronLeft, ChevronRight, Copy, MessageSquarePlus, RotateCw, Save, ZoomIn } from "lucide-react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Textarea } from "../components/ui/textarea";
import {
  createTextVersion,
  createAnnotation,
  getDocument,
  getPage,
  listAnnotations,
  listPages,
  listRecognitionResults,
  listTextVersions,
  patchAnnotation,
  RecognitionResult,
  TextVersion,
} from "../lib/api";
import { formatTime, statusLabel } from "../lib/utils";

const kindRank: Record<string, number> = { final: 0, manual: 1, candidate: 2, raw_selected: 3 };

export function ReviewPage() {
  const { documentID = "", pageID = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editor, setEditor] = useState("");
  const [selectedResultID, setSelectedResultID] = useState("");
  const [annotationBody, setAnnotationBody] = useState("");
  const [zoom, setZoom] = useState(100);
  const [rotation, setRotation] = useState(0);
  const editorRef = useRef<HTMLTextAreaElement | null>(null);

  const doc = useQuery({ queryKey: ["document", documentID], queryFn: () => getDocument(documentID), enabled: !!documentID });
  const pages = useQuery({ queryKey: ["pages", documentID], queryFn: () => listPages(documentID), enabled: !!documentID });
  const currentPageID = pageID || pages.data?.[0]?.page_id || "";
  const page = useQuery({ queryKey: ["page", currentPageID], queryFn: () => getPage(currentPageID), enabled: !!currentPageID });
  const results = useQuery({
    queryKey: ["recognition-results", currentPageID],
    queryFn: () => listRecognitionResults(currentPageID),
    enabled: !!currentPageID,
  });
  const versions = useQuery({
    queryKey: ["text-versions", currentPageID],
    queryFn: () => listTextVersions(currentPageID),
    enabled: !!currentPageID,
  });
  const annotations = useQuery({
    queryKey: ["annotations", documentID, currentPageID],
    queryFn: () => listAnnotations(documentID, currentPageID),
    enabled: !!documentID && !!currentPageID,
  });

  const orderedPages = pages.data ?? [];
  const pageIndex = orderedPages.findIndex((item) => item.page_id === currentPageID);
  const previousPage = pageIndex > 0 ? orderedPages[pageIndex - 1] : undefined;
  const nextPage = pageIndex >= 0 && pageIndex < orderedPages.length - 1 ? orderedPages[pageIndex + 1] : undefined;

  const preferredVersion = useMemo(() => pickVersion(versions.data ?? []), [versions.data]);

  useEffect(() => {
    if (!pageID && pages.data?.[0]) {
      navigate(`/review/${documentID}/${pages.data[0].page_id}`, { replace: true });
    }
  }, [documentID, navigate, pageID, pages.data]);

  useEffect(() => {
    if (preferredVersion) {
      setEditor(preferredVersion.text);
    } else if (results.data?.[0]) {
      setEditor(results.data[0].text);
      setSelectedResultID(results.data[0].id);
    } else {
      setEditor("");
      setSelectedResultID("");
    }
  }, [currentPageID, preferredVersion, results.data]);

  const save = useMutation({
    mutationFn: (status: "draft" | "verified") =>
      createTextVersion(currentPageID, {
        kind: status === "verified" ? "final" : "manual",
        text: editor,
        status,
        source_result_id: selectedResultID,
        base_version_id: preferredVersion?.id,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["text-versions", currentPageID] });
      queryClient.invalidateQueries({ queryKey: ["page", currentPageID] });
      queryClient.invalidateQueries({ queryKey: ["pages", documentID] });
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
    },
  });
  const addAnnotation = useMutation({
    mutationFn: (kind: "page_note" | "uncertain_text") => {
      const selection = selectedText(editorRef.current, editor);
      const body = annotationBody.trim() || selection.text || (kind === "uncertain_text" ? "存疑" : "批注");
      return createAnnotation(documentID, {
        page_id: currentPageID,
        text_version_id: preferredVersion?.id,
        kind,
        body,
        anchor_json: JSON.stringify({
          type: selection.text ? "text_range" : "page",
          start: selection.start,
          end: selection.end,
          text: selection.text,
        }),
      });
    },
    onSuccess: () => {
      setAnnotationBody("");
      queryClient.invalidateQueries({ queryKey: ["annotations", documentID, currentPageID] });
    },
  });
  const resolveAnnotation = useMutation({
    mutationFn: (annotationID: string) => patchAnnotation(annotationID, { status: "resolved" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["annotations", documentID, currentPageID] }),
  });

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null;
      const editing = target ? ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName) : false;
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        if (currentPageID) save.mutate("draft");
      }
      if (editing) return;
      if (event.key.toLowerCase() === "j" && nextPage) navigate(`/review/${documentID}/${nextPage.page_id}`);
      if (event.key.toLowerCase() === "k" && previousPage) navigate(`/review/${documentID}/${previousPage.page_id}`);
      if (event.key.toLowerCase() === "v" && currentPageID) save.mutate("verified");
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [currentPageID, documentID, navigate, nextPage, previousPage, save]);

  function copyResult(result: RecognitionResult) {
    setSelectedResultID(result.id);
    setEditor(result.text);
  }

  return (
    <div className="space-y-3">
      <section className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Link to={`/documents/${documentID}`} className="truncate text-lg font-semibold hover:text-primary">
              {doc.data?.title ?? "文档"}
            </Link>
            {page.data ? <Badge value={page.data.page_status} /> : null}
          </div>
          <div className="mt-1 text-sm text-muted-foreground">
            第 {page.data?.page_no ?? "-"} 页 / {orderedPages.length || "-"} 页
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" size="icon" title="上一页" disabled={!previousPage} onClick={() => previousPage && navigate(`/review/${documentID}/${previousPage.page_id}`)}>
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button variant="secondary" size="icon" title="下一页" disabled={!nextPage} onClick={() => nextPage && navigate(`/review/${documentID}/${nextPage.page_id}`)}>
            <ChevronRight className="h-4 w-4" />
          </Button>
          <Button variant="secondary" onClick={() => save.mutate("draft")} disabled={!currentPageID || save.isPending}>
            <Save className="h-4 w-4" />
            保存
          </Button>
          <Button onClick={() => save.mutate("verified")} disabled={!currentPageID || save.isPending}>
            <Check className="h-4 w-4" />
            确认
          </Button>
        </div>
      </section>

      {save.error ? <p className="text-sm text-red-700">{save.error.message}</p> : null}

      <section className="grid min-h-[calc(100vh-185px)] gap-3 lg:grid-cols-[minmax(0,1.08fr)_minmax(420px,0.92fr)]">
        <div className="panel flex min-h-[520px] flex-col overflow-hidden">
          <div className="flex h-11 items-center justify-between border-b border-border px-3">
            <div className="text-sm font-medium">原页</div>
            <div className="flex items-center gap-2">
              <ZoomIn className="h-4 w-4 text-muted-foreground" />
              <input
                className="w-28 accent-primary"
                type="range"
                min={50}
                max={180}
                step={5}
                value={zoom}
                onChange={(event) => setZoom(Number(event.target.value))}
              />
              <Button variant="secondary" size="icon" title="旋转" onClick={() => setRotation((value) => (value + 90) % 360)}>
                <RotateCw className="h-4 w-4" />
              </Button>
            </div>
          </div>
          <div className="flex flex-1 items-center justify-center overflow-auto bg-neutral-100 p-4">
            {page.data ? (
              <img
                src={page.data.image_url}
                alt={`第 ${page.data.page_no} 页`}
                className="max-h-none max-w-none object-contain shadow-sm"
                style={{ width: `${zoom}%`, transform: `rotate(${rotation}deg)` }}
              />
            ) : (
              <div className="text-sm text-muted-foreground">加载中</div>
            )}
          </div>
        </div>

        <div className="flex min-h-[520px] flex-col gap-3">
          <section className="panel overflow-hidden">
            <div className="flex min-h-11 items-center gap-2 overflow-x-auto border-b border-border px-3 py-2">
              {results.data?.length ? (
                results.data.map((result) => (
                  <button
                    key={result.id}
                    className={`h-8 whitespace-nowrap rounded-md border px-2 text-xs ${
                      selectedResultID === result.id ? "border-primary bg-primary text-white" : "border-border bg-white hover:bg-muted"
                    }`}
                    onClick={() => setSelectedResultID(result.id)}
                  >
                    {result.provider ?? "OCR"} · {result.model ?? "model"}
                  </button>
                ))
              ) : (
                <span className="text-sm text-muted-foreground">暂无识别结果</span>
              )}
            </div>
            <div className="max-h-36 overflow-auto p-3 text-sm leading-6 text-muted-foreground">
              {selectedResult(results.data ?? [], selectedResultID)?.text ?? results.data?.[0]?.text ?? ""}
            </div>
            {selectedResult(results.data ?? [], selectedResultID) ? (
              <div className="border-t border-border px-3 py-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => copyResult(selectedResult(results.data ?? [], selectedResultID)!)}
                >
                  <Copy className="h-4 w-4" />
                  复制到编辑区
                </Button>
              </div>
            ) : null}
          </section>

          <Textarea
            ref={editorRef}
            className="min-h-[300px] flex-1 text-base"
            value={editor}
            onChange={(event) => setEditor(event.target.value)}
          />

          {results.data && results.data.length > 1 ? <DiffView results={results.data} /> : null}

          <section className="panel grid gap-2 p-3 text-sm md:grid-cols-4">
            <StatusItem label="识别" value={`${page.data?.recognition_count ?? 0} 次`} />
            <StatusItem label="模型" value={page.data?.last_model || "-"} />
            <StatusItem label="置信度" value={page.data?.best_confidence == null ? "-" : page.data.best_confidence.toFixed(2)} />
            <StatusItem label="更新" value={formatTime(page.data?.updated_at)} />
          </section>

          <section className="panel overflow-hidden">
            <div className="flex items-center justify-between border-b border-border px-3 py-2 text-sm font-medium">
              <span>批注</span>
              <div className="flex gap-2">
                <Button variant="secondary" size="sm" onClick={() => addAnnotation.mutate("page_note")} disabled={addAnnotation.isPending}>
                  <MessageSquarePlus className="h-4 w-4" />
                  批注
                </Button>
                <Button variant="secondary" size="sm" onClick={() => addAnnotation.mutate("uncertain_text")} disabled={addAnnotation.isPending}>
                  存疑
                </Button>
              </div>
            </div>
            <div className="border-b border-border p-3">
              <Textarea
                className="h-20"
                placeholder="批注内容"
                value={annotationBody}
                onChange={(event) => setAnnotationBody(event.target.value)}
              />
            </div>
            <div className="max-h-44 overflow-auto">
              {annotations.data?.length ? (
                annotations.data.map((annotation) => (
                  <div key={annotation.id} className="flex gap-3 border-b border-border px-3 py-2 text-sm last:border-b-0">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <Badge value={annotation.status} />
                        <span className="text-xs text-muted-foreground">{annotation.kind === "uncertain_text" ? "存疑" : "批注"}</span>
                      </div>
                      <div className="mt-1 whitespace-pre-wrap">{annotation.body}</div>
                    </div>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={annotation.status !== "open" || resolveAnnotation.isPending}
                      onClick={() => resolveAnnotation.mutate(annotation.id)}
                    >
                      解决
                    </Button>
                  </div>
                ))
              ) : (
                <div className="px-3 py-4 text-sm text-muted-foreground">暂无批注</div>
              )}
            </div>
          </section>

          <section className="panel overflow-hidden">
            <div className="border-b border-border px-3 py-2 text-sm font-medium">版本</div>
            <div className="max-h-40 overflow-auto">
              {versions.data?.length ? (
                versions.data.map((version) => (
                  <div key={version.id} className="flex items-center justify-between gap-3 border-b border-border px-3 py-2 text-sm last:border-b-0">
                    <span>
                      {statusLabel(version.kind)} · {statusLabel(version.status)}
                    </span>
                    <span className="text-muted-foreground">{formatTime(version.created_at)}</span>
                  </div>
                ))
              ) : (
                <div className="px-3 py-4 text-sm text-muted-foreground">暂无版本</div>
              )}
            </div>
          </section>
        </div>
      </section>
    </div>
  );
}

function pickVersion(versions: TextVersion[]) {
  return [...versions].sort((a, b) => {
    const rank = (kindRank[a.kind] ?? 9) - (kindRank[b.kind] ?? 9);
    if (rank !== 0) return rank;
    return b.created_at.localeCompare(a.created_at);
  })[0];
}

function selectedResult(results: RecognitionResult[], id: string) {
  return results.find((result) => result.id === id);
}

function StatusItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 truncate font-medium">{value}</div>
    </div>
  );
}

function selectedText(textarea: HTMLTextAreaElement | null, fallback: string) {
  if (!textarea) return { start: 0, end: 0, text: "" };
  const start = textarea.selectionStart ?? 0;
  const end = textarea.selectionEnd ?? start;
  return { start, end, text: fallback.slice(start, end) };
}

function DiffView({ results }: { results: RecognitionResult[] }) {
  const base = results[0];
  const other = results[1];
  const segments = useMemo(() => diffChars(base.text, other.text), [base.text, other.text]);
  return (
    <section className="panel overflow-hidden">
      <div className="border-b border-border px-3 py-2 text-sm font-medium">
        分歧 · {base.model || base.provider} / {other.model || other.provider}
      </div>
      <div className="max-h-36 overflow-auto px-3 py-2 text-sm leading-7">
        {segments.map((segment, index) => (
          <span
            key={`${segment.type}-${index}`}
            className={
              segment.type === "insert"
                ? "bg-emerald-100 text-emerald-900"
                : segment.type === "delete"
                  ? "bg-red-100 text-red-800 line-through"
                  : ""
            }
          >
            {segment.text}
          </span>
        ))}
      </div>
    </section>
  );
}

function diffChars(a: string, b: string) {
  const left = Array.from(a).slice(0, 1200);
  const right = Array.from(b).slice(0, 1200);
  const cols = right.length + 1;
  const dp = new Uint16Array((left.length + 1) * cols);
  for (let i = left.length - 1; i >= 0; i -= 1) {
    for (let j = right.length - 1; j >= 0; j -= 1) {
      dp[i * cols + j] =
        left[i] === right[j]
          ? dp[(i + 1) * cols + j + 1] + 1
          : Math.max(dp[(i + 1) * cols + j], dp[i * cols + j + 1]);
    }
  }
  const segments: Array<{ type: "equal" | "insert" | "delete"; text: string }> = [];
  let i = 0;
  let j = 0;
  while (i < left.length && j < right.length) {
    if (left[i] === right[j]) {
      pushSegment(segments, "equal", left[i]);
      i += 1;
      j += 1;
    } else if (dp[(i + 1) * cols + j] >= dp[i * cols + j + 1]) {
      pushSegment(segments, "delete", left[i]);
      i += 1;
    } else {
      pushSegment(segments, "insert", right[j]);
      j += 1;
    }
  }
  while (i < left.length) {
    pushSegment(segments, "delete", left[i]);
    i += 1;
  }
  while (j < right.length) {
    pushSegment(segments, "insert", right[j]);
    j += 1;
  }
  return segments;
}

function pushSegment(segments: Array<{ type: "equal" | "insert" | "delete"; text: string }>, type: "equal" | "insert" | "delete", text: string) {
  const last = segments[segments.length - 1];
  if (last?.type === type) {
    last.text += text;
  } else {
    segments.push({ type, text });
  }
}

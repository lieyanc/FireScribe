import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Check,
  ChevronLeft,
  ChevronRight,
  Copy,
  FileText,
  MessageSquarePlus,
  RotateCw,
  Save,
  TextCursorInput,
  Undo2,
  ZoomIn,
} from "lucide-react";
import { EmptyState, ErrorMessage, IconTooltipButton, PageHeader } from "../components/app/chrome";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Label } from "../components/ui/label";
import { ScrollArea, ScrollBar } from "../components/ui/scroll-area";
import { Separator } from "../components/ui/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../components/ui/tabs";
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

// final 与 manual 同级:取两者中最新的一份,避免旧定稿永远压过新保存的草稿。
const kindRank: Record<string, number> = { final: 0, manual: 0, candidate: 2, raw_selected: 3 };

export function ReviewPage() {
  const { documentID = "", pageID = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editor, setEditor] = useState("");
  const [selectedResultID, setSelectedResultID] = useState("");
  const [annotationBody, setAnnotationBody] = useState("");
  const [zoom, setZoom] = useState(100);
  const [rotation, setRotation] = useState(0);
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const editorValueRef = useRef(editor);
  editorValueRef.current = editor;
  const [naturalSize, setNaturalSize] = useState<{ w: number; h: number } | null>(null);
  const editorRef = useRef<HTMLTextAreaElement | null>(null);
  const currentPageButtonRef = useRef<HTMLButtonElement | null>(null);
  const savedPageRef = useRef("");

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

  const syncedPageRef = useRef<string | null>(null);
  useEffect(() => {
    const pageChanged = syncedPageRef.current !== currentPageID;
    if (pageChanged) {
      syncedPageRef.current = currentPageID;
      setDirty(false);
      // 不带上一页残留的识别结果 ID,防止保存时写错 source_result_id;
      // 同时清掉上一页残留的保存/批注结果与错误提示。
      setSelectedResultID("");
      setNaturalSize(null);
      setEditor("");
      save.reset();
      addAnnotation.reset();
      resolveAnnotation.reset();
    } else if (dirtyRef.current) {
      // 用户有未保存的输入,后台 refetch 不覆盖编辑区。
      return;
    }
    if (preferredVersion) {
      setEditor(preferredVersion.text);
    } else if (versions.isSuccess && results.data?.[0]) {
      // versions 未返回前不用识别结果回填,避免闪现原始 OCR 并误选 source_result_id。
      setEditor(results.data[0].text);
      setSelectedResultID(results.data[0].id);
    } else if (versions.isSuccess) {
      setEditor("");
      setSelectedResultID("");
    }
  }, [currentPageID, preferredVersion, results.data, versions.isSuccess]);

  // 有未保存修改时,拦截刷新/关闭标签页。
  useEffect(() => {
    if (!dirty) return;
    function onBeforeUnload(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [dirty]);

  useEffect(() => {
    currentPageButtonRef.current?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [currentPageID, orderedPages.length]);

  const save = useMutation({
    mutationFn: (input: { status: "draft" | "verified"; text: string }) =>
      createTextVersion(currentPageID, {
        kind: input.status === "verified" ? "final" : "manual",
        text: input.text,
        status: input.status,
        // 仅当选中结果确实属于当前页时才携带溯源 ID。
        source_result_id: selectedResult(results.data ?? [], selectedResultID)?.id ?? "",
        base_version_id: preferredVersion?.id,
      }),
    onSuccess: (saved) => {
      savedPageRef.current = saved.page_id;
      // 请求在途期间用户可能继续输入,只有内容一致才算已同步。
      if (editorValueRef.current === saved.text) {
        setDirty(false);
      }
      queryClient.invalidateQueries({ queryKey: ["text-versions", saved.page_id] });
      queryClient.invalidateQueries({ queryKey: ["page", saved.page_id] });
      queryClient.invalidateQueries({ queryKey: ["pages", saved.document_id] });
      queryClient.invalidateQueries({ queryKey: ["document", saved.document_id] });
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

  const dataLoading = versions.isLoading || results.isLoading;
  const canSave = !!currentPageID && !save.isPending && !dataLoading;

  // 旋转 90°/270° 时 rotate 不改变布局盒,按长短边比例缩小让整页保持可见。
  // 后端未识别的格式(width/height 为 0)退回图片自然尺寸。
  const pageW = page.data?.width || naturalSize?.w || 0;
  const pageH = page.data?.height || naturalSize?.h || 0;
  const rotationFit =
    rotation % 180 !== 0 && pageW > 0 && pageH > 0 ? Math.min(pageW, pageH) / Math.max(pageW, pageH) : 1;

  function goToPage(targetPageID: string) {
    if (targetPageID === currentPageID) return;
    if (dirtyRef.current && !window.confirm("当前页有未保存的修改,离开后将丢失。确定要离开吗?")) return;
    navigate(`/review/${documentID}/${targetPageID}`);
  }

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null;
      const editing = target ? ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName) : false;
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        if (canSave) save.mutate({ status: "draft", text: editor });
      }
      if (editing) return;
      // 排除修饰键组合(如 Ctrl+V 粘贴、Ctrl+J 浏览器快捷键),只响应裸按键。
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.key.toLowerCase() === "j" && nextPage) goToPage(nextPage.page_id);
      if (event.key.toLowerCase() === "k" && previousPage) goToPage(previousPage.page_id);
      if (event.key.toLowerCase() === "v" && canSave && editor.trim()) save.mutate({ status: "verified", text: editor });
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  });

  function copyResult(result: RecognitionResult) {
    setSelectedResultID(result.id);
    setEditor(result.text);
    setDirty(true);
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title={
          <span className="inline-flex min-w-0 items-center gap-2">
            <Link
              to={`/documents/${documentID}`}
              className="truncate hover:text-primary"
              onClick={(event) => {
                if (dirtyRef.current && !window.confirm("当前页有未保存的修改,离开后将丢失。确定要离开吗?")) {
                  event.preventDefault();
                }
              }}
            >
              {doc.data?.title ?? "文档"}
            </Link>
            {page.data ? <Badge value={page.data.page_status} /> : null}
          </span>
        }
        description={`第 ${page.data?.page_no ?? "-"} 页 / ${orderedPages.length || "-"} 页`}
      >
        <IconTooltipButton
          label="上一页 (K)"
          variant="outline"
          size="icon"
          disabled={!previousPage}
          onClick={() => previousPage && goToPage(previousPage.page_id)}
        >
          <ChevronLeft className="size-4" />
        </IconTooltipButton>
        <IconTooltipButton
          label="下一页 (J)"
          variant="outline"
          size="icon"
          disabled={!nextPage}
          onClick={() => nextPage && goToPage(nextPage.page_id)}
        >
          <ChevronRight className="size-4" />
        </IconTooltipButton>
        <Button
          variant={dirty ? "default" : "secondary"}
          onClick={() => save.mutate({ status: "draft", text: editor })}
          disabled={!canSave}
        >
          <Save className="size-4" />
          {save.isPending && save.variables?.status === "draft" ? "保存中…" : "保存"}
        </Button>
        <Button
          onClick={() => {
            if (!editor.trim() && !window.confirm("当前内容为空,确定以空文本定稿吗?")) return;
            save.mutate({ status: "verified", text: editor });
          }}
          disabled={!canSave}
        >
          <Check className="size-4" />
          {save.isPending && save.variables?.status === "verified" ? "确认中…" : "确认"}
        </Button>
      </PageHeader>

      <ErrorMessage message={save.error?.message || addAnnotation.error?.message || resolveAnnotation.error?.message} />

      <Card>
        <ScrollArea className="w-full">
          <div className="flex gap-2 p-2">
            {orderedPages.map((item) => (
              <Button
                key={item.page_id}
                ref={item.page_id === currentPageID ? currentPageButtonRef : undefined}
                variant={item.page_id === currentPageID ? "default" : "ghost"}
                size="sm"
                className="shrink-0"
                onClick={() => goToPage(item.page_id)}
              >
                第 {item.page_no} 页
              </Button>
            ))}
          </div>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>
      </Card>

      <section className="grid min-h-[calc(100vh-250px)] gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(440px,0.95fr)]">
        <Card className="flex min-h-[540px] flex-col overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between space-y-0 border-b p-3">
            <CardTitle className="text-sm">原页</CardTitle>
            <div className="flex items-center gap-2">
              <ZoomIn className="size-4 text-muted-foreground" />
              <input
                className="w-28 accent-primary"
                aria-label="缩放"
                type="range"
                min={50}
                max={180}
                step={5}
                value={zoom}
                onChange={(event) => setZoom(Number(event.target.value))}
              />
              <span className="w-10 text-right text-xs text-muted-foreground">{zoom}%</span>
              <IconTooltipButton label="旋转 90°" variant="outline" size="icon-sm" onClick={() => setRotation((value) => (value + 90) % 360)}>
                <RotateCw className="size-4" />
              </IconTooltipButton>
              <IconTooltipButton
                label="重置视图"
                variant="outline"
                size="icon-sm"
                disabled={zoom === 100 && rotation === 0}
                onClick={() => {
                  setZoom(100);
                  setRotation(0);
                }}
              >
                <Undo2 className="size-4" />
              </IconTooltipButton>
            </div>
          </CardHeader>
          <CardContent className="flex flex-1 overflow-auto bg-muted/60 p-4">
            {page.data ? (
              <img
                src={page.data.image_url}
                alt={`第 ${page.data.page_no} 页`}
                className="m-auto max-h-none max-w-none object-contain shadow-sm"
                style={{ width: `${zoom}%`, transform: `rotate(${rotation}deg) scale(${rotationFit})` }}
                onLoad={(event) =>
                  setNaturalSize({ w: event.currentTarget.naturalWidth, h: event.currentTarget.naturalHeight })
                }
              />
            ) : (
              <EmptyState title="加载中" className="m-auto min-h-60" />
            )}
          </CardContent>
        </Card>

        <div className="flex min-h-[540px] flex-col gap-4">
          <Card className="flex min-h-[420px] flex-1 flex-col overflow-hidden">
            <CardHeader className="space-y-3 border-b p-3">
              <div className="flex items-center justify-between gap-3">
                <CardTitle className="flex items-center gap-2 text-sm">
                  <TextCursorInput className="size-4" />
                  校对文本
                  {dirty ? (
                    <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[11px] font-normal text-muted-foreground">
                      <span className="size-1.5 rounded-full bg-primary" />
                      未保存
                    </span>
                  ) : save.isSuccess && savedPageRef.current === currentPageID ? (
                    <span className="inline-flex items-center gap-1 text-[11px] font-normal text-muted-foreground">
                      <Check className="size-3" />
                      已保存
                    </span>
                  ) : null}
                </CardTitle>
                <div className="text-xs text-muted-foreground">{editor.length} 字</div>
              </div>
              <div className="grid gap-2 text-sm sm:grid-cols-4">
                <StatusItem label="识别" value={`${page.data?.recognition_count ?? 0} 次`} />
                <StatusItem label="模型" value={page.data?.last_model || "-"} />
                <StatusItem label="置信度" value={page.data?.best_confidence == null ? "-" : page.data.best_confidence.toFixed(2)} />
                <StatusItem label="更新" value={formatTime(page.data?.updated_at)} />
              </div>
            </CardHeader>
            <CardContent className="flex flex-1 flex-col p-0">
              <Textarea
                ref={editorRef}
                className="min-h-[320px] flex-1 resize-none rounded-none border-0 text-base leading-8 shadow-none focus-visible:ring-0"
                value={editor}
                onChange={(event) => {
                  setEditor(event.target.value);
                  setDirty(true);
                }}
              />
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t px-3 py-2 text-xs text-muted-foreground">
                <span className="flex items-center gap-1.5">
                  <Kbd>Ctrl+S</Kbd> 保存草稿
                </span>
                <span className="flex items-center gap-1.5">
                  <Kbd>V</Kbd> 确认定稿
                </span>
                <span className="flex items-center gap-1.5">
                  <Kbd>J</Kbd>/<Kbd>K</Kbd> 翻页
                </span>
              </div>
            </CardContent>
          </Card>

          <Tabs defaultValue="ocr" className="w-full">
            <TabsList className="grid w-full grid-cols-4">
              <TabsTrigger value="ocr">识别</TabsTrigger>
              <TabsTrigger value="diff">分歧</TabsTrigger>
              <TabsTrigger value="notes">批注</TabsTrigger>
              <TabsTrigger value="versions">版本</TabsTrigger>
            </TabsList>

            <TabsContent value="ocr">
              <Card>
                <CardContent className="space-y-3 p-3">
                  <div className="flex min-h-9 items-center gap-2 overflow-x-auto">
                    {results.data?.length ? (
                      results.data.map((result) => (
                        <Button
                          key={result.id}
                          variant={selectedResultID === result.id ? "default" : "outline"}
                          size="sm"
                          className="shrink-0"
                          onClick={() => setSelectedResultID(result.id)}
                        >
                          {result.provider ?? "OCR"} · {result.model ?? "model"}
                        </Button>
                      ))
                    ) : (
                      <span className="text-sm text-muted-foreground">暂无识别结果</span>
                    )}
                  </div>
                  <ScrollArea className="h-32 rounded-md border bg-muted/30">
                    <div className="p-3 text-sm leading-6 text-muted-foreground">
                      {selectedResult(results.data ?? [], selectedResultID)?.text ?? results.data?.[0]?.text ?? ""}
                    </div>
                  </ScrollArea>
                  {selectedResult(results.data ?? [], selectedResultID) ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => copyResult(selectedResult(results.data ?? [], selectedResultID)!)}
                    >
                      <Copy className="size-4" />
                      复制到编辑区
                    </Button>
                  ) : null}
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="diff">
              {results.data && results.data.length > 1 ? (
                <DiffView results={results.data} />
              ) : (
                <Card>
                  <EmptyState icon={<FileText className="size-5" />} title="暂无可比较结果" className="min-h-40" />
                </Card>
              )}
            </TabsContent>

            <TabsContent value="notes">
              <Card>
                <CardContent className="space-y-3 p-3">
                  <div className="grid gap-2">
                    <Label htmlFor="annotation-body">批注内容</Label>
                    <Textarea
                      id="annotation-body"
                      className="h-20"
                      value={annotationBody}
                      onChange={(event) => setAnnotationBody(event.target.value)}
                    />
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button variant="secondary" size="sm" onClick={() => addAnnotation.mutate("page_note")} disabled={addAnnotation.isPending}>
                      <MessageSquarePlus className="size-4" />
                      批注
                    </Button>
                    <Button variant="secondary" size="sm" onClick={() => addAnnotation.mutate("uncertain_text")} disabled={addAnnotation.isPending}>
                      存疑
                    </Button>
                  </div>
                  <Separator />
                  <ScrollArea className="h-44">
                    {annotations.data?.length ? (
                      annotations.data.map((annotation) => (
                        <div key={annotation.id} className="flex gap-3 border-b py-2 text-sm last:border-b-0">
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
                      <EmptyState title="暂无批注" className="min-h-36" />
                    )}
                  </ScrollArea>
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="versions">
              <Card>
                <ScrollArea className="h-56">
                  <div className="p-3">
                    {versions.data?.length ? (
                      versions.data.map((version) => (
                        <div key={version.id} className="flex items-center justify-between gap-3 border-b py-2 text-sm last:border-b-0">
                          <span>
                            {statusLabel(version.kind)} · {statusLabel(version.status)}
                          </span>
                          <span className="shrink-0 text-muted-foreground">{formatTime(version.created_at)}</span>
                        </div>
                      ))
                    ) : (
                      <EmptyState title="暂无版本" className="min-h-40" />
                    )}
                  </div>
                </ScrollArea>
              </Card>
            </TabsContent>
          </Tabs>
        </div>
      </section>
    </div>
  );
}

function pickVersion(versions: TextVersion[]) {
  return [...versions].sort((a, b) => {
    const rank = (kindRank[a.kind] ?? 9) - (kindRank[b.kind] ?? 9);
    if (rank !== 0) return rank;
    // RFC3339Nano 会去掉尾随零,字符串比较在同一秒内可能排错,按解析后的时间值比较。
    return timeValue(b.created_at) - timeValue(a.created_at);
  })[0];
}

function timeValue(value: string) {
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
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

function Kbd({ children }: { children: string }) {
  return (
    <kbd className="pointer-events-none inline-flex h-5 select-none items-center rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground">
      {children}
    </kbd>
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
    <Card className="overflow-hidden">
      <CardHeader className="border-b p-3">
        <CardTitle className="text-sm">
          分歧 · {base.model || base.provider} / {other.model || other.provider}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <ScrollArea className="h-40">
          <div className="px-3 py-2 text-sm leading-7">
            {segments.map((segment, index) => (
              <span
                key={`${segment.type}-${index}`}
                className={
                  segment.type === "insert"
                    ? "bg-primary/15 text-primary"
                    : segment.type === "delete"
                      ? "bg-destructive/15 text-destructive line-through"
                      : ""
                }
              >
                {segment.text}
              </span>
            ))}
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
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

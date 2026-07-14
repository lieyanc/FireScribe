import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  Check,
  ChevronLeft,
  ChevronRight,
  Copy,
  FileText,
  GitMerge,
  MessageSquarePlus,
  RefreshCw,
  RotateCw,
  Save,
  ScanLine,
  TextCursorInput,
  Undo2,
  ZoomIn,
} from "lucide-react";
import { toast } from "sonner";
import { EmptyState, IconTooltipButton, LabeledValue, PageHeader, PendingButton } from "../components/app/chrome";
import { StatusBadge } from "../components/app/status-badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "../components/ui/alert-dialog";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Field, FieldGroup, FieldLabel } from "../components/ui/field";
import { ScrollArea, ScrollBar } from "../components/ui/scroll-area";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Separator } from "../components/ui/separator";
import { Slider } from "../components/ui/slider";
import { Spinner } from "../components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../components/ui/tabs";
import { Textarea } from "../components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "../components/ui/toggle-group";
import {
  createTextVersion,
  createAnnotation,
  getCandidateMerge,
  getDocument,
  getPage,
  listAnnotations,
  listPages,
  listRecognitionResults,
  listRecognizerProfiles,
  listTextVersions,
  mergeRecognitionCandidates,
  patchAnnotation,
  recordReviewActivity,
  RecognitionResult,
  TextVersion,
} from "../lib/api";
import { formatTime, statusLabel } from "../lib/format";
import { cn } from "../lib/utils";

// final 与 manual 同级:取两者中最新的一份,避免旧定稿永远压过新保存的草稿。
const kindRank: Record<string, number> = { final: 0, manual: 0, candidate: 2, raw_selected: 3 };

export function ReviewPage() {
  const { documentID = "", pageID = "" } = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [editor, setEditor] = useState("");
  const [selectedResultID, setSelectedResultID] = useState("");
  const [annotationBody, setAnnotationBody] = useState("");
  const [zoom, setZoom] = useState(100);
  const [rotation, setRotation] = useState(0);
  const [regionMode, setRegionMode] = useState(false);
  const [regionSelection, setRegionSelection] = useState<NormalizedRegion | null>(null);
  const [highlightRegion, setHighlightRegion] = useState<NormalizedRegion | null>(null);
  const [pendingTextAnchor, setPendingTextAnchor] = useState<TextRangeAnchor | null>(null);
  const [dirty, setDirty] = useState(false);
  const [pendingNavigation, setPendingNavigation] = useState<string | null>(null);
  const [pendingEditorAction, setPendingEditorAction] = useState<EditorAction | null>(null);
  const [emptyFinalizeOpen, setEmptyFinalizeOpen] = useState(false);
  const [reviewActivityGeneration, setReviewActivityGeneration] = useState(0);
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const editorValueRef = useRef(editor);
  editorValueRef.current = editor;
  const [naturalSize, setNaturalSize] = useState<{ w: number; h: number } | null>(null);
  const editorRef = useRef<HTMLTextAreaElement | null>(null);
  const regionDragStartRef = useRef<{ x: number; y: number } | null>(null);
  const currentPageButtonRef = useRef<HTMLButtonElement | null>(null);
  const savedPageRef = useRef("");
  const focusedQueueRangeRef = useRef("");
  const finishReviewActivityRef = useRef<(() => Promise<void>) | null>(null);

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
  const recognizerProfiles = useQuery({ queryKey: ["recognizer-profiles"], queryFn: listRecognizerProfiles });

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
      setRegionMode(false);
      setRegionSelection(null);
      setHighlightRegion(null);
      setPendingTextAnchor(null);
      setEditor("");
      setPendingEditorAction(null);
      save.reset();
      saveCandidate.reset();
      restoreVersion.reset();
      addAnnotation.reset();
      updateAnnotationStatus.reset();
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

  useEffect(() => {
    const start = Number.parseInt(searchParams.get("focus_start") ?? "", 10);
    const end = Number.parseInt(searchParams.get("focus_end") ?? "", 10);
    const key = `${currentPageID}:${start}:${end}`;
    if (!editor || !Number.isFinite(start) || !Number.isFinite(end) || end <= start || focusedQueueRangeRef.current === key) return;
    focusedQueueRangeRef.current = key;
    requestAnimationFrame(() => focusTextRange(editorRef.current, { type: "text_range", start, end, text: editor.slice(start, end) }));
  }, [currentPageID, editor, searchParams]);

  useEffect(() => {
    if (!currentPageID) return;
    const sessionID = globalThis.crypto?.randomUUID?.() ?? `review-${Date.now()}-${Math.random().toString(16).slice(2)}`;
    let activeSeconds = 0;
    let lastSent = 0;
    let lastTick = Date.now();
    let lastSignal = Date.now();
    let finished = false;

    function accrue() {
      const timestamp = Date.now();
      if (document.visibilityState === "visible" && timestamp - lastSignal <= 60_000) {
        activeSeconds += Math.max(0, timestamp - lastTick) / 1000;
      }
      lastTick = timestamp;
    }
    function signal() {
      accrue();
      lastSignal = Date.now();
    }
    function flush(isFinished: boolean, keepalive = false): Promise<void> {
      accrue();
      if (finished) return Promise.resolve();
      const rounded = Math.round(activeSeconds * 10) / 10;
      if (!isFinished && rounded - lastSent < 1) return Promise.resolve();
      if (isFinished) finished = true;
      lastSent = rounded;
      return recordReviewActivity(currentPageID, { session_id: sessionID, active_seconds: rounded, finished: isFinished }, keepalive).then(() => undefined).catch(() => undefined);
    }
    function finishOnUnload() {
      void flush(true, true);
    }
    function visibilityChanged() {
      lastTick = Date.now();
      lastSignal = lastTick;
    }

    finishReviewActivityRef.current = () => flush(true);

    window.addEventListener("keydown", signal);
    window.addEventListener("pointerdown", signal);
    window.addEventListener("wheel", signal, { passive: true });
    window.addEventListener("beforeunload", finishOnUnload);
    document.addEventListener("visibilitychange", visibilityChanged);
    const interval = window.setInterval(() => {
      accrue();
      if (activeSeconds - lastSent >= 30) flush(false);
    }, 5000);
    return () => {
      window.clearInterval(interval);
      window.removeEventListener("keydown", signal);
      window.removeEventListener("pointerdown", signal);
      window.removeEventListener("wheel", signal);
      window.removeEventListener("beforeunload", finishOnUnload);
      document.removeEventListener("visibilitychange", visibilityChanged);
      if (finishReviewActivityRef.current) finishReviewActivityRef.current = null;
      void flush(true, true);
    };
  }, [currentPageID, reviewActivityGeneration]);

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

  function invalidatePageData(pageID: string, docID: string) {
    for (const key of [["text-versions", pageID], ["page", pageID], ["pages", docID], ["document", docID]]) {
      queryClient.invalidateQueries({ queryKey: key });
    }
  }

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
    onSuccess: (saved, variables) => {
      if (variables.status === "verified") void finishReviewActivityRef.current?.();
      savedPageRef.current = saved.page_id;
      // 请求在途期间用户可能继续输入,只有内容一致才算已同步。
      const editorStillMatches = editorValueRef.current === saved.text;
      if (editorStillMatches) {
        setDirty(false);
      }
      invalidatePageData(saved.page_id, saved.document_id);
      if (variables.status === "draft") {
        toast.success("草稿已保存");
        return;
      }
      if (!editorStillMatches) {
        toast.success("已确认定稿", { description: "提交后有新的输入，本页仍保留为未保存状态。" });
        return;
      }
      const nextUnfinalized =
        orderedPages.slice(pageIndex + 1).find((item) => !item.has_final) ??
        orderedPages.slice(0, Math.max(pageIndex, 0)).find((item) => !item.has_final);
      if (nextUnfinalized) {
        toast.success("已确认定稿", { description: `继续校对第 ${nextUnfinalized.page_no} 页` });
        navigate(`/review/${documentID}/${nextUnfinalized.page_id}`);
      } else {
        toast.success("已确认定稿", { description: "所有页面均已定稿。" });
      }
    },
    onError: (error: Error, variables) => {
      if (variables.status === "verified") setReviewActivityGeneration((value) => value + 1);
      toast.error("保存失败", { description: error.message });
    },
  });
  const saveCandidate = useMutation({
    mutationFn: (result: RecognitionResult) =>
      createTextVersion(currentPageID, {
        kind: "candidate",
        text: result.text,
        status: "draft",
        source_result_id: result.id,
      }),
    onSuccess: (saved) => {
      invalidatePageData(saved.page_id, saved.document_id);
      toast.success("已设为候选稿", { description: "候选正文与 OCR 来源已保存到版本历史。" });
    },
    onError: (error: Error) => toast.error("保存候选稿失败", { description: error.message }),
  });
  const mergeCandidates = useMutation({
    mutationFn: ({ resultIDs, profileID, segments }: { resultIDs?: string[]; profileID?: string; segments?: AlignedSegmentInput[] }) =>
      mergeRecognitionCandidates(currentPageID, {
        result_ids: resultIDs,
        recognizer_profile_id: profileID === "default" ? undefined : profileID,
        segments,
      }),
    onSuccess: (merged) => {
      invalidatePageData(merged.page_id, documentID);
      toast.success("已生成保守合并候选稿", { description: "来源结果、Prompt 哈希与原始响应已保存，可在版本历史中继续校对。" });
    },
    onError: (error: Error) => toast.error("候选合并失败", { description: error.message }),
  });
  const restoreVersion = useMutation({
    mutationFn: (version: TextVersion) =>
      createTextVersion(currentPageID, {
        kind: "manual",
        text: version.text,
        status: "draft",
        source_result_id: version.source_result_id,
        base_version_id: version.id,
      }),
    onSuccess: (saved) => {
      setEditor(saved.text);
      setSelectedResultID(saved.source_result_id || "");
      setDirty(false);
      savedPageRef.current = saved.page_id;
      invalidatePageData(saved.page_id, saved.document_id);
      toast.success("历史版本已恢复", { description: "已创建新的人工草稿，原版本仍保留在历史中。" });
    },
    onError: (error: Error) => toast.error("恢复版本失败", { description: error.message }),
  });
  const addAnnotation = useMutation({
    mutationFn: (kind: "page_note" | "uncertain_text" | "page_region") => {
      const selection = pendingTextAnchor ?? selectedText(editorRef.current, editor);
      const body = annotationBody.trim() || selection.text || (kind === "uncertain_text" ? "存疑" : kind === "page_region" ? "区域批注" : "批注");
      const region = regionSelection && pageW > 0 && pageH > 0 ? {
        x: Math.round(regionSelection.x * pageW), y: Math.round(regionSelection.y * pageH),
        width: Math.round(regionSelection.width * pageW), height: Math.round(regionSelection.height * pageH),
      } : null;
      const anchor = kind === "page_region" && region
        ? {
            type: selection.text ? "text_region_link" : "page_region",
            ...(selection.text ? { start: selection.start, end: selection.end, text: selection.text, region } : region),
          }
        : {
            type: selection.text ? "text_range" : "page",
            start: selection.start,
            end: selection.end,
            text: selection.text,
          };
      return createAnnotation(documentID, {
        page_id: currentPageID,
        text_version_id: preferredVersion?.id,
        kind,
        body,
        anchor_json: JSON.stringify(anchor),
      });
    },
    onSuccess: () => {
      setAnnotationBody("");
      setRegionSelection(null);
      setRegionMode(false);
      setPendingTextAnchor(null);
      queryClient.invalidateQueries({ queryKey: ["annotations", documentID, currentPageID] });
      toast.success("批注已添加");
    },
    onError: (error: Error) => toast.error("添加批注失败", { description: error.message }),
  });
  const updateAnnotationStatus = useMutation({
    mutationFn: ({ annotationID, status }: { annotationID: string; status: "open" | "resolved" | "ignored" }) =>
      patchAnnotation(annotationID, { status }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["annotations", documentID, currentPageID] });
      toast.success("批注状态已更新");
    },
    onError: (error: Error) => toast.error("更新批注失败", { description: error.message }),
  });

  const coreDataReady = page.isSuccess && versions.isSuccess && results.isSuccess;
  const canSave = !!currentPageID && !save.isPending && coreDataReady;
  const coreLoadError = doc.error ?? pages.error ?? page.error ?? results.error ?? versions.error;
  const coreLoadFailed = doc.isError || pages.isError || page.isError || results.isError || versions.isError;

  // 旋转 90°/270° 时 rotate 不改变布局盒,按长短边比例缩小让整页保持可见。
  // 后端未识别的格式(width/height 为 0)退回图片自然尺寸。
  const pageW = page.data?.width || naturalSize?.w || 0;
  const pageH = page.data?.height || naturalSize?.h || 0;
  const rotationFit =
    rotation % 180 !== 0 && pageW > 0 && pageH > 0 ? Math.min(pageW, pageH) / Math.max(pageW, pageH) : 1;

  function requestNavigation(target: string) {
    if (dirtyRef.current) {
      setPendingNavigation(target);
      return;
    }
    navigate(target);
  }

  function goToPage(targetPageID: string) {
    if (targetPageID === currentPageID) return;
    requestNavigation(`/review/${documentID}/${targetPageID}`);
  }

  async function commitFinal(text: string) {
    await finishReviewActivityRef.current?.();
    save.mutate({ status: "verified", text });
  }

  function finalizePage() {
    if (!editor.trim()) {
      setEmptyFinalizeOpen(true);
      return;
    }
    void commitFinal(editor);
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
      if (event.key.toLowerCase() === "v" && canSave && editor.trim()) finalizePage();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [canSave, editor, nextPage?.page_id, previousPage?.page_id]);

  function applyEditorAction(action: EditorAction) {
    if (action.type === "copy_result") {
      const result = selectedResult(results.data ?? [], action.resultID);
      if (!result) return;
      setSelectedResultID(result.id);
      setEditor(result.text);
      setDirty(true);
      return;
    }
    const version = versions.data?.find((item) => item.id === action.versionID);
    if (version) restoreVersion.mutate(version);
  }

  function requestEditorAction(action: EditorAction) {
    if (dirtyRef.current) {
      setPendingEditorAction(action);
      return;
    }
    applyEditorAction(action);
  }

  function copyResult(result: RecognitionResult) {
    requestEditorAction({ type: "copy_result", resultID: result.id });
  }

  function markCandidate(result: RecognitionResult) {
    setSelectedResultID(result.id);
    saveCandidate.mutate(result);
  }

  function toggleRegionMode() {
    setRegionMode((enabled) => {
      const next = !enabled;
      if (next) {
        setRotation(0);
        setHighlightRegion(null);
        const selection = selectedText(editorRef.current, editorValueRef.current);
        setPendingTextAnchor(selection.text ? { type: "text_range", ...selection } : null);
      } else {
        setRegionSelection(null);
      }
      return next;
    });
  }

  function startRegionDrag(event: ReactPointerEvent<HTMLDivElement>) {
    if (!regionMode) return;
    const point = normalizedPointer(event);
    regionDragStartRef.current = point;
    setRegionSelection({ x: point.x, y: point.y, width: 0, height: 0 });
    event.currentTarget.setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function moveRegionDrag(event: ReactPointerEvent<HTMLDivElement>) {
    const start = regionDragStartRef.current;
    if (!regionMode || !start) return;
    const point = normalizedPointer(event);
    setRegionSelection(regionFromPoints(start, point));
  }

  function finishRegionDrag(event: ReactPointerEvent<HTMLDivElement>) {
    const start = regionDragStartRef.current;
    if (!regionMode || !start) return;
    const region = regionFromPoints(start, normalizedPointer(event));
    regionDragStartRef.current = null;
    setRegionSelection(region.width >= 0.005 && region.height >= 0.005 ? region : null);
    event.currentTarget.releasePointerCapture(event.pointerId);
  }

  function focusPageRegion(region: PageRegionAnchor) {
    if (pageW <= 0 || pageH <= 0) return;
    setRotation(0);
    setRegionMode(false);
    setRegionSelection(null);
    setHighlightRegion({
      x: region.x / pageW,
      y: region.y / pageH,
      width: region.width / pageW,
      height: region.height / pageH,
    });
  }

  function focusLinkedAnnotation(textAnchor: TextRangeAnchor | null, region: PageRegionAnchor | null) {
    if (textAnchor) focusTextRange(editorRef.current, textAnchor);
    if (region) focusPageRegion(region);
  }

  const activeResult = selectedResult(results.data ?? [], selectedResultID) ?? results.data?.[0];

  if (coreLoadFailed) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader title="校对页加载失败" description="无法获取校对所需数据，请重试。" />
        <Card>
          <EmptyState title="无法打开校对页" description={coreLoadError?.message || "未知错误"}>
            <div className="flex flex-wrap justify-center gap-2">
              <Button variant="outline" onClick={() => navigate(`/documents/${documentID}`)}>返回文档</Button>
              <Button
                onClick={() => {
                  void doc.refetch();
                  void pages.refetch();
                  if (currentPageID) {
                    void page.refetch();
                    void results.refetch();
                    void versions.refetch();
                  }
                }}
              >
                <RefreshCw />
                重试
              </Button>
            </div>
          </EmptyState>
        </Card>
      </div>
    );
  }

  if (pages.isSuccess && orderedPages.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader title={doc.data?.title ?? "文档校对"} description="当前文档还没有可校对页面。" />
        <Card>
          <EmptyState title="暂无页面" description="页面拆分完成后即可开始校对。">
            <Button variant="outline" onClick={() => navigate(`/documents/${documentID}`)}>返回文档</Button>
          </EmptyState>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={
          <span className="inline-flex min-w-0 items-center gap-2">
            <Link
              to={`/documents/${documentID}`}
              className="truncate hover:text-primary"
              onClick={(event) => {
                if (dirtyRef.current) {
                  event.preventDefault();
                  setPendingNavigation(`/documents/${documentID}`);
                }
              }}
            >
              {doc.data?.title ?? "文档"}
            </Link>
            {page.data ? <StatusBadge value={page.data.page_status} /> : null}
          </span>
        }
        description={`第 ${page.data?.page_no ?? "-"} 页 / ${orderedPages.length || "-"} 页 · ${page.data ? pageReviewLabel(page.data) : "加载中"}`}
      >
        <IconTooltipButton
          label="上一页 (K)"
          variant="outline"
          size="icon"
          disabled={!previousPage}
          onClick={() => previousPage && goToPage(previousPage.page_id)}
        >
          <ChevronLeft />
        </IconTooltipButton>
        <IconTooltipButton
          label="下一页 (J)"
          variant="outline"
          size="icon"
          disabled={!nextPage}
          onClick={() => nextPage && goToPage(nextPage.page_id)}
        >
          <ChevronRight />
        </IconTooltipButton>
        <PendingButton
          variant={dirty ? "default" : "secondary"}
          onClick={() => save.mutate({ status: "draft", text: editor })}
          disabled={!canSave}
          pending={save.isPending && save.variables?.status === "draft"}
          pendingLabel="保存中…"
          icon={<Save />}
        >
          保存
        </PendingButton>
        <PendingButton
          onClick={finalizePage}
          disabled={!canSave}
          pending={save.isPending && save.variables?.status === "verified"}
          pendingLabel="定稿中…"
          icon={<Check />}
        >
          确认定稿
        </PendingButton>
      </PageHeader>

      <Card>
        <ScrollArea className="w-full">
          <div className="flex gap-2 p-2">
            {orderedPages.map((item) => (
              <Button
                key={item.page_id}
                ref={item.page_id === currentPageID ? currentPageButtonRef : undefined}
                variant={item.page_id === currentPageID ? "default" : "outline"}
                size="sm"
                className="shrink-0"
                aria-current={item.page_id === currentPageID ? "page" : undefined}
                title={`第 ${item.page_no} 页 · ${pageReviewLabel(item)}`}
                onClick={() => goToPage(item.page_id)}
              >
                {item.has_final ? <Check /> : item.has_manual ? <Save /> : <FileText />}
                第 {item.page_no} 页 · {pageReviewLabel(item)}
              </Button>
            ))}
          </div>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>
      </Card>

      <section className="grid min-h-[calc(100vh-250px)] gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(440px,0.95fr)]">
        <Card className="flex min-h-[540px] flex-col overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between border-b p-3">
            <CardTitle className="text-sm">原页</CardTitle>
            <div className="flex items-center gap-2 [&_svg]:size-4">
              <ZoomIn />
              <Slider
                className="w-28"
                aria-label="缩放"
                min={50}
                max={180}
                step={5}
                value={[zoom]}
                onValueChange={(value) => setZoom(value[0])}
              />
              <span className="w-10 text-right text-xs text-muted-foreground">{zoom}%</span>
              <IconTooltipButton
                label="框选区域批注"
                variant={regionMode ? "default" : "outline"}
                size="icon-sm"
                onClick={toggleRegionMode}
              >
                <ScanLine />
              </IconTooltipButton>
              <IconTooltipButton
                label="旋转 90°"
                variant="outline"
                size="icon-sm"
                onClick={() => {
                  setRegionMode(false);
                  setRegionSelection(null);
                  setHighlightRegion(null);
                  setRotation((value) => (value + 90) % 360);
                }}
              >
                <RotateCw />
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
                <Undo2 />
              </IconTooltipButton>
            </div>
          </CardHeader>
          <CardContent className="flex flex-1 overflow-auto bg-muted/60 p-4">
            {page.data ? (
              <div
                className={cn("relative m-auto shrink-0 touch-none", regionMode && "cursor-crosshair select-none")}
                style={{ width: `${zoom}%` }}
                onPointerDown={startRegionDrag}
                onPointerMove={moveRegionDrag}
                onPointerUp={finishRegionDrag}
                onPointerCancel={() => {
                  regionDragStartRef.current = null;
                  setRegionSelection(null);
                }}
              >
                <img
                  src={page.data.image_url}
                  alt={`第 ${page.data.page_no} 页`}
                  draggable={false}
                  className="block h-auto w-full max-w-none object-contain shadow-sm"
                  style={{ transform: `rotate(${rotation}deg) scale(${rotationFit})` }}
                  onLoad={(event) =>
                    setNaturalSize({ w: event.currentTarget.naturalWidth, h: event.currentTarget.naturalHeight })
                  }
                />
                {highlightRegion ? <RegionOverlay region={highlightRegion} className="border-warning bg-warning/20" /> : null}
                {regionSelection ? <RegionOverlay region={regionSelection} className="border-primary bg-primary/15" /> : null}
              </div>
            ) : (
              <EmptyState title="加载中" className="m-auto min-h-60" />
            )}
          </CardContent>
        </Card>

        <div className="flex min-h-[540px] flex-col gap-4">
          <Card className="flex min-h-[420px] flex-1 flex-col overflow-hidden">
            <CardHeader className="flex flex-col gap-3 border-b p-3">
              <div className="flex items-center justify-between gap-3">
                <CardTitle className="flex items-center gap-2 text-sm [&_svg]:size-4">
                  <TextCursorInput />
                  校对文本
                  {dirty ? (
                    <Badge variant="secondary">未保存</Badge>
                  ) : save.isSuccess && savedPageRef.current === currentPageID ? (
                    <Badge variant="outline">
                      <Check />
                      已保存
                    </Badge>
                  ) : null}
                </CardTitle>
                <div className="text-xs text-muted-foreground">{editor.length} 字</div>
              </div>
              <div className="grid gap-2 text-sm sm:grid-cols-4">
                <LabeledValue label="识别" value={`${page.data?.recognition_count ?? 0} 次`} className="font-medium" />
                <LabeledValue label="模型" value={page.data?.last_model || "-"} className="font-medium" />
                <LabeledValue label="置信度" value={page.data?.best_confidence == null ? "-" : page.data.best_confidence.toFixed(2)} className="font-medium" />
                <LabeledValue label="更新" value={formatTime(page.data?.updated_at)} className="font-medium" />
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
                <CardContent className="flex flex-col gap-3 p-3">
                  {results.data?.length && activeResult ? (
                    <>
                      <div className="min-h-9 overflow-x-auto pb-1">
                        <ToggleGroup
                          type="single"
                          variant="outline"
                          size="sm"
                          value={activeResult.id}
                          onValueChange={(value) => value && setSelectedResultID(value)}
                          className="w-max justify-start"
                          aria-label="选择识别结果"
                        >
                          {results.data.map((result) => (
                            <ToggleGroupItem key={result.id} value={result.id} className="shrink-0">
                              {result.provider ?? "OCR"} · {result.model ?? "model"}
                            </ToggleGroupItem>
                          ))}
                        </ToggleGroup>
                      </div>
                      <ScrollArea className="h-32 rounded-md border bg-muted/30">
                        <div className="p-3 text-sm leading-6 text-muted-foreground">{activeResult.text}</div>
                      </ScrollArea>
                      <div className="flex flex-wrap gap-2">
                        <PendingButton
                          size="sm"
                          onClick={() => markCandidate(activeResult)}
                          disabled={saveCandidate.isPending}
                          pending={saveCandidate.isPending}
                          pendingLabel="保存中…"
                          icon={<Check />}
                        >
                          设为候选稿
                        </PendingButton>
                        <Button variant="secondary" size="sm" onClick={() => copyResult(activeResult)}>
                          <Copy />
                          复制到编辑区
                        </Button>
                      </div>
                      <RecognitionDetails result={activeResult} />
                    </>
                  ) : (
                    <EmptyState title={results.isLoading ? "加载识别结果" : "暂无识别结果"} className="min-h-40" />
                  )}
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="diff">
              {results.data && results.data.length > 1 ? (
                <DiffView
                  results={results.data}
                  profiles={recognizerProfiles.data ?? []}
                  isMerging={mergeCandidates.isPending}
                  onMerge={(resultIDs, profileID) => mergeCandidates.mutate({ resultIDs, profileID })}
                  onAlignedMerge={(segments) => mergeCandidates.mutate({ segments })}
                />
              ) : (
                <Card>
                  <EmptyState icon={<FileText />} title="暂无可比较结果" className="min-h-40" />
                </Card>
              )}
            </TabsContent>

            <TabsContent value="notes">
              <Card>
                <CardContent className="flex flex-col gap-3 p-3">
                  <FieldGroup className="gap-3">
                    <Field>
                      <FieldLabel htmlFor="annotation-body">批注内容</FieldLabel>
                      <Textarea
                        id="annotation-body"
                        className="h-20"
                        value={annotationBody}
                        onChange={(event) => setAnnotationBody(event.target.value)}
                      />
                    </Field>
                  </FieldGroup>
                  <div className="flex flex-wrap gap-2">
                    <Button variant="secondary" size="sm" onClick={() => addAnnotation.mutate("page_note")} disabled={addAnnotation.isPending}>
                      {addAnnotation.isPending && addAnnotation.variables === "page_note" ? <Spinner /> : <MessageSquarePlus />}
                      批注
                    </Button>
                    <Button variant="secondary" size="sm" onClick={() => addAnnotation.mutate("uncertain_text")} disabled={addAnnotation.isPending}>
                      {addAnnotation.isPending && addAnnotation.variables === "uncertain_text" ? <Spinner /> : null}
                      存疑
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => addAnnotation.mutate("page_region")}
                      disabled={addAnnotation.isPending || !regionSelection}
                    >
                      {addAnnotation.isPending && addAnnotation.variables === "page_region" ? <Spinner /> : <ScanLine />}
                      保存区域批注
                    </Button>
                  </div>
                  {regionMode ? (
                    <p className="text-xs text-muted-foreground">{pendingTextAnchor ? `已绑定文本“${pendingTextAnchor.text}”，请在左侧框图后保存。` : "在左侧原页上拖动框选区域，再点击保存。可先在正文选择文本以创建双向联动批注。"}</p>
                  ) : null}
                  <Separator />
                  <ScrollArea className="h-44">
                    {annotations.isError ? (
                      <EmptyState title="批注加载失败" description={(annotations.error as Error).message} className="min-h-36">
                        <Button variant="outline" size="sm" onClick={() => void annotations.refetch()} disabled={annotations.isFetching}>
                          {annotations.isFetching ? <Spinner /> : <RefreshCw />}
                          重试
                        </Button>
                      </EmptyState>
                    ) : annotations.data?.length ? (
                      annotations.data.map((annotation) => {
                        const textAnchor = annotationTextAnchor(annotation.anchor_json);
                        const pageRegion = annotationPageRegion(annotation.anchor_json);
                        return (
                          <div key={annotation.id} className="flex gap-3 border-b py-2 text-sm last:border-b-0">
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <StatusBadge value={annotation.status} />
                                <span className="text-xs text-muted-foreground">
                                  {annotation.kind === "uncertain_text" ? "存疑" : annotation.kind === "page_region" ? "区域批注" : "批注"}
                                </span>
                              </div>
                              <div className="mt-1 whitespace-pre-wrap">{annotation.body}</div>
                              {textAnchor?.text ? (
                                <Button
                                  variant="link"
                                  size="sm"
                                  className="mt-1 h-auto max-w-full justify-start whitespace-normal px-0 text-left"
                                  onClick={() => focusLinkedAnnotation(textAnchor, pageRegion)}
                                >
                                  定位：“{textAnchor.text}”
                                </Button>
                              ) : null}
                              {pageRegion ? (
                                <Button
                                  variant="link"
                                  size="sm"
                                  className="mt-1 h-auto px-0"
                                  onClick={() => focusLinkedAnnotation(textAnchor, pageRegion)}
                                >
                                  <ScanLine />
                                  定位原图区域
                                </Button>
                              ) : null}
                            </div>
                            <div className="flex shrink-0 flex-col gap-1">
                              {annotation.status === "open" ? (
                                <>
                                  <Button
                                    variant="secondary"
                                    size="sm"
                                    disabled={updateAnnotationStatus.isPending}
                                    onClick={() => updateAnnotationStatus.mutate({ annotationID: annotation.id, status: "resolved" })}
                                  >
                                    {updateAnnotationStatus.isPending && updateAnnotationStatus.variables?.annotationID === annotation.id && updateAnnotationStatus.variables.status === "resolved" ? <Spinner /> : null}
                                    解决
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    disabled={updateAnnotationStatus.isPending}
                                    onClick={() => updateAnnotationStatus.mutate({ annotationID: annotation.id, status: "ignored" })}
                                  >
                                    {updateAnnotationStatus.isPending && updateAnnotationStatus.variables?.annotationID === annotation.id && updateAnnotationStatus.variables.status === "ignored" ? <Spinner /> : null}
                                    忽略
                                  </Button>
                                </>
                              ) : (
                                <Button
                                  variant="secondary"
                                  size="sm"
                                  disabled={updateAnnotationStatus.isPending}
                                  onClick={() => updateAnnotationStatus.mutate({ annotationID: annotation.id, status: "open" })}
                                >
                                  {updateAnnotationStatus.isPending && updateAnnotationStatus.variables?.annotationID === annotation.id ? <Spinner /> : null}
                                  重新打开
                                </Button>
                              )}
                            </div>
                          </div>
                        );
                      })
                    ) : (
                      <EmptyState title="暂无批注" className="min-h-36" />
                    )}
                  </ScrollArea>
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="versions">
              <VersionHistory
                versions={versions.data ?? []}
                currentVersion={preferredVersion}
                results={results.data ?? []}
                isRestoring={restoreVersion.isPending}
                onRestore={(version) => requestEditorAction({ type: "restore_version", versionID: version.id })}
              />
            </TabsContent>
          </Tabs>
        </div>
      </section>

      <AlertDialog open={pendingNavigation !== null} onOpenChange={(open) => !open && setPendingNavigation(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle>
            <AlertDialogDescription>
              当前页的编辑内容尚未保存。继续离开会丢失这些修改，此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                const target = pendingNavigation;
                setPendingNavigation(null);
                setDirty(false);
                if (target) navigate(target);
              }}
            >
              放弃修改并离开
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={emptyFinalizeOpen} onOpenChange={setEmptyFinalizeOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>以空文本定稿？</AlertDialogTitle>
            <AlertDialogDescription>
              当前编辑区为空。确认后会创建一个空白定稿版本，你仍可稍后重新编辑。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>返回检查</AlertDialogCancel>
            <AlertDialogAction onClick={() => void commitFinal(editor)}>仍然定稿</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={pendingEditorAction !== null} onOpenChange={(open) => !open && setPendingEditorAction(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>替换未保存的编辑内容？</AlertDialogTitle>
            <AlertDialogDescription>
              当前编辑区有未保存修改。继续{pendingEditorAction?.type === "restore_version" ? "恢复历史版本" : "复制 OCR 结果"}会替换这些内容。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction asChild>
              <Button
                variant="destructive"
                onClick={() => {
                  const action = pendingEditorAction;
                  setPendingEditorAction(null);
                  if (action) applyEditorAction(action);
                }}
              >
                放弃修改并继续
              </Button>
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

type EditorAction =
  | { type: "copy_result"; resultID: string }
  | { type: "restore_version"; versionID: string };

function DiffSegments({ segments }: { segments: ReturnType<typeof diffChars> }) {
  return (
    <>
      {segments.map((segment, index) => (
        <span
          key={`${segment.type}-${index}`}
          className={cn(
            segment.type === "insert"
              ? "bg-primary/15 text-primary"
              : segment.type === "delete"
                ? "bg-destructive/15 text-destructive line-through"
                : "",
          )}
        >
          {segment.text}
        </span>
      ))}
    </>
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

function VersionHistory({
  versions,
  currentVersion,
  results,
  isRestoring,
  onRestore,
}: {
  versions: TextVersion[];
  currentVersion?: TextVersion;
  results: RecognitionResult[];
  isRestoring: boolean;
  onRestore: (version: TextVersion) => void;
}) {
  const [selectedID, setSelectedID] = useState(currentVersion?.id ?? versions[0]?.id ?? "");
  useEffect(() => {
    setSelectedID(currentVersion?.id ?? versions[0]?.id ?? "");
  }, [currentVersion?.page_id]);
  const selected = versions.find((version) => version.id === selectedID) ?? currentVersion ?? versions[0];
  const candidateMerge = useQuery({
    queryKey: ["candidate-merge", selected?.id],
    queryFn: () => getCandidateMerge(selected.id),
    enabled: !!selected && ["conservative-merge", "aligned-segment-merge"].includes(selected.created_by),
  });
  const source = selected ? versionSource(selected, versions, results) : null;
  const segments = useMemo(
    () => currentVersion && selected ? diffChars(currentVersion.text, selected.text) : [],
    [currentVersion, selected],
  );

  if (!versions.length || !selected) {
    return (
      <Card>
        <CardHeader className="p-3">
          <CardTitle className="text-sm">版本历史</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <EmptyState title="暂无版本" className="min-h-40" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="overflow-hidden">
      <CardHeader className="border-b p-3">
        <CardTitle className="text-sm">版本历史</CardTitle>
        <CardDescription>选择版本可查看正文、OCR 来源，并与当前有效版本比较。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 p-3">
        <ScrollArea className="h-36 rounded-md border">
          <div className="flex flex-col gap-1 p-2">
            {versions.map((version) => (
              <Button
                key={version.id}
                variant={version.id === selected.id ? "secondary" : "ghost"}
                size="sm"
                className="h-auto justify-between gap-3 py-2"
                onClick={() => setSelectedID(version.id)}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <Badge variant="outline">{statusLabel(version.kind)}</Badge>
                  <span className="truncate text-left">{versionSource(version, versions, results).summary}</span>
                  {version.id === currentVersion?.id ? <Badge variant="secondary">当前</Badge> : null}
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">{formatTime(version.created_at)}</span>
              </Button>
            ))}
          </div>
        </ScrollArea>

        <div className="grid gap-2 text-xs sm:grid-cols-3">
          <LabeledValue label="类型" value={`${statusLabel(selected.kind)} · ${statusLabel(selected.status)}`} className="font-medium" />
          <LabeledValue label="创建者" value={selected.created_by || "-"} className="font-medium" />
          <LabeledValue label="来源" value={source?.detail || "-"} className="font-medium" />
        </div>
        {candidateMerge.data ? <CandidateMergeDetails merge={candidateMerge.data} /> : null}
        <div>
          <div className="mb-1 text-xs font-medium text-muted-foreground">正文</div>
          <ScrollArea className="h-28 rounded-md border bg-muted/20">
            <div className="whitespace-pre-wrap p-3 text-sm leading-6">{selected.text || "（空文本）"}</div>
          </ScrollArea>
        </div>
        <div className="flex items-center justify-between gap-3">
          <div className="text-xs text-muted-foreground">
            {selected.base_version_id ? `基于版本 ${shortID(selected.base_version_id)}` : "无上游版本"}
          </div>
          <PendingButton
            size="sm"
            variant="secondary"
            disabled={isRestoring || selected.id === currentVersion?.id}
            onClick={() => onRestore(selected)}
            pending={isRestoring}
            pendingLabel="恢复中…"
            icon={<Undo2 />}
          >
            恢复为人工草稿
          </PendingButton>
        </div>
        <Separator />
        <div>
          <div className="mb-1 flex items-center justify-between gap-2 text-xs font-medium text-muted-foreground">
            <span>当前版本 → 所选历史版本</span>
            <span>删除为划线，新增为高亮</span>
          </div>
          {selected.id === currentVersion?.id ? (
            <div className="rounded-md border p-3 text-sm text-muted-foreground">请选择一个历史版本查看差异。</div>
          ) : currentVersion ? (
            <ScrollArea className="h-32 rounded-md border">
              <div className="whitespace-pre-wrap p-3 text-sm leading-7">
                <DiffSegments segments={segments} />
              </div>
            </ScrollArea>
          ) : (
            <div className="rounded-md border p-3 text-sm text-muted-foreground">暂无当前有效版本可比较。</div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function CandidateMergeDetails({ merge }: { merge: import("../lib/api").CandidateMerge }) {
  const sourceMap = new Map(merge.sources.map((source) => [source.id, source]));
  return (
    <details className="rounded-md border bg-muted/20 text-sm" open>
      <summary className="cursor-pointer px-3 py-2 font-medium">候选合并血缘 · {merge.segments?.length ? `${merge.segments.length} 段` : `${merge.source_result_ids.length} 个来源`}</summary>
      <div className="flex flex-col gap-3 border-t p-3">
        <div className="grid gap-2 text-xs sm:grid-cols-3">
          <LabeledValue label="方式" value={merge.driver} className="font-medium" />
          <LabeledValue label="Prompt" value={merge.prompt_version || "-"} className="font-medium" />
          <LabeledValue label="哈希" value={shortID(merge.prompt_hash || "-")} className="font-medium" />
        </div>
        <div className="flex flex-col gap-2">
          {merge.source_result_ids.map((id) => {
            const source = sourceMap.get(id);
            return <div key={id} className="rounded-md border p-2 text-xs"><span className="font-medium">{source?.provider || "OCR"} · {source?.model || "model"}</span>（{shortID(id)}）<div className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap text-muted-foreground">{source?.text || "来源结果不可用"}</div></div>;
          })}
        </div>
        {merge.segments?.length ? <div className="text-xs text-muted-foreground">{merge.segments.map((segment) => `#${segment.ordinal + 1} ${shortID(segment.source_result_id)} [${segment.source_start},${segment.source_end}) → [${segment.output_start},${segment.output_end})`).join("；")}</div> : null}
        <details className="rounded-md border"><summary className="cursor-pointer px-3 py-2 text-xs font-medium">原始响应</summary><pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words border-t p-3 text-xs">{formatJSON(merge.raw_response)}</pre></details>
      </div>
    </details>
  );
}

function versionSource(version: TextVersion, versions: TextVersion[], results: RecognitionResult[]) {
  const result = results.find((item) => item.id === version.source_result_id);
  if (result) {
    const model = `${result.provider || "OCR"} · ${result.model || "model"}`;
    return { summary: model, detail: `${model}（${shortID(result.id)}）` };
  }
  const base = versions.find((item) => item.id === version.base_version_id);
  if (base) {
    const label = `${statusLabel(base.kind)}版本 ${shortID(base.id)}`;
    return { summary: label, detail: label };
  }
  return { summary: version.created_by || "直接创建", detail: version.created_by || "直接创建" };
}

function shortID(value: string) {
  return value.length > 12 ? `${value.slice(0, 12)}…` : value;
}

function RecognitionDetails({ result }: { result: RecognitionResult }) {
  const fields = [
    ["配置", result.config_json],
    ["元数据", result.metadata_json],
    ["原始响应", result.raw_json],
  ] as const;
  return (
    <details className="rounded-md border bg-muted/20 text-sm">
      <summary className="cursor-pointer px-3 py-2 font-medium">识别详情</summary>
      <div className="flex flex-col gap-3 border-t p-3">
        <div className="grid gap-2 text-xs sm:grid-cols-3">
          <LabeledValue label="时间" value={formatTime(result.created_at) || "-"} className="font-medium" />
          <LabeledValue label="Prompt 版本" value={result.prompt_version || "-"} className="font-medium" />
          <LabeledValue label="置信度" value={result.confidence == null ? "-" : result.confidence.toFixed(2)} className="font-medium" />
        </div>
        {fields.map(([label, value]) => (
          <div key={label}>
            <div className="mb-1 text-xs font-medium text-muted-foreground">{label}</div>
            <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-2 text-xs">
              {formatJSON(value)}
            </pre>
          </div>
        ))}
      </div>
    </details>
  );
}

function formatJSON(value?: string) {
  if (!value) return "-";
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
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

type TextRangeAnchor = { type: "text_range" | "text_region_link"; start: number; end: number; text: string };
type PageRegionAnchor = { type: "page_region"; x: number; y: number; width: number; height: number };
type TextRegionLinkAnchor = { type: "text_region_link"; start: number; end: number; text: string; region: Omit<PageRegionAnchor, "type"> };
type NormalizedRegion = { x: number; y: number; width: number; height: number };

function annotationTextAnchor(value: string): TextRangeAnchor | null {
  try {
    const parsed = JSON.parse(value) as Partial<TextRangeAnchor>;
    if ((parsed.type !== "text_range" && parsed.type !== "text_region_link") || typeof parsed.start !== "number" || typeof parsed.end !== "number") return null;
    return { type: parsed.type, start: parsed.start, end: parsed.end, text: typeof parsed.text === "string" ? parsed.text : "" };
  } catch {
    return null;
  }
}

function annotationPageRegion(value: string): PageRegionAnchor | null {
  try {
    const raw = JSON.parse(value) as {
      type?: string; x?: number; y?: number; width?: number; height?: number;
      region?: { x?: number; y?: number; width?: number; height?: number };
    };
    const parsed = raw.type === "text_region_link" ? raw.region : raw;
    if (
      (raw.type !== "page_region" && raw.type !== "text_region_link") ||
      !parsed ||
      typeof parsed.x !== "number" ||
      typeof parsed.y !== "number" ||
      typeof parsed.width !== "number" ||
      typeof parsed.height !== "number"
    ) return null;
    return { type: "page_region", x: parsed.x, y: parsed.y, width: parsed.width, height: parsed.height };
  } catch {
    return null;
  }
}

function normalizedPointer(event: ReactPointerEvent<HTMLDivElement>) {
  const rect = event.currentTarget.getBoundingClientRect();
  return {
    x: clamp01((event.clientX - rect.left) / rect.width),
    y: clamp01((event.clientY - rect.top) / rect.height),
  };
}

function regionFromPoints(start: { x: number; y: number }, end: { x: number; y: number }): NormalizedRegion {
  return {
    x: Math.min(start.x, end.x),
    y: Math.min(start.y, end.y),
    width: Math.abs(end.x - start.x),
    height: Math.abs(end.y - start.y),
  };
}

function clamp01(value: number) {
  return Math.max(0, Math.min(1, value));
}

function RegionOverlay({ region, className }: { region: NormalizedRegion; className: string }) {
  return (
    <div
      className={cn("pointer-events-none absolute border-2", className)}
      style={{
        left: `${region.x * 100}%`,
        top: `${region.y * 100}%`,
        width: `${region.width * 100}%`,
        height: `${region.height * 100}%`,
      }}
    />
  );
}

function focusTextRange(textarea: HTMLTextAreaElement | null, anchor: TextRangeAnchor) {
  if (!textarea) return;
  const start = Math.max(0, Math.min(anchor.start, textarea.value.length));
  const end = Math.max(start, Math.min(anchor.end, textarea.value.length));
  textarea.focus();
  textarea.setSelectionRange(start, end);
}

function pageReviewLabel(page: {
  has_final: boolean;
  has_manual: boolean;
  has_candidate: boolean;
  recognition_count: number;
}) {
  if (page.has_final) return "已定稿";
  if (page.has_manual) return "校对中";
  if (page.has_candidate || page.recognition_count > 0) return "待校对";
  return "未识别";
}

function DiffView({
  results,
  profiles,
  isMerging,
  onMerge,
  onAlignedMerge,
}: {
  results: RecognitionResult[];
  profiles: Array<{ id: string; name: string }>;
  isMerging: boolean;
  onMerge: (resultIDs: string[], profileID: string) => void;
  onAlignedMerge: (segments: AlignedSegmentInput[]) => void;
}) {
  const [baseID, setBaseID] = useState(results[0]?.id ?? "");
  const [otherID, setOtherID] = useState(results[1]?.id ?? results[0]?.id ?? "");
  const [mergeIDs, setMergeIDs] = useState<string[]>(results.slice(0, 2).map((result) => result.id));
  const [mergeProfileID, setMergeProfileID] = useState("default");
  const [alignmentMode, setAlignmentMode] = useState<"line" | "paragraph">("paragraph");
  const [rowSources, setRowSources] = useState<Array<"base" | "other">>([]);
  const resultKey = results.map((result) => result.id).join("|");
  useEffect(() => {
    setBaseID(results[0]?.id ?? "");
    setOtherID(results[1]?.id ?? results[0]?.id ?? "");
    setMergeIDs(results.slice(0, 2).map((result) => result.id));
  }, [resultKey]);
  const base = results.find((result) => result.id === baseID) ?? results[0];
  const other = results.find((result) => result.id === otherID) ?? results.find((result) => result.id !== base.id) ?? results[0];
  const segments = useMemo(() => diffChars(base.text, other.text), [base.text, other.text]);
  const alignedRows = useMemo(() => alignSourceChunks(base, other, alignmentMode), [base, other, alignmentMode]);
  useEffect(() => { setRowSources(alignedRows.map((row) => row.base ? "base" : "other")); }, [base.id, other.id, alignmentMode, alignedRows.length]);
  const selectedSegments = alignedRows.flatMap((row, index) => {
    const selected = rowSources[index] === "other" ? row.other : row.base;
    return selected ? [{ source_result_id: selected.resultID, source_start: selected.start, source_end: selected.end, text: selected.text }] : [];
  });
  return (
    <Card className="overflow-hidden">
      <CardHeader className="gap-3 border-b p-3">
        <CardTitle className="text-sm">选择两个识别结果进行比较</CardTitle>
        <div className="grid gap-2 sm:grid-cols-2">
          <ResultSelect value={base.id} results={results} onValueChange={setBaseID} label="基准结果" />
          <ResultSelect value={other.id} results={results} onValueChange={setOtherID} label="对比结果" />
        </div>
        <p className="text-xs text-muted-foreground">为避免浏览器卡顿，每个结果仅比较前 1200 个字符。</p>
        <Separator />
        <div className="flex flex-col gap-2">
          <div className="text-xs font-medium text-muted-foreground">保守合并（至少选择 2 个结果）</div>
          <ToggleGroup
            type="multiple"
            variant="outline"
            size="sm"
            value={mergeIDs}
            onValueChange={setMergeIDs}
            className="flex-wrap justify-start"
            aria-label="选择候选合并来源"
          >
            {results.map((result, index) => (
              <ToggleGroupItem key={result.id} value={result.id}>{index + 1}. {result.model || "model"}</ToggleGroupItem>
            ))}
          </ToggleGroup>
          <div className="flex flex-wrap items-end gap-2">
            <Field className="min-w-52 flex-1">
              <FieldLabel htmlFor="merge-profile">合并识别器</FieldLabel>
              <Select value={mergeProfileID} onValueChange={setMergeProfileID}>
                <SelectTrigger id="merge-profile"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">默认识别器</SelectItem>
                  {profiles.map((profile) => <SelectItem key={profile.id} value={profile.id}>{profile.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
            <PendingButton size="sm" disabled={mergeIDs.length < 2 || isMerging} onClick={() => onMerge(mergeIDs, mergeProfileID)} pending={isMerging} pendingLabel="合并中…" icon={<GitMerge />}>
              合并为候选稿
            </PendingButton>
          </div>
          <p className="text-xs text-muted-foreground">服务端只接受候选中原样出现的完整行；任何扩写或新行都会拒绝，失败不会改动现有文本。</p>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        <ScrollArea className="h-40">
          <div className="px-3 py-2 text-sm leading-7">
            <DiffSegments segments={segments} />
          </div>
        </ScrollArea>
        <Separator />
        <div className="flex flex-col gap-3 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div><div className="text-sm font-medium">逐段选择来源</div><p className="text-xs text-muted-foreground">按行或段对齐，逐项选择后组合为可追溯候选稿。</p></div>
            <ToggleGroup type="single" value={alignmentMode} onValueChange={(value) => value && setAlignmentMode(value as "line" | "paragraph")} variant="outline" size="sm">
              <ToggleGroupItem value="paragraph">按段</ToggleGroupItem><ToggleGroupItem value="line">按行</ToggleGroupItem>
            </ToggleGroup>
          </div>
          <ScrollArea className="h-64 rounded-md border">
            <div className="flex flex-col gap-2 p-2">
              {alignedRows.map((row, index) => (
                <div key={`${alignmentMode}-${index}`} className="grid gap-2 rounded-md border p-2 sm:grid-cols-2">
                  {(["base", "other"] as const).map((side) => {
                    const chunk = row[side];
                    return <Button key={side} type="button" variant={rowSources[index] === side ? "secondary" : "ghost"} className="h-auto min-h-14 justify-start whitespace-pre-wrap text-left" disabled={!chunk} onClick={() => setRowSources((current) => current.map((value, rowIndex) => rowIndex === index ? side : value))}>{chunk?.text || "（此来源无对应内容）"}</Button>;
                  })}
                </div>
              ))}
            </div>
          </ScrollArea>
          <PendingButton size="sm" disabled={!selectedSegments.length || isMerging} onClick={() => onAlignedMerge(selectedSegments)} pending={isMerging} icon={<GitMerge />}>组合并保存候选稿</PendingButton>
        </div>
      </CardContent>
    </Card>
  );
}

type AlignedSegmentInput = { source_result_id: string; source_start: number; source_end: number; text: string };
type SourceChunk = { resultID: string; start: number; end: number; text: string };

function sourceChunks(result: RecognitionResult, mode: "line" | "paragraph"): SourceChunk[] {
  const expression = mode === "line" ? /[^\n]*\n|[^\n]+$/g : /[\s\S]*?(?:\n[ \t]*\n|$)/g;
  const chunks: SourceChunk[] = [];
  for (const match of result.text.matchAll(expression)) {
    if (!match[0]) continue;
    const start = match.index ?? 0;
    chunks.push({ resultID: result.id, start, end: start + match[0].length, text: match[0] });
  }
  return chunks;
}

function alignSourceChunks(base: RecognitionResult, other: RecognitionResult, mode: "line" | "paragraph") {
  const left = sourceChunks(base, mode);
  const right = sourceChunks(other, mode);
  return Array.from({ length: Math.max(left.length, right.length) }, (_, index) => ({ base: left[index], other: right[index] }));
}

function ResultSelect({
  value,
  results,
  onValueChange,
  label,
}: {
  value: string;
  results: RecognitionResult[];
  onValueChange: (value: string) => void;
  label: string;
}) {
  return (
    <Field>
      <FieldLabel htmlFor={`diff-${label}`}>{label}</FieldLabel>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger id={`diff-${label}`}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {results.map((result, index) => (
              <SelectItem key={result.id} value={result.id}>
                {index + 1}. {result.provider || "OCR"} · {result.model || "model"}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </Field>
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

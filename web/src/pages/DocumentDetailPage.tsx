import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Ban,
  Download,
  FileText,
  FileType,
  Info,
  Layers3,
  Paperclip,
  Pencil,
  Play,
  RefreshCw,
  RotateCcw,
  RotateCw,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage, IconTooltipButton, LabeledValue, MetricCard, PageHeader } from "../components/app/chrome";
import { StatusBadge } from "../components/app/status-badge";
import { PageProcessingCard } from "../components/app/page-processing-card";
import { RecognitionExperimentsCard } from "../components/app/recognition-experiments-card";
import { CrossCheckCard } from "../components/app/cross-check-card";
import { ExportHistoryCard } from "../components/app/export-history-card";
import { TagChips, TagEditor } from "../components/app/tags";
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
import { Button } from "../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../components/ui/dialog";
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel, FieldTitle } from "../components/ui/field";
import { Input } from "../components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Switch } from "../components/ui/switch";
import { Progress } from "../components/ui/progress";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "../components/ui/tooltip";
import {
  cancelRun,
  deleteDocument,
  exportDocument,
  getDocument,
  listDocumentAssets,
  listPages,
  listPromptVersions,
  listProviderAdapters,
  listRecognizerProfiles,
  listRecognitionRuns,
  listRunPages,
  patchDocument,
  retryRun,
  startRecognition,
  ApiError,
  type Document,
  type DocumentAsset,
  type RecognitionRun,
} from "../lib/api";
import { formatBytes, formatTime } from "../lib/format";

const ACTIVE_RUN_STATUSES = new Set(["queued", "running"]);

const ASSET_ROLE_LABELS: Record<string, string> = {
  original: "原件",
  page_image: "页图",
  thumbnail: "缩略图",
  export: "导出",
};

const PRIMARY_ASSET_ROLES = new Set(["original", "export"]);

function assetRoleLabel(role: string) {
  return ASSET_ROLE_LABELS[role] ?? role;
}

function assetFileName(asset: DocumentAsset) {
  if (asset.original_name) return asset.original_name;
  const tail = asset.storage_path.split(/[\\/]/).filter(Boolean).pop();
  return tail || asset.storage_path || "--";
}

function InfoField({ label, value }: { label: string; value?: string }) {
  return <LabeledValue label={label} value={value || "--"} className="text-sm" title={value || undefined} />;
}

type DocumentForm = {
  title: string;
  author: string;
  source: string;
  description: string;
};

function DocumentInfoCard({ documentID, doc }: { documentID: string; doc?: Document }) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<DocumentForm>({ title: "", author: "", source: "", description: "" });
  const patchMutation = useMutation({
    mutationFn: (input: DocumentForm) => patchDocument(documentID, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
      queryClient.invalidateQueries({ queryKey: ["documents"] });
      setOpen(false);
      toast.success("文档信息已保存");
    },
    onError: (error: Error) => toast.error("保存失败", { description: error.message }),
  });

  function onOpenChange(next: boolean) {
    if (next && doc) {
      setForm({
        title: doc.title ?? "",
        author: doc.author ?? "",
        source: doc.source ?? "",
        description: doc.description ?? "",
      });
      patchMutation.reset();
    }
    setOpen(next);
  }

  function setField<K extends keyof DocumentForm>(key: K, value: string) {
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between pb-3">
        <CardTitle className="inline-flex items-center gap-2 text-base [&_svg]:size-4">
          <Info />
          文档信息
        </CardTitle>
        <Button variant="outline" size="sm" disabled={!doc} onClick={() => onOpenChange(true)}>
          <Pencil />
          编辑
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {doc ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <InfoField label="作者" value={doc.author} />
              <InfoField label="来源" value={doc.source} />
              <InfoField label="创建时间" value={formatTime(doc.created_at)} />
              <InfoField label="更新时间" value={formatTime(doc.updated_at)} />
            </div>
            <div className="flex flex-col gap-1">
              <div className="text-xs text-muted-foreground">描述</div>
              <div className="whitespace-pre-wrap text-sm">{doc.description || "--"}</div>
            </div>
          </>
        ) : (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-2/3" />
          </div>
        )}
      </CardContent>

      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑文档信息</DialogTitle>
            <DialogDescription>修改标题、作者、来源与描述。</DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              patchMutation.mutate(form);
            }}
          >
            <FieldGroup className="gap-4">
              <Field>
                <FieldLabel htmlFor="doc-title">标题</FieldLabel>
                <Input
                  id="doc-title"
                  required
                  value={form.title}
                  onChange={(event) => setField("title", event.target.value)}
                />
              </Field>
              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="doc-author">作者</FieldLabel>
                  <Input id="doc-author" value={form.author} onChange={(event) => setField("author", event.target.value)} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="doc-source">来源</FieldLabel>
                  <Input id="doc-source" value={form.source} onChange={(event) => setField("source", event.target.value)} />
                </Field>
              </FieldGroup>
              <Field>
                <FieldLabel htmlFor="doc-description">描述</FieldLabel>
                <Textarea
                  id="doc-description"
                  rows={3}
                  value={form.description}
                  onChange={(event) => setField("description", event.target.value)}
                />
              </Field>
            </FieldGroup>
            <ErrorMessage message={patchMutation.error?.message} />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={patchMutation.isPending || !form.title.trim()}>
                {patchMutation.isPending ? <Spinner /> : <Pencil />}
                {patchMutation.isPending ? "保存中" : "保存"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

function DocumentAssetsCard({ documentID }: { documentID: string }) {
  const [showAll, setShowAll] = useState(false);
  const assets = useQuery({
    queryKey: ["assets", documentID],
    queryFn: () => listDocumentAssets(documentID),
    enabled: !!documentID,
  });

  const allAssets = assets.data ?? [];
  const hiddenCount = allAssets.filter((asset) => !PRIMARY_ASSET_ROLES.has(asset.role)).length;
  const items = showAll ? allAssets : allAssets.filter((asset) => PRIMARY_ASSET_ROLES.has(asset.role));

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between pb-3">
        <CardTitle className="inline-flex items-center gap-2 text-base [&_svg]:size-4">
          <Paperclip />
          文件
        </CardTitle>
        {hiddenCount > 0 ? (
          <Button variant="ghost" size="sm" onClick={() => setShowAll((prev) => !prev)}>
            {showAll ? "仅原件与导出" : `显示全部 (${allAssets.length})`}
          </Button>
        ) : null}
      </CardHeader>
      <CardContent className="p-0">
        {assets.isError ? (
          <EmptyState
            icon={<Paperclip />}
            title="文件加载失败"
            description={assets.error.message}
            className="min-h-40"
          >
            <Button variant="outline" size="sm" onClick={() => void assets.refetch()} disabled={assets.isFetching}>
              {assets.isFetching ? <Spinner /> : <RefreshCw />}
              重试
            </Button>
          </EmptyState>
        ) : items.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-20">角色</TableHead>
                <TableHead>文件名</TableHead>
                <TableHead className="hidden w-32 md:table-cell">类型</TableHead>
                <TableHead className="w-20">大小</TableHead>
                <TableHead className="hidden w-28 lg:table-cell">时间</TableHead>
                <TableHead className="w-12" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((asset) => (
                <TableRow key={asset.id}>
                  <TableCell className="text-muted-foreground">{assetRoleLabel(asset.role)}</TableCell>
                  <TableCell className="min-w-0 max-w-48">
                    <div className="truncate font-medium" title={assetFileName(asset)}>
                      {assetFileName(asset)}
                    </div>
                  </TableCell>
                  <TableCell className="hidden truncate text-muted-foreground md:table-cell">
                    {asset.mime_type || "--"}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatBytes(asset.byte_size)}</TableCell>
                  <TableCell className="hidden text-muted-foreground lg:table-cell">{formatTime(asset.created_at) || "--"}</TableCell>
                  <TableCell className="text-right">
                    <IconTooltipButton label="下载" variant="ghost" size="icon-sm" asChild>
                      <a href={asset.download_url} download>
                        <Download />
                      </a>
                    </IconTooltipButton>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState
            icon={<Paperclip />}
            title={assets.isLoading ? "加载中" : hiddenCount > 0 ? "暂无原件或导出文件" : "暂无文件"}
            description={
              assets.isLoading
                ? undefined
                : hiddenCount > 0
                  ? "可切换「显示全部」查看页图与缩略图。"
                  : "导入或导出后会在这里列出文件。"
            }
            className="min-h-32 py-6"
          />
        )}
      </CardContent>
    </Card>
  );
}

function FailedPagesList({ runID }: { runID: string }) {
  const pages = useQuery({
    queryKey: ["run-pages", runID],
    queryFn: () => listRunPages(runID),
    enabled: !!runID,
  });
  const failed = (pages.data ?? []).filter((page) => page.status === "failed" || page.status === "canceled");

  if (pages.isLoading) {
    return <div className="text-sm text-muted-foreground">加载失败页…</div>;
  }
  if (pages.error) {
    return (
      <div className="flex items-center gap-2 text-sm text-destructive">
        <span>无法加载失败页详情：{pages.error.message}</span>
        <Button size="sm" variant="ghost" onClick={() => void pages.refetch()}>
          重试
        </Button>
      </div>
    );
  }
  if (!failed.length) {
    return null;
  }
  return (
    <div className="flex flex-col gap-1.5">
      <div className="text-xs font-medium text-muted-foreground">失败页面({failed.length})</div>
      <ul className="flex flex-col gap-1">
        {failed.map((page) => (
          <li key={page.page_id} className="flex items-start gap-2 text-sm">
            <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs tabular-nums text-muted-foreground">
              第 {page.page_no} 页
            </span>
            {page.error ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="min-w-0 flex-1 cursor-help truncate text-destructive">{page.error}</span>
                </TooltipTrigger>
                <TooltipContent side="bottom" align="start" className="max-w-sm whitespace-pre-wrap break-words">
                  {page.error}
                </TooltipContent>
              </Tooltip>
            ) : (
              <span className="min-w-0 flex-1 text-muted-foreground">{page.status === "canceled" ? "已取消" : "识别失败"}</span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function RecognitionSection({
  documentID,
  runs,
  isLoading,
  error,
  onRetry,
}: {
  documentID: string;
  runs: RecognitionRun[];
  isLoading: boolean;
  error?: Error | null;
  onRetry: () => void;
}) {
  const queryClient = useQueryClient();
  const latest = runs[0];
  const latestActive = latest ? ACTIVE_RUN_STATUSES.has(latest.status) : false;
  const latestHasFailures = latest ? ["partial", "failed", "canceled"].includes(latest.status) && latest.failed_pages > 0 : false;
  const progressValue = latest && latest.total_pages > 0 ? Math.round((latest.done_pages / latest.total_pages) * 100) : 0;

  const cancel = useMutation({
    mutationFn: (runID: string) => cancelRun(runID),
    onSuccess: () => {
      toast("正在取消识别任务");
      queryClient.invalidateQueries({ queryKey: ["runs", documentID] });
    },
    onError: (error: Error) => toast.error("取消失败", { description: error.message }),
  });
  const retry = useMutation({
    mutationFn: (runID: string) => retryRun(runID),
    onSuccess: () => {
      toast.success("已开始重试失败页");
      queryClient.invalidateQueries({ queryKey: ["runs", documentID] });
      queryClient.invalidateQueries({ queryKey: ["pages", documentID] });
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
    },
    onError: (error: Error) => toast.error("无法重试", { description: error.message }),
  });

  if (error) {
    return (
      <Card>
        <EmptyState title="识别记录加载失败" description={error.message} className="min-h-40">
          <Button variant="outline" size="sm" onClick={onRetry}>
            <RefreshCw />
            重试
          </Button>
        </EmptyState>
      </Card>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {latest ? (
        <Card>
          <CardHeader className="flex-row items-center justify-between gap-3 pb-3">
            <CardTitle className="inline-flex min-w-0 items-center gap-2 text-base">
              <span className="truncate">最新识别</span>
              <StatusBadge value={latest.status} />
            </CardTitle>
            <div className="flex shrink-0 items-center gap-2">
              {latestActive ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={cancel.isPending}
                  onClick={() => cancel.mutate(latest.id)}
                >
                  {cancel.isPending ? <Spinner /> : <Ban />}
                  取消
                </Button>
              ) : null}
              {latestHasFailures ? (
                <Button variant="secondary" size="sm" disabled={retry.isPending} onClick={() => retry.mutate(latest.id)}>
                  {retry.isPending ? <Spinner /> : <RotateCcw />}
                  重试失败页
                </Button>
              ) : null}
            </div>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-center gap-3">
              <Progress value={progressValue} className="flex-1" />
              <span className="w-20 text-right text-xs tabular-nums text-muted-foreground">
                {latest.done_pages}/{latest.total_pages} 页
              </span>
            </div>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <span>{latest.provider} · {latest.model || "未配置模型"}</span>
              {latest.failed_pages > 0 ? <span className="text-destructive">失败 {latest.failed_pages} 页</span> : null}
              <span>{formatTime(latest.created_at)}</span>
            </div>
            <ErrorMessage message={latest.error} />
            {latestHasFailures ? <FailedPagesList runID={latest.id} /> : null}
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">识别运行</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>模型</TableHead>
                <TableHead className="w-28">状态</TableHead>
                <TableHead className="hidden w-24 sm:table-cell">进度</TableHead>
                <TableHead className="hidden w-40 md:table-cell">创建</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.length ? (
                runs.map((run) => (
                  <TableRow key={run.id}>
                    <TableCell className="min-w-0">
                      <div className="truncate font-medium">
                        {run.provider} · {run.model || "未配置模型"}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground sm:hidden">
                        {run.done_pages}/{run.total_pages} 页 · {formatTime(run.created_at)}
                      </div>
                      {run.error ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="mt-1 max-w-md cursor-help truncate text-xs text-destructive">{run.error}</div>
                          </TooltipTrigger>
                          <TooltipContent side="bottom" align="start" className="max-w-sm whitespace-pre-wrap break-words">
                            {run.error}
                          </TooltipContent>
                        </Tooltip>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      <StatusBadge value={run.status} />
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground sm:table-cell">
                      <span className="tabular-nums">{run.done_pages}/{run.total_pages}</span>
                      {run.failed_pages > 0 ? <span className="ml-1 text-destructive">(-{run.failed_pages})</span> : null}
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground md:table-cell">{formatTime(run.created_at)}</TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={4}>
                    <EmptyState title={isLoading ? "加载中" : "暂无运行记录"} className="min-h-32" />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}

export function DocumentDetailPage() {
  const { documentID = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [exportOpen, setExportOpen] = useState(false);
  const [exportFormat, setExportFormat] = useState("md");
  const [includePageNumbers, setIncludePageNumbers] = useState(true);
  const [exportTextScope, setExportTextScope] = useState<"current" | "final">("current");
  const [includeAnnotations, setIncludeAnnotations] = useState(false);
  const [includeUncertain, setIncludeUncertain] = useState(false);
  const [recognitionImageSource, setRecognitionImageSource] = useState<"original" | "enhanced">("original");
  const [recognitionProviderSource, setRecognitionProviderSource] = useState("default");
  const [recognitionPromptID, setRecognitionPromptID] = useState("active");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const doc = useQuery({ queryKey: ["document", documentID], queryFn: () => getDocument(documentID), enabled: !!documentID });
  const pages = useQuery({
    queryKey: ["pages", documentID],
    queryFn: () => listPages(documentID),
    enabled: !!documentID,
    refetchInterval: 2500,
  });
  const recognizerProfiles = useQuery({ queryKey: ["recognizer-profiles"], queryFn: listRecognizerProfiles });
  const providerAdapters = useQuery({ queryKey: ["provider-adapters"], queryFn: listProviderAdapters });
  const promptVersions = useQuery({ queryKey: ["prompt-versions"], queryFn: listPromptVersions });
  const runs = useQuery({
    queryKey: ["runs", documentID],
    queryFn: () => listRecognitionRuns(documentID),
    enabled: !!documentID,
    refetchInterval: (query) => {
      const latest = query.state.data?.[0];
      return latest && ACTIVE_RUN_STATUSES.has(latest.status) ? 1500 : 4000;
    },
  });
  const recognition = useMutation({
    mutationFn: () => startRecognition(documentID, {
      image_source: recognitionImageSource,
      recognizer_profile_id: recognitionProviderSource.startsWith("profile:")
        ? recognitionProviderSource.slice("profile:".length)
        : undefined,
      provider_adapter_id: recognitionProviderSource.startsWith("adapter:")
        ? recognitionProviderSource.slice("adapter:".length)
        : undefined,
      prompt_version_id: recognitionPromptID === "active" ? undefined : recognitionPromptID,
    }),
    onSuccess: () => {
      toast.success("已开始识别");
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
      queryClient.invalidateQueries({ queryKey: ["runs", documentID] });
    },
    onError: (error: Error) => {
      if (error instanceof ApiError && error.status === 409) {
        toast.error("已有识别任务进行中");
        return;
      }
      toast.error("启动识别失败", { description: error.message });
    },
  });
  const exportMutation = useMutation({
    mutationFn: () => exportDocument(documentID, {
      format: exportFormat,
      include_page_numbers: includePageNumbers,
      text_scope: exportTextScope,
      include_annotations: includeAnnotations,
      include_uncertain: includeUncertain,
    }),
    onSuccess: (file) => {
      setExportOpen(false);
      window.location.href = file.download_url;
      queryClient.invalidateQueries({ queryKey: ["assets", documentID] });
      queryClient.invalidateQueries({ queryKey: ["document-exports", documentID] });
      toast.success("导出文件已生成");
    },
    onError: (error: Error) => toast.error("导出失败", { description: error.message }),
  });
  const deleteMutation = useMutation({
    mutationFn: () => deleteDocument(documentID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["documents"] });
      toast.success("文档已删除");
      navigate("/");
    },
    onError: (error: Error) => toast.error("删除失败", { description: error.message }),
  });

  const firstPageID = pages.data?.[0]?.page_id;
  const pageItems = pages.data ?? [];
  const runItems = runs.data ?? [];
  const hasActiveRun = runItems.some((run) => ACTIVE_RUN_STATUSES.has(run.status));
  const recognizedPages = pageItems.filter((page) => page.recognition_count > 0).length;
  const verifiedPages = pageItems.filter((page) => page.has_final).length;

  if (doc.isError) {
    return (
      <div className="flex flex-col gap-5">
        <PageHeader title="文档加载失败" description="无法获取文档信息，请检查连接后重试。" />
        <Card>
          <EmptyState title="无法打开文档" description={doc.error.message}>
            <div className="flex flex-wrap justify-center gap-2">
              <Button variant="outline" onClick={() => navigate("/")}>返回文档列表</Button>
              <Button onClick={() => void doc.refetch()} disabled={doc.isFetching}>
                {doc.isFetching ? <Spinner /> : <RefreshCw />}
                重试
              </Button>
            </div>
          </EmptyState>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-2">
        <PageHeader
          title={
            <span className="inline-flex min-w-0 items-center gap-2">
              <span className="truncate">{doc.data?.title ?? "文档"}</span>
              {doc.data ? <StatusBadge value={doc.data.status} /> : null}
            </span>
          }
          description={`${doc.data?.page_count ?? 0} 页 · ${formatTime(doc.data?.updated_at) || "--"}`}
        >
          <IconTooltipButton label="刷新" variant="outline" size="icon" onClick={() => void pages.refetch()} disabled={pages.isFetching}>
            {pages.isFetching ? <Spinner /> : <RefreshCw />}
          </IconTooltipButton>
          <Button onClick={() => recognition.mutate()} disabled={recognition.isPending || !pageItems.length || hasActiveRun}>
            {recognition.isPending ? <Spinner /> : <Play />}
            {recognition.isPending ? "排队中" : hasActiveRun ? "识别中" : "识别"}
          </Button>
          <Select value={recognitionImageSource} onValueChange={(value) => setRecognitionImageSource(value as "original" | "enhanced")}>
            <SelectTrigger className="w-32" aria-label="识别图像来源"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="original">使用原图</SelectItem>
              <SelectItem value="enhanced">使用增强图</SelectItem>
            </SelectContent>
          </Select>
          <Select value={recognitionProviderSource} onValueChange={setRecognitionProviderSource}>
            <SelectTrigger className="w-44" aria-label="识别来源"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="default">默认模型</SelectItem>
              </SelectGroup>
              <SelectGroup>
                {recognizerProfiles.data?.map((profile) => (
                  <SelectItem key={profile.id} value={`profile:${profile.id}`}>
                    {profile.provider_name ? `${profile.provider_name} · ` : ""}{profile.name}
                  </SelectItem>
                ))}
              </SelectGroup>
              <SelectGroup>
                {providerAdapters.data?.filter((adapter) => adapter.is_enabled).map((adapter) => (
                  <SelectItem key={adapter.id} value={`adapter:${adapter.id}`}>HTTP · {adapter.name}</SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select value={recognitionPromptID} onValueChange={setRecognitionPromptID}>
            <SelectTrigger className="w-40" aria-label="Prompt 版本"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="active">模型默认 / 激活版本</SelectItem>
              {promptVersions.data?.map((prompt) => (
                <SelectItem key={prompt.id} value={prompt.id}>{prompt.version}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="secondary" disabled={!firstPageID} onClick={() => navigate(`/review/${documentID}/${firstPageID}`)}>
            <FileText />
            校对
          </Button>
          <Button variant="secondary" disabled={exportMutation.isPending} onClick={() => setExportOpen(true)}>
            {exportMutation.isPending ? <Spinner /> : <Download />}
            {exportMutation.isPending ? "导出中" : "导出"}
          </Button>
          <IconTooltipButton label="删除文档" variant="outline" size="icon" onClick={() => setDeleteOpen(true)}>
            <Trash2 />
          </IconTooltipButton>
        </PageHeader>
        <div className="flex flex-wrap items-center gap-2">
          {doc.data ? <TagEditor documentID={documentID} tags={doc.data.tags ?? []} /> : null}
          <TagChips tags={doc.data?.tags} />
        </div>
      </div>

      <section className="grid gap-3 md:grid-cols-4">
        <MetricCard icon={<Layers3 />} label="页面" value={pageItems.length || doc.data?.page_count || 0} />
        <MetricCard icon={<FileType />} label="已识别" value={recognizedPages} />
        <MetricCard icon={<FileText />} label="已定稿" value={verifiedPages} />
        <MetricCard icon={<RotateCw />} label="运行" value={runItems.length} hint={runItems[0] ? formatTime(runItems[0].created_at) : "暂无"} />
      </section>

      <section className="grid items-start gap-3 xl:grid-cols-2">
        <DocumentInfoCard documentID={documentID} doc={doc.data} />
        <DocumentAssetsCard documentID={documentID} />
      </section>

      <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {pages.isError ? (
          <Card className="sm:col-span-2 xl:col-span-4">
            <EmptyState title="页面加载失败" description={pages.error.message}>
              <Button variant="outline" size="sm" onClick={() => void pages.refetch()} disabled={pages.isFetching}>
                {pages.isFetching ? <Spinner /> : <RefreshCw />}
                重试
              </Button>
            </EmptyState>
          </Card>
        ) : pageItems.map((page) => (
          <Link
            key={page.page_id}
            to={`/review/${documentID}/${page.page_id}`}
            className="group overflow-hidden rounded-lg border bg-card text-card-foreground shadow-sm transition-all hover:border-primary/50 hover:shadow-md"
          >
            <div className="relative aspect-[4/3] border-b bg-muted/60">
              <img
                src={page.thumbnail_url}
                alt={`第 ${page.page_no} 页`}
                loading="lazy"
                className="size-full object-contain transition-opacity group-hover:opacity-90"
              />
              <span className="absolute left-2 top-2 rounded-md border bg-background/90 px-1.5 py-0.5 text-xs font-medium shadow-sm">
                {page.page_no}
              </span>
            </div>
            <div className="flex flex-col gap-3 p-3 transition-colors group-hover:bg-accent/40">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium group-hover:text-primary">第 {page.page_no} 页</span>
                <StatusBadge value={page.page_status} />
              </div>
              <div className="grid grid-cols-3 gap-2 text-xs text-muted-foreground">
                <span>识别 {page.recognition_count}</span>
                <span>{page.has_manual ? "人工稿" : "未校对"}</span>
                <span>{page.has_final ? "定稿" : "未定稿"}</span>
              </div>
            </div>
          </Link>
        ))}
        {!pages.isError && !pageItems.length ? (
          <Card className="sm:col-span-2 xl:col-span-4">
            <EmptyState
              icon={<FileText />}
              title={pages.isLoading ? "加载中" : "暂无页面"}
              description={pages.isLoading ? undefined : "页面拆分完成后会显示缩略图。"}
            />
          </Card>
        ) : null}
      </section>

      <PageProcessingCard documentID={documentID} pages={pageItems} />

      <RecognitionExperimentsCard documentID={documentID} pages={pageItems} />

      <CrossCheckCard documentID={documentID} pages={pageItems} />

      <ExportHistoryCard scope="document" targetID={documentID} />

      <RecognitionSection
        documentID={documentID}
        runs={runItems}
        isLoading={runs.isLoading}
        error={runs.isError ? runs.error : null}
        onRetry={() => void runs.refetch()}
      />

      <Dialog open={exportOpen} onOpenChange={(open) => !exportMutation.isPending && setExportOpen(open)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>导出文档</DialogTitle>
            <DialogDescription>选择文件格式、文本版本以及需要随文保留的审校信息。</DialogDescription>
          </DialogHeader>
          <FieldGroup className="gap-4">
            <Field>
              <FieldLabel htmlFor="export-format">格式</FieldLabel>
              <Select value={exportFormat} onValueChange={setExportFormat}>
                <SelectTrigger id="export-format"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="md">Markdown</SelectItem>
                    <SelectItem value="txt">纯文本</SelectItem>
                    <SelectItem value="docx">Word 文档（DOCX）</SelectItem>
                    <SelectItem value="pdf">PDF 审校版</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>{exportFormat === "pdf" ? "PDF 审校版会同时排入原始页图、转录文本和所选审校记录。" : "所有格式都使用同一份导出选项快照。"}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="export-text-scope">文本版本</FieldLabel>
              <Select value={exportTextScope} onValueChange={(value) => setExportTextScope(value as "current" | "final")}>
                <SelectTrigger id="export-text-scope"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="current">当前稿</SelectItem>
                    <SelectItem value="final">仅最终定稿</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>{exportTextScope === "final" ? "只导出已经确认定稿的页面；尚未定稿的页面会跳过。" : "逐页导出当前校对界面正在使用的最新有效文本。"}</FieldDescription>
            </Field>
            <Field orientation="horizontal" className="rounded-md border p-3">
              <FieldLabel htmlFor="include-page-numbers">保留页码</FieldLabel>
              <Switch id="include-page-numbers" checked={includePageNumbers} onCheckedChange={setIncludePageNumbers} />
            </Field>
            <Field orientation="horizontal" className="rounded-md border p-3">
              <FieldContent>
                <FieldLabel htmlFor="include-annotations">
                  <FieldTitle>包含批注</FieldTitle>
                </FieldLabel>
                <FieldDescription>附带页级批注和区域批注。</FieldDescription>
              </FieldContent>
              <Switch id="include-annotations" checked={includeAnnotations} onCheckedChange={setIncludeAnnotations} />
            </Field>
            <Field orientation="horizontal" className="rounded-md border p-3">
              <FieldContent>
                <FieldLabel htmlFor="include-uncertain">
                  <FieldTitle>保留存疑标记</FieldTitle>
                </FieldLabel>
                <FieldDescription>在正文中标出仍待处理的存疑字词。</FieldDescription>
              </FieldContent>
              <Switch id="include-uncertain" checked={includeUncertain} onCheckedChange={setIncludeUncertain} />
            </Field>
            <ErrorMessage message={exportMutation.error?.message} />
          </FieldGroup>
          <DialogFooter>
            <Button variant="outline" onClick={() => setExportOpen(false)} disabled={exportMutation.isPending}>取消</Button>
            <Button onClick={() => exportMutation.mutate()} disabled={exportMutation.isPending}>
              {exportMutation.isPending ? <Spinner /> : <Download />}
              {exportMutation.isPending ? "导出中" : "开始导出"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteOpen} onOpenChange={(open) => !deleteMutation.isPending && setDeleteOpen(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除文档？</AlertDialogTitle>
            <AlertDialogDescription>
              确定删除“{doc.data?.title || "当前文档"}”吗？页面、识别结果和文本版本将一并删除，此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ErrorMessage message={deleteMutation.error?.message} />
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => deleteMutation.mutate()} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <Spinner /> : <Trash2 />}
              {deleteMutation.isPending ? "删除中" : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

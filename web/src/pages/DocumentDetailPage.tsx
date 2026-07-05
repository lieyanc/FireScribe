import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Ban,
  ChevronDown,
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
} from "lucide-react";
import { EmptyState, ErrorMessage, IconTooltipButton, MetricCard, PageHeader } from "../components/app/chrome";
import { TagChips, TagEditor } from "../components/app/tags";
import { Badge } from "../components/ui/badge";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../components/ui/dropdown-menu";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Progress } from "../components/ui/progress";
import { Skeleton } from "../components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "../components/ui/tooltip";
import { toast } from "../components/ui/toaster";
import {
  cancelRun,
  exportDocument,
  getDocument,
  listDocumentAssets,
  listPages,
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
import { cn, formatBytes, formatTime } from "../lib/utils";

const ACTIVE_RUN_STATUSES = new Set(["queued", "running"]);

const RUN_STATUS_META: Record<string, { label: string; tone: string }> = {
  queued: { label: "排队中", tone: "border-accent bg-accent text-accent-foreground" },
  running: { label: "识别中", tone: "border-accent bg-accent text-accent-foreground" },
  succeeded: { label: "已完成", tone: "border-primary/25 bg-primary/10 text-primary" },
  partial: { label: "部分失败", tone: "border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400" },
  failed: { label: "失败", tone: "border-destructive/20 bg-destructive/10 text-destructive" },
  canceled: { label: "已取消", tone: "border-destructive/20 bg-destructive/10 text-destructive" },
};

function RunStatusBadge({ status }: { status: string }) {
  const meta = RUN_STATUS_META[status] ?? { label: status, tone: "border-transparent bg-secondary text-secondary-foreground" };
  return (
    <span className={cn("inline-flex h-6 items-center rounded-md border px-2.5 text-xs font-semibold", meta.tone)}>
      {meta.label}
    </span>
  );
}

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
  return (
    <div className="min-w-0 space-y-1">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="truncate text-sm" title={value || undefined}>
        {value || "--"}
      </div>
    </div>
  );
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
    },
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
      <CardHeader className="flex-row items-center justify-between space-y-0 pb-3">
        <CardTitle className="inline-flex items-center gap-2 text-base">
          <Info className="size-4 text-muted-foreground" />
          文档信息
        </CardTitle>
        <Button variant="outline" size="sm" disabled={!doc} onClick={() => onOpenChange(true)}>
          <Pencil className="size-4" />
          编辑
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        {doc ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <InfoField label="作者" value={doc.author} />
              <InfoField label="来源" value={doc.source} />
              <InfoField label="创建时间" value={formatTime(doc.created_at)} />
              <InfoField label="更新时间" value={formatTime(doc.updated_at)} />
            </div>
            <div className="space-y-1">
              <div className="text-xs text-muted-foreground">描述</div>
              <div className="whitespace-pre-wrap text-sm">{doc.description || "--"}</div>
            </div>
          </>
        ) : (
          <div className="space-y-3">
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
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              patchMutation.mutate(form);
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor="doc-title">标题</Label>
              <Input id="doc-title" value={form.title} onChange={(event) => setField("title", event.target.value)} />
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="doc-author">作者</Label>
                <Input id="doc-author" value={form.author} onChange={(event) => setField("author", event.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="doc-source">来源</Label>
                <Input id="doc-source" value={form.source} onChange={(event) => setField("source", event.target.value)} />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="doc-description">描述</Label>
              <Textarea
                id="doc-description"
                rows={3}
                value={form.description}
                onChange={(event) => setField("description", event.target.value)}
              />
            </div>
            <ErrorMessage message={patchMutation.error?.message} />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={patchMutation.isPending || !form.title.trim()}>
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
      <CardHeader className="flex-row items-center justify-between space-y-0 pb-3">
        <CardTitle className="inline-flex items-center gap-2 text-base">
          <Paperclip className="size-4 text-muted-foreground" />
          文件
        </CardTitle>
        {hiddenCount > 0 ? (
          <Button variant="ghost" size="sm" onClick={() => setShowAll((prev) => !prev)}>
            {showAll ? "仅原件与导出" : `显示全部 (${allAssets.length})`}
          </Button>
        ) : null}
      </CardHeader>
      <CardContent className="p-0">
        <ErrorMessage message={assets.error?.message} />
        {items.length ? (
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
                        <Download className="size-4" />
                      </a>
                    </IconTooltipButton>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState
            icon={<Paperclip className="size-5" />}
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
    <div className="space-y-1.5">
      <div className="text-xs font-medium text-muted-foreground">失败页面({failed.length})</div>
      <ul className="space-y-1">
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
}: {
  documentID: string;
  runs: RecognitionRun[];
  isLoading: boolean;
}) {
  const queryClient = useQueryClient();
  const latest = runs[0];
  const latestActive = latest ? ACTIVE_RUN_STATUSES.has(latest.status) : false;
  const latestHasFailures = latest ? ["partial", "failed", "canceled"].includes(latest.status) && latest.failed_pages > 0 : false;
  const progressValue = latest && latest.total_pages > 0 ? Math.round((latest.done_pages / latest.total_pages) * 100) : 0;

  const cancel = useMutation({
    mutationFn: (runID: string) => cancelRun(runID),
    onSuccess: () => {
      toast({ title: "正在取消识别任务" });
      queryClient.invalidateQueries({ queryKey: ["runs", documentID] });
    },
    onError: (error: Error) => toast({ title: "取消失败", description: error.message, variant: "error" }),
  });
  const retry = useMutation({
    mutationFn: (runID: string) => retryRun(runID),
    onSuccess: () => {
      toast({ title: "已开始重试失败页", variant: "success" });
      queryClient.invalidateQueries({ queryKey: ["runs", documentID] });
      queryClient.invalidateQueries({ queryKey: ["pages", documentID] });
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
    },
    onError: (error: Error) => toast({ title: "无法重试", description: error.message, variant: "error" }),
  });

  return (
    <div className="space-y-3">
      {latest ? (
        <Card>
          <CardHeader className="flex-row items-center justify-between gap-3 space-y-0 pb-3">
            <CardTitle className="inline-flex min-w-0 items-center gap-2 text-base">
              <span className="truncate">最新识别</span>
              <RunStatusBadge status={latest.status} />
            </CardTitle>
            <div className="flex shrink-0 items-center gap-2">
              {latestActive ? (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={cancel.isPending}
                  onClick={() => cancel.mutate(latest.id)}
                >
                  <Ban className="size-4" />
                  取消
                </Button>
              ) : null}
              {latestHasFailures ? (
                <Button variant="secondary" size="sm" disabled={retry.isPending} onClick={() => retry.mutate(latest.id)}>
                  <RotateCcw className="size-4" />
                  重试失败页
                </Button>
              ) : null}
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
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
            {latest.error ? (
              <div className="rounded-md border border-destructive/20 bg-destructive/5 px-3 py-2 text-sm text-destructive">
                {latest.error}
              </div>
            ) : null}
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
                      <RunStatusBadge status={run.status} />
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
  const doc = useQuery({ queryKey: ["document", documentID], queryFn: () => getDocument(documentID), enabled: !!documentID });
  const pages = useQuery({
    queryKey: ["pages", documentID],
    queryFn: () => listPages(documentID),
    enabled: !!documentID,
    refetchInterval: 2500,
  });
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
    mutationFn: () => startRecognition(documentID),
    onSuccess: () => {
      toast({ title: "已开始识别", variant: "success" });
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
      queryClient.invalidateQueries({ queryKey: ["runs", documentID] });
    },
    onError: (error: Error) => {
      if (error instanceof ApiError && error.status === 409) {
        toast({ title: "已有识别任务进行中", variant: "error" });
        return;
      }
      toast({ title: "启动识别失败", description: error.message, variant: "error" });
    },
  });
  const exportMutation = useMutation({
    mutationFn: (format: string) => exportDocument(documentID, { format, include_page_numbers: true }),
    onSuccess: (file) => {
      window.location.href = file.download_url;
      queryClient.invalidateQueries({ queryKey: ["assets", documentID] });
    },
  });

  const firstPageID = pages.data?.[0]?.page_id;
  const pageItems = pages.data ?? [];
  const runItems = runs.data ?? [];
  const hasActiveRun = runItems.some((run) => ACTIVE_RUN_STATUSES.has(run.status));
  const recognizedPages = pageItems.filter((page) => page.recognition_count > 0).length;
  const verifiedPages = pageItems.filter((page) => page.has_final).length;

  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <PageHeader
          title={
            <span className="inline-flex min-w-0 items-center gap-2">
              <span className="truncate">{doc.data?.title ?? "文档"}</span>
              {doc.data ? <Badge value={doc.data.status} /> : null}
            </span>
          }
          description={`${doc.data?.page_count ?? 0} 页 · ${formatTime(doc.data?.updated_at) || "--"}`}
        >
          <IconTooltipButton label="刷新" variant="outline" size="icon" onClick={() => pages.refetch()}>
            <RefreshCw className="size-4" />
          </IconTooltipButton>
          <Button onClick={() => recognition.mutate()} disabled={recognition.isPending || !pageItems.length || hasActiveRun}>
            <Play className="size-4" />
            {recognition.isPending ? "排队中" : hasActiveRun ? "识别中" : "识别"}
          </Button>
          <Button variant="secondary" disabled={!firstPageID} onClick={() => navigate(`/review/${documentID}/${firstPageID}`)}>
            <FileText className="size-4" />
            校对
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="secondary" disabled={exportMutation.isPending}>
                <Download className="size-4" />
                {exportMutation.isPending ? "导出中" : "导出"}
                <ChevronDown className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => exportMutation.mutate("md")}>
                <FileType className="size-4" />
                Markdown
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => exportMutation.mutate("txt")}>
                <FileText className="size-4" />
                纯文本
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </PageHeader>
        <div className="flex flex-wrap items-center gap-2">
          {doc.data ? <TagEditor documentID={documentID} tags={doc.data.tags ?? []} /> : null}
          <TagChips tags={doc.data?.tags} />
        </div>
      </div>

      <section className="grid gap-3 md:grid-cols-4">
        <MetricCard icon={<Layers3 className="size-4" />} label="页面" value={pageItems.length || doc.data?.page_count || 0} />
        <MetricCard icon={<FileType className="size-4" />} label="已识别" value={recognizedPages} />
        <MetricCard icon={<FileText className="size-4" />} label="已定稿" value={verifiedPages} />
        <MetricCard icon={<RotateCw className="size-4" />} label="运行" value={runItems.length} hint={runItems[0] ? formatTime(runItems[0].created_at) : "暂无"} />
      </section>

      <ErrorMessage message={exportMutation.error?.message} />

      <section className="grid items-start gap-3 xl:grid-cols-2">
        <DocumentInfoCard documentID={documentID} doc={doc.data} />
        <DocumentAssetsCard documentID={documentID} />
      </section>

      <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {pageItems.map((page) => (
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
            <div className="space-y-3 p-3 transition-colors group-hover:bg-accent/40">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium group-hover:text-primary">第 {page.page_no} 页</span>
                <Badge value={page.page_status} />
              </div>
              <div className="grid grid-cols-3 gap-2 text-xs text-muted-foreground">
                <span>识别 {page.recognition_count}</span>
                <span>{page.has_manual ? "人工稿" : "未校对"}</span>
                <span>{page.has_final ? "定稿" : "未定稿"}</span>
              </div>
            </div>
          </Link>
        ))}
        {!pageItems.length ? (
          <Card className="sm:col-span-2 xl:col-span-4">
            <EmptyState
              icon={<FileText className="size-5" />}
              title={pages.isLoading ? "加载中" : "暂无页面"}
              description={pages.isLoading ? undefined : "页面拆分完成后会显示缩略图。"}
            />
          </Card>
        ) : null}
      </section>

      <RecognitionSection documentID={documentID} runs={runItems} isLoading={runs.isLoading} />
    </div>
  );
}

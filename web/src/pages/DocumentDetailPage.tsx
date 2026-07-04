import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
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
import { Skeleton } from "../components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import {
  exportDocument,
  getDocument,
  listDocumentAssets,
  listPages,
  listRecognitionRuns,
  patchDocument,
  startRecognition,
  type Document,
  type DocumentAsset,
} from "../lib/api";
import { formatBytes, formatTime } from "../lib/utils";

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
    refetchInterval: 2500,
  });
  const recognition = useMutation({
    mutationFn: () => startRecognition(documentID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
      queryClient.invalidateQueries({ queryKey: ["runs", documentID] });
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
          <Button onClick={() => recognition.mutate()} disabled={recognition.isPending || !pageItems.length}>
            <Play className="size-4" />
            {recognition.isPending ? "排队中" : "识别"}
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

      <ErrorMessage message={recognition.error?.message || exportMutation.error?.message} />

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
                <TableHead className="hidden w-40 sm:table-cell">创建</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runItems.length ? (
                runItems.map((run) => (
                  <TableRow key={run.id}>
                    <TableCell className="min-w-0">
                      <div className="truncate font-medium">
                        {run.provider} · {run.model || "未配置模型"}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground sm:hidden">{formatTime(run.created_at)}</div>
                    </TableCell>
                    <TableCell>
                      <Badge value={run.status} />
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground sm:table-cell">{formatTime(run.created_at)}</TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={3}>
                    <EmptyState title="暂无运行记录" className="min-h-32" />
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

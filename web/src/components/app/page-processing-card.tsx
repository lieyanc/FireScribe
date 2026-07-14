import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, ImagePlus, RefreshCw, RotateCcw, ScanLine, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage } from "@/components/app/chrome";
import { StatusBadge } from "@/components/app/status-badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import {
  cancelJob,
  getPageProcessingPreview,
  listPageProcessingRuns,
  retryJob,
  startPageProcessing,
  type PageDetail,
  type PageProcessingConfig,
} from "@/lib/api";
import { formatTime } from "@/lib/format";

const ACTIVE = new Set(["queued", "running"]);

const DEFAULT_CONFIG: PageProcessingConfig = {
  auto_crop: true,
  normalize_background: true,
  deskew: true,
  enhance_contrast: true,
  detect_segments: true,
  crop_padding: 24,
  deskew_max_angle: 3,
  deskew_step: 0.5,
};

type Metadata = { output_width?: number; output_height?: number; deskew_angle?: number };

function PreviewImage({ src, alt, segments = [], width = 0, height = 0 }: {
  src: string;
  alt: string;
  segments?: Array<{ id: string; x: number; y: number; width: number; height: number }>;
  width?: number;
  height?: number;
}) {
  return (
    <div className="flex min-h-56 items-center justify-center overflow-hidden rounded-md border bg-muted/30">
      <div className="relative max-w-full">
        <img src={src} alt={alt} className="block max-h-[34rem] max-w-full object-contain" />
        {width > 0 && height > 0 ? segments.map((segment) => (
          <span
            key={segment.id}
            className="pointer-events-none absolute border border-primary/60 bg-primary/10"
            style={{
              left: `${segment.x / width * 100}%`,
              top: `${segment.y / height * 100}%`,
              width: `${segment.width / width * 100}%`,
              height: `${segment.height / height * 100}%`,
            }}
          />
        )) : null}
      </div>
    </div>
  );
}

export function PageProcessingCard({ documentID, pages }: { documentID: string; pages: PageDetail[] }) {
  const queryClient = useQueryClient();
  const [selectedPageID, setSelectedPageID] = useState("");
  const [config, setConfig] = useState(DEFAULT_CONFIG);
  useEffect(() => {
    if (!selectedPageID && pages[0]?.page_id) setSelectedPageID(pages[0].page_id);
    if (selectedPageID && !pages.some((page) => page.page_id === selectedPageID)) setSelectedPageID(pages[0]?.page_id ?? "");
  }, [pages, selectedPageID]);

  const runs = useQuery({
    queryKey: ["page-processing-runs", documentID],
    queryFn: () => listPageProcessingRuns(documentID),
    enabled: !!documentID,
    refetchInterval: (query) => query.state.data?.some((run) => ACTIVE.has(run.status)) ? 1500 : 5000,
  });
  const preview = useQuery({
    queryKey: ["page-processing-preview", selectedPageID],
    queryFn: () => getPageProcessingPreview(selectedPageID),
    enabled: !!selectedPageID,
  });
  const latest = runs.data?.[0];
  const active = latest && ACTIVE.has(latest.status);
  const process = useMutation({
    mutationFn: () => startPageProcessing(documentID, { config }),
    onSuccess: () => {
      toast.success("页图处理任务已创建");
      queryClient.invalidateQueries({ queryKey: ["page-processing-runs", documentID] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
    onError: (error: Error) => toast.error("无法启动页图处理", { description: error.message }),
  });
  const cancel = useMutation({
    mutationFn: (jobID: string) => cancelJob(jobID),
    onSuccess: () => {
      toast("正在取消页图处理");
      queryClient.invalidateQueries({ queryKey: ["page-processing-runs", documentID] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
  const retry = useMutation({
    mutationFn: (jobID: string) => retryJob(jobID),
    onSuccess: () => {
      toast.success("已重试失败页面");
      queryClient.invalidateQueries({ queryKey: ["page-processing-runs", documentID] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });

  useEffect(() => {
    if (latest?.status === "succeeded" || latest?.status === "partial") {
      void queryClient.invalidateQueries({ queryKey: ["page-processing-preview"] });
      void queryClient.invalidateQueries({ queryKey: ["assets", documentID] });
    }
  }, [documentID, latest?.status, queryClient]);

  const metadata = useMemo<Metadata>(() => {
    try {
      return JSON.parse(preview.data?.result?.metadata_json || "{}") as Metadata;
    } catch {
      return {};
    }
  }, [preview.data?.result?.metadata_json]);
  const progress = latest?.total_pages ? latest.done_pages + latest.failed_pages : 0;

  const toggles: Array<{ key: keyof PageProcessingConfig; label: string; description: string }> = [
    { key: "auto_crop", label: "自动裁边", description: "保留安全留白，不改写原图。" },
    { key: "normalize_background", label: "去阴影", description: "归一化纸张照明与背景。" },
    { key: "deskew", label: "倾斜矫正", description: "在 ±3° 内搜索文字行角度。" },
    { key: "enhance_contrast", label: "对比度增强", description: "拉开墨迹与纸张层次。" },
    { key: "detect_segments", label: "区域切分", description: "保存可预览的文本带区域。" },
  ];

  return (
    <Card>
      <CardHeader className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="flex flex-col gap-1">
          <CardTitle className="inline-flex items-center gap-2"><Sparkles className="size-5" />页图增强</CardTitle>
          <CardDescription>从不可变原图生成独立增强资产；识别时可明确选择增强图。</CardDescription>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {latest ? <StatusBadge value={latest.status} /> : null}
          {active ? (
            <Button variant="outline" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate(latest.job_id)}>
              {cancel.isPending ? <Spinner /> : <Ban />}取消
            </Button>
          ) : latest && ["failed", "partial"].includes(latest.status) ? (
            <Button variant="outline" size="sm" disabled={retry.isPending} onClick={() => retry.mutate(latest.job_id)}>
              {retry.isPending ? <Spinner /> : <RotateCcw />}重试失败页
            </Button>
          ) : null}
          <Button size="sm" disabled={!pages.length || !!active || process.isPending} onClick={() => process.mutate()}>
            {process.isPending ? <Spinner /> : <ImagePlus />}
            {latest?.status === "succeeded" ? "重新处理全部页" : "处理全部页"}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          {toggles.map((item) => (
            <Field key={item.key} orientation="horizontal" className="justify-between rounded-md border p-3">
              <div className="flex flex-col gap-1">
                <FieldLabel htmlFor={`processing-${item.key}`}>{item.label}</FieldLabel>
                <FieldDescription>{item.description}</FieldDescription>
              </div>
              <Switch
                id={`processing-${item.key}`}
                checked={Boolean(config[item.key])}
                onCheckedChange={(checked) => setConfig((current) => ({ ...current, [item.key]: checked }))}
              />
            </Field>
          ))}
        </div>

        {latest ? (
          <div className="flex flex-col gap-2 rounded-md border p-3">
            <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
              <span>{latest.status === "running" ? "正在处理" : `最近运行 · ${formatTime(latest.created_at)}`}</span>
              <span className="text-muted-foreground">成功 {latest.done_pages} / 失败 {latest.failed_pages} / 共 {latest.total_pages}</span>
            </div>
            <Progress value={latest.total_pages ? progress / latest.total_pages * 100 : 0} />
            <ErrorMessage message={latest.last_error} />
          </div>
        ) : null}
        <ErrorMessage message={runs.error?.message || process.error?.message || cancel.error?.message || retry.error?.message} />

        <div className="flex flex-wrap items-end justify-between gap-3">
          <Field className="w-full max-w-56">
            <FieldLabel>预览页面</FieldLabel>
            <Select value={selectedPageID} onValueChange={setSelectedPageID}>
              <SelectTrigger><SelectValue placeholder="选择页面" /></SelectTrigger>
              <SelectContent>{pages.map((page) => <SelectItem key={page.page_id} value={page.page_id}>第 {page.page_no} 页</SelectItem>)}</SelectContent>
            </Select>
          </Field>
          <Button variant="ghost" size="sm" disabled={preview.isFetching || !selectedPageID} onClick={() => void preview.refetch()}>
            {preview.isFetching ? <Spinner /> : <RefreshCw />}刷新预览
          </Button>
        </div>

        {preview.isError ? (
          <EmptyState title="预览加载失败" description={preview.error.message} />
        ) : selectedPageID ? (
          <div className="grid gap-4 lg:grid-cols-2">
            <div className="flex flex-col gap-2">
              <div className="text-sm font-medium">不可变原图</div>
              <PreviewImage src={preview.data?.original_url || `/api/pages/${selectedPageID}/image`} alt="原始页图" />
            </div>
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-2 text-sm font-medium">
                <span>增强图</span>
                {preview.data?.segments?.length ? <span className="inline-flex items-center gap-1 text-xs text-muted-foreground"><ScanLine className="size-3" />{preview.data.segments.length} 个区域</span> : null}
              </div>
              {preview.data?.result?.enhanced_url ? (
                <PreviewImage
                  src={preview.data.result.enhanced_url}
                  alt="增强页图"
                  segments={preview.data.segments}
                  width={metadata.output_width}
                  height={metadata.output_height}
                />
              ) : <EmptyState title={active ? "正在生成增强图" : "尚未处理此页"} description="运行页图增强后，可在这里对照原图并查看区域切分。" className="min-h-56" />}
              {preview.data?.result ? <div className="text-xs text-muted-foreground">输出 {metadata.output_width ?? "--"} × {metadata.output_height ?? "--"}{metadata.deskew_angle ? ` · 矫正 ${metadata.deskew_angle}°` : ""}</div> : null}
            </div>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

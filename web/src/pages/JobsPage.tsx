import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Ban, Clock3, DatabaseZap, History, ListChecks, RotateCcw, TimerReset } from "lucide-react";
import { EmptyState, ErrorMessage, IconTooltipButton, MetricCard, PageHeader } from "../components/app/chrome";
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
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../components/ui/dialog";
import { Field, FieldTitle } from "../components/ui/field";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { Progress } from "../components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "../components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "../components/ui/tooltip";
import { cancelJob, listJobEvents, listJobs, rebuildSearchIndex, retryJob } from "../lib/api";
import { cn, formatTime } from "../lib/utils";

const JOB_TYPE_LABELS: Record<string, string> = {
  recognize_document: "识别文档",
  import_document: "导入文档",
  export_document: "导出文档",
  export_project: "导出项目",
  rebuild_search_index: "重建搜索索引",
  process_pages: "增强页图",
  apply_update: "应用更新",
};

function jobTypeLabel(type: string) {
  return JOB_TYPE_LABELS[type] ?? type;
}

const TARGET_TYPE_LABELS: Record<string, string> = {
  recognition_run: "识别运行",
  document: "文档",
  page: "页面",
  page_processing_run: "页图处理运行",
  project: "项目",
};

function targetLabel(targetType: string, targetID: string) {
  if (!targetType && !targetID) return "";
  const label = TARGET_TYPE_LABELS[targetType] ?? targetType;
  return targetID ? `${label} · ${targetID.slice(0, 8)}` : label;
}

export function JobsPage() {
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState("all");
  const [cancelID, setCancelID] = useState("");
  const [eventJobID, setEventJobID] = useState("");
  const jobs = useQuery({ queryKey: ["jobs"], queryFn: listJobs, refetchInterval: 2500 });
  const events = useQuery({ queryKey: ["job-events", eventJobID], queryFn: () => listJobEvents(eventJobID), enabled: Boolean(eventJobID), refetchInterval: eventJobID ? 2500 : false });
  const cancel = useMutation({
    mutationFn: cancelJob,
    onSuccess: () => {
      setCancelID("");
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
  const retry = useMutation({
    mutationFn: retryJob,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  });
  const rebuild = useMutation({
    mutationFn: rebuildSearchIndex,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  });
  const items = jobs.data ?? [];
  const running = items.filter((job) => job.status === "running").length;
  const queued = items.filter((job) => job.status === "queued").length;
  const failed = items.filter((job) => job.status === "failed").length;
  const visibleItems = items.filter((job) => {
    if (filter === "active") return ["queued", "running"].includes(job.status);
    if (filter === "failed") return job.status === "failed";
    return true;
  });

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="任务" description={`${items.length} 条后台任务 · 自动刷新`}>
        <Button
          variant="outline"
          onClick={() => rebuild.mutate()}
          disabled={rebuild.isPending || items.some((job) => job.type === "rebuild_search_index" && ["queued", "running"].includes(job.status))}
        >
          {rebuild.isPending ? <Spinner data-icon="inline-start" /> : <DatabaseZap data-icon="inline-start" />}
          重建搜索索引
        </Button>
      </PageHeader>

      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard icon={<TimerReset />} label="运行中" value={running} hint={queued ? `${queued} 个排队` : "队列空闲"} />
        <MetricCard icon={<ListChecks />} label="总数" value={items.length} />
        <MetricCard icon={<Ban />} label="失败" value={failed} />
      </section>

      <ErrorMessage message={jobs.error?.message} title="任务列表加载失败" onRetry={() => void jobs.refetch()} />
      <ErrorMessage message={cancel.error?.message || retry.error?.message || rebuild.error?.message} />

      <Card>
        <CardHeader className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-col gap-1">
            <CardTitle>任务记录</CardTitle>
            <CardDescription>自动刷新运行状态；失败任务可直接重试。</CardDescription>
          </div>
          <Field orientation="horizontal" className="w-auto">
            <FieldTitle id="job-filter">显示</FieldTitle>
            <ToggleGroup
              type="single"
              value={filter}
              onValueChange={(value) => value && setFilter(value)}
              aria-labelledby="job-filter"
              variant="outline"
              size="sm"
            >
              <ToggleGroupItem value="all">全部</ToggleGroupItem>
              <ToggleGroupItem value="active">进行中</ToggleGroupItem>
              <ToggleGroupItem value="failed">失败</ToggleGroupItem>
            </ToggleGroup>
          </Field>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
          <TableHeader>
            <TableRow>
              <TableHead>类型</TableHead>
              <TableHead className="w-28">状态</TableHead>
              <TableHead className="w-24">次数</TableHead>
              <TableHead className="hidden w-36 md:table-cell">时间</TableHead>
              <TableHead className="w-24 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {jobs.isLoading ? (
              Array.from({ length: 4 }, (_, index) => (
                <TableRow key={index}>
                  <TableCell>
                    <Skeleton className="h-4 w-32" />
                    <Skeleton className="mt-2 h-3 w-24" />
                  </TableCell>
                  <TableCell>
                    <Skeleton className="h-5 w-16" />
                  </TableCell>
                  <TableCell>
                    <Skeleton className="h-4 w-10" />
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    <Skeleton className="h-4 w-24" />
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-2">
                      <Skeleton className="size-9" />
                      <Skeleton className="size-9" />
                    </div>
                  </TableCell>
                </TableRow>
              ))
            ) : visibleItems.length ? (
              visibleItems.map((job) => {
                const target = targetLabel(job.target_type, job.target_id);
                let processingDocumentID = "";
                if (job.type === "process_pages") {
                  try {
                    processingDocumentID = (JSON.parse(job.payload_json) as { document_id?: string }).document_id ?? "";
                  } catch {
                    processingDocumentID = "";
                  }
                }
                return (
                  <TableRow key={job.id} className={cn(job.status === "failed" && "border-l-2 border-l-destructive bg-destructive/5")}>
                    <TableCell className="min-w-0">
                      <div className="truncate font-medium">{jobTypeLabel(job.type)}</div>
                      {target ? (
                        job.target_type === "document" || job.target_type === "project" || processingDocumentID ? (
                          <Link className="mt-0.5 block truncate text-xs text-muted-foreground hover:text-foreground" to={job.target_type === "project" ? `/projects/${job.target_id}` : `/documents/${processingDocumentID || job.target_id}`}>
                            {target}
                          </Link>
                        ) : (
                          <div className="mt-0.5 truncate text-xs text-muted-foreground">{target}</div>
                        )
                      ) : null}
                      {job.last_error ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button type="button" className="mt-1 block max-w-md cursor-help truncate text-left text-xs text-destructive">
                              {job.last_error}
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom" align="start" className="max-w-sm whitespace-pre-wrap break-words">
                            {job.last_error}
                          </TooltipContent>
                        </Tooltip>
                      ) : null}
                      {job.progress_total > 0 && ["queued", "running"].includes(job.status) ? (
                        <div className="mt-2 flex max-w-md flex-col gap-1">
                          <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
                            <span className="truncate">{job.progress_message || "处理中"}</span>
                            <span>{job.progress_current}/{job.progress_total}</span>
                          </div>
                          <Progress value={(job.progress_current / job.progress_total) * 100} className="h-1.5" />
                        </div>
                      ) : job.progress_message ? (
                        <div className="mt-1 truncate text-xs text-muted-foreground">{job.progress_message}</div>
                      ) : null}
                      <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground md:hidden">
                        <Clock3 className="size-3" />
                        {formatTime(job.created_at)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <StatusBadge value={job.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {job.attempts}/{job.max_attempts}
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground md:table-cell">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button type="button" className="cursor-help">{formatTime(job.created_at)}</button>
                        </TooltipTrigger>
                        <TooltipContent side="bottom" align="start">
                          <div className="flex flex-col gap-0.5">
                            <div>创建:{formatTime(job.created_at) || "--"}</div>
                            <div>开始:{formatTime(job.started_at) || "--"}</div>
                            <div>完成:{formatTime(job.finished_at) || "--"}</div>
                          </div>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <IconTooltipButton variant="secondary" size="icon" label="事件日志" onClick={() => setEventJobID(job.id)}>
                          <History />
                        </IconTooltipButton>
                        <IconTooltipButton
                          variant="secondary"
                          size="icon"
                          label="重试"
                          disabled={job.status !== "failed" || retry.isPending || (job.type !== "recognize_document" && job.attempts >= job.max_attempts)}
                          onClick={() => retry.mutate(job.id)}
                        >
                          {retry.isPending && retry.variables === job.id ? <Spinner /> : <RotateCcw />}
                        </IconTooltipButton>
                        <IconTooltipButton
                          variant="secondary"
                          size="icon"
                          label="取消"
                          disabled={!["queued", "running"].includes(job.status) || cancel.isPending}
                          onClick={() => setCancelID(job.id)}
                        >
                          <Ban />
                        </IconTooltipButton>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            ) : (
              <TableRow>
                <TableCell colSpan={5}>
                  <EmptyState
                    icon={<ListChecks />}
                    title={items.length ? "当前筛选没有任务" : "暂无任务"}
                    description={items.length ? "切换筛选条件可查看其他任务。" : "识别、导入和更新任务会显示在这里。"}
                  />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={Boolean(eventJobID)} onOpenChange={(open) => !open && setEventJobID("")}>
        <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>任务事件日志</DialogTitle>
            <DialogDescription>按尝试和阶段记录任务的排队、进度及终态。</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            {events.isLoading ? <Skeleton className="h-24 w-full" /> : events.data?.length ? events.data.map((event) => (
              <div key={event.id} className={cn("rounded-md border p-3", event.level === "error" && "border-destructive/40 bg-destructive/5")}>
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span className="font-medium">第 {event.attempt || 0} 次 · {event.stage}</span>
                  <span className="text-xs text-muted-foreground">{formatTime(event.created_at)}</span>
                </div>
                <div className="mt-1 whitespace-pre-wrap break-words text-sm text-muted-foreground">{event.message}</div>
              </div>
            )) : <div className="py-8 text-center text-sm text-muted-foreground">暂无事件记录</div>}
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(cancelID)} onOpenChange={(open) => !cancel.isPending && !open && setCancelID("")}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>取消这个任务？</AlertDialogTitle>
            <AlertDialogDescription>任务会停止执行；已经完成的处理结果不会被删除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cancel.isPending}>继续执行</AlertDialogCancel>
            <AlertDialogAction
              disabled={cancel.isPending}
              variant="destructive"
              onClick={(event) => {
                event.preventDefault();
                if (cancelID) cancel.mutate(cancelID);
              }}
            >
              {cancel.isPending ? <Spinner data-icon="inline-start" /> : null}
              确认取消
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

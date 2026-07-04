import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, Clock3, ListChecks, RotateCcw, TimerReset } from "lucide-react";
import { EmptyState, ErrorMessage, IconTooltipButton, MetricCard, PageHeader } from "../components/app/chrome";
import { Badge } from "../components/ui/badge";
import { Card } from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "../components/ui/tooltip";
import { cancelJob, listJobs, retryJob } from "../lib/api";
import { cn, formatTime } from "../lib/utils";

const JOB_TYPE_LABELS: Record<string, string> = {
  recognize_document: "识别文档",
  import_document: "导入文档",
  apply_update: "应用更新",
};

function jobTypeLabel(type: string) {
  return JOB_TYPE_LABELS[type] ?? type;
}

const TARGET_TYPE_LABELS: Record<string, string> = {
  recognition_run: "识别运行",
  document: "文档",
  page: "页面",
};

function targetLabel(targetType: string, targetID: string) {
  if (!targetType && !targetID) return "";
  const label = TARGET_TYPE_LABELS[targetType] ?? targetType;
  return targetID ? `${label} · ${targetID.slice(0, 8)}` : label;
}

export function JobsPage() {
  const queryClient = useQueryClient();
  const jobs = useQuery({ queryKey: ["jobs"], queryFn: listJobs, refetchInterval: 2500 });
  const cancel = useMutation({
    mutationFn: cancelJob,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  });
  const retry = useMutation({
    mutationFn: retryJob,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  });
  const items = jobs.data ?? [];
  const running = items.filter((job) => job.status === "running").length;
  const queued = items.filter((job) => job.status === "queued").length;
  const failed = items.filter((job) => job.status === "failed").length;

  return (
    <div className="space-y-5">
      <PageHeader title="任务" description={`${items.length} 条后台任务 · 自动刷新`} />

      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard icon={<TimerReset className="size-4" />} label="运行中" value={running} hint={queued ? `${queued} 个排队` : "队列空闲"} />
        <MetricCard icon={<ListChecks className="size-4" />} label="总数" value={items.length} />
        <MetricCard icon={<Ban className="size-4" />} label="失败" value={failed} />
      </section>

      <ErrorMessage message={cancel.error?.message || retry.error?.message} />

      <Card>
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
            ) : items.length ? (
              items.map((job) => {
                const target = targetLabel(job.target_type, job.target_id);
                return (
                  <TableRow key={job.id} className={cn(job.status === "failed" && "border-l-2 border-l-destructive bg-destructive/5")}>
                    <TableCell className="min-w-0">
                      <div className="truncate font-medium">{jobTypeLabel(job.type)}</div>
                      {target ? <div className="mt-0.5 truncate text-xs text-muted-foreground">{target}</div> : null}
                      {job.last_error ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="mt-1 max-w-md cursor-help truncate text-xs text-destructive">{job.last_error}</div>
                          </TooltipTrigger>
                          <TooltipContent side="bottom" align="start" className="max-w-sm whitespace-pre-wrap break-words">
                            {job.last_error}
                          </TooltipContent>
                        </Tooltip>
                      ) : null}
                      <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground md:hidden">
                        <Clock3 className="size-3" />
                        {formatTime(job.created_at)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge value={job.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {job.attempts}/{job.max_attempts}
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground md:table-cell">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="cursor-help">{formatTime(job.created_at)}</span>
                        </TooltipTrigger>
                        <TooltipContent side="bottom" align="start">
                          <div className="space-y-0.5">
                            <div>创建:{formatTime(job.created_at) || "--"}</div>
                            <div>开始:{formatTime(job.started_at) || "--"}</div>
                            <div>完成:{formatTime(job.finished_at) || "--"}</div>
                          </div>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <IconTooltipButton
                          variant="secondary"
                          size="icon"
                          label="重试"
                          disabled={job.status !== "failed" || retry.isPending}
                          onClick={() => retry.mutate(job.id)}
                        >
                          <RotateCcw className="size-4" />
                        </IconTooltipButton>
                        <IconTooltipButton
                          variant="secondary"
                          size="icon"
                          label="取消"
                          disabled={!["queued", "running"].includes(job.status) || cancel.isPending}
                          onClick={() => cancel.mutate(job.id)}
                        >
                          <Ban className="size-4" />
                        </IconTooltipButton>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            ) : (
              <TableRow>
                <TableCell colSpan={5}>
                  <EmptyState icon={<ListChecks className="size-5" />} title="暂无任务" description="识别、导入和更新任务会显示在这里。" />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}

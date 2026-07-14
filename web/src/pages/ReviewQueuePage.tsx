import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { AlertTriangle, FileSearch, Gauge, MessageSquareWarning } from "lucide-react";
import { EmptyState, ErrorMessage, MetricCard, PageHeader } from "../components/app/chrome";
import { StatusBadge } from "../components/app/status-badge";
import { Card } from "../components/ui/card";
import { Slider } from "../components/ui/slider";
import { Skeleton } from "../components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { listReviewQueue } from "../lib/api";
import { formatTime } from "../lib/format";

export function ReviewQueuePage() {
  const [thresholdPercent, setThresholdPercent] = useState(80);
  const threshold = thresholdPercent / 100;
  const queue = useQuery({
    queryKey: ["review-queue", thresholdPercent],
    queryFn: () => listReviewQueue(threshold),
  });
  const items = queue.data ?? [];
  const uncertainCount = items.filter((item) => item.open_uncertain_count > 0).length;
  const segmentCount = items.reduce((total, item) => total + item.low_confidence_segments.length, 0);

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="低置信队列" description="优先处理 provider 标出的低置信字词、行、段；没有细粒度数据时回退到页面级置信度。" />

      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard icon={<Gauge />} label="低置信片段" value={queue.isLoading ? <Skeleton className="h-6 w-10" /> : segmentCount} hint={`${items.length} 个待复核页面`} />
        <MetricCard icon={<MessageSquareWarning />} label="含存疑标记" value={queue.isLoading ? <Skeleton className="h-6 w-10" /> : uncertainCount} />
        <MetricCard icon={<AlertTriangle />} label="置信度阈值" value={`${thresholdPercent}%`} hint="阈值越高，进入队列的页面越多。" />
      </section>

      <Card className="p-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <div className="min-w-36 text-sm font-medium">低于或等于 {thresholdPercent}%</div>
          <Slider
            aria-label="低置信阈值"
            className="max-w-xl flex-1"
            min={10}
            max={100}
            step={5}
            value={[thresholdPercent]}
            onValueChange={(value) => setThresholdPercent(value[0])}
          />
        </div>
      </Card>

      <ErrorMessage message={queue.error?.message} onRetry={() => void queue.refetch()} />

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>文档与页面</TableHead>
              <TableHead className="w-28">置信度</TableHead>
              <TableHead className="hidden w-36 md:table-cell">模型</TableHead>
              <TableHead className="w-24">状态</TableHead>
              <TableHead className="hidden w-36 lg:table-cell">更新</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {queue.isLoading ? (
              Array.from({ length: 4 }, (_, index) => (
                <TableRow key={index}>
                  <TableCell><Skeleton className="h-5 w-48" /></TableCell>
                  <TableCell><Skeleton className="h-5 w-14" /></TableCell>
                  <TableCell className="hidden md:table-cell"><Skeleton className="h-5 w-24" /></TableCell>
                  <TableCell><Skeleton className="h-5 w-16" /></TableCell>
                  <TableCell className="hidden lg:table-cell"><Skeleton className="h-5 w-24" /></TableCell>
                </TableRow>
              ))
            ) : items.length ? (
              items.map((item) => (
                <TableRow key={item.page_id}>
                  <TableCell className="min-w-0">
                    <Link className="font-medium hover:text-primary" to={`/review/${item.document_id}/${item.page_id}`}>
                      {item.document_title} · 第 {item.page_no} 页
                    </Link>
                    <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      <span>识别 {item.recognition_count} 次</span>
                      {item.open_uncertain_count > 0 ? <span className="text-warning-text">存疑 {item.open_uncertain_count} 项</span> : null}
                    </div>
                    {item.low_confidence_segments.length ? (
                      <div className="mt-2 flex flex-col gap-1.5">
                        {item.low_confidence_segments.slice(0, 4).map((segment, index) => (
                          <Link
                            key={`${segment.start}-${segment.end}-${index}`}
                            className="flex max-w-xl items-center gap-2 rounded-md border bg-muted/30 px-2 py-1 text-xs hover:border-primary/50 hover:bg-muted"
                            to={`/review/${item.document_id}/${item.page_id}?focus_start=${segment.start}&focus_end=${segment.end}`}
                          >
                            <span className="shrink-0 tabular-nums text-warning-text">{Math.round(segment.confidence * 100)}%</span>
                            <span className="truncate">{segment.text.replace(/\s+/g, " ") || "空白片段"}</span>
                            <span className="ml-auto shrink-0 text-muted-foreground">{segmentLevelLabel(segment.level)}</span>
                          </Link>
                        ))}
                        {item.low_confidence_segments.length > 4 ? <span className="text-xs text-muted-foreground">另有 {item.low_confidence_segments.length - 4} 个片段</span> : null}
                      </div>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    {item.confidence == null ? (
                      <span className="text-sm text-muted-foreground">未知</span>
                    ) : (
                      <span className="font-medium tabular-nums">{Math.round(item.confidence * 100)}%</span>
                    )}
                  </TableCell>
                  <TableCell className="hidden text-muted-foreground md:table-cell">
                    {item.last_model || item.last_provider || "--"}
                  </TableCell>
                  <TableCell><StatusBadge value={item.page_status} /></TableCell>
                  <TableCell className="hidden text-muted-foreground lg:table-cell">{formatTime(item.updated_at) || "--"}</TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={5}>
                  <EmptyState
                    icon={<FileSearch />}
                    title="当前没有低置信页面"
                    description="降低阈值，或在校对页添加存疑标记后，这里会显示需要优先处理的页面。"
                  />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}

function segmentLevelLabel(level: string) {
  return ({ token: "字词", word: "词", line: "行", paragraph: "段" } as Record<string, string>)[level] ?? "片段";
}

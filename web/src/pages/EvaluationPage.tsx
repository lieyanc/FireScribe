import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Activity, Gauge, ScanSearch, Timer, TrendingUp } from "lucide-react";
import { EmptyState, ErrorMessage, MetricCard, PageHeader } from "../components/app/chrome";
import { Badge } from "../components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Field, FieldDescription, FieldLabel } from "../components/ui/field";
import { Skeleton } from "../components/ui/skeleton";
import { Switch } from "../components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { getEvaluationMetrics } from "../lib/api";
import { formatTime } from "../lib/utils";

export function EvaluationPage() {
  const [benchmarkOnly, setBenchmarkOnly] = useState(true);
  const metrics = useQuery({
    queryKey: ["evaluation", benchmarkOnly],
    queryFn: () => getEvaluationMetrics(benchmarkOnly),
  });
  const data = metrics.data;

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="识别评测" description="以最新最终定稿作为人工真值，持续跟踪候选质量、处理耗时和审校产能。" />

      <Card>
        <CardContent className="pt-6">
          <Field orientation="horizontal" className="justify-between">
            <div>
              <FieldLabel htmlFor="benchmark-only">仅基准集</FieldLabel>
              <FieldDescription>只统计带“基准”或“benchmark”标签且已有最终定稿的文档；建议选取 20–50 页真实扫描稿。</FieldDescription>
            </div>
            <Switch id="benchmark-only" checked={benchmarkOnly} onCheckedChange={setBenchmarkOnly} />
          </Field>
        </CardContent>
      </Card>

      <ErrorMessage message={metrics.error?.message} onRetry={() => void metrics.refetch()} />

      <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
        <MetricCard icon={<Gauge />} label="字符错误率 CER" value={metrics.isLoading ? <Skeleton className="h-6 w-16" /> : percent(data?.cer)} hint={`${data?.edit_distance ?? 0} 次编辑 / ${data?.reference_char_count ?? 0} 个真值字符`} />
        <MetricCard icon={<ScanSearch />} label="低置信命中率" value={metrics.isLoading ? <Skeleton className="h-6 w-16" /> : percent(data?.low_confidence_hit_rate)} hint={`${data?.low_confidence_hit_count ?? 0} / ${data?.low_confidence_item_count ?? 0} 个片段确有修改`} />
        <MetricCard icon={<Timer />} label="候选生成耗时" value={metrics.isLoading ? <Skeleton className="h-6 w-16" /> : duration(data?.average_candidate_seconds)} hint="从页面导入到实际校对候选版本" />
        <MetricCard icon={<Activity />} label="人工活跃校对" value={metrics.isLoading ? <Skeleton className="h-6 w-16" /> : activeDuration(data?.average_review_seconds)} hint={`仅累计校对页可见且最近有操作的时段 · ${data?.review_sample_count ?? 0} 页`} />
        <MetricCard icon={<Timer />} label="候选到定稿" value={metrics.isLoading ? <Skeleton className="h-6 w-16" /> : duration(data?.average_turnaround_seconds)} hint="墙钟时间，包含排队和等待" />
        <MetricCard icon={<TrendingUp />} label="活跃小时产能" value={metrics.isLoading ? <Skeleton className="h-6 w-16" /> : `${(data?.pages_per_active_hour ?? 0).toFixed(1)} 页`} hint={`最近一小时确认 ${data?.confirmed_last_hour ?? 0} 页`} />
      </section>

      {!metrics.isLoading && data?.sample_count === 0 ? (
        <Card>
          <EmptyState
            icon={<Gauge />}
            title={benchmarkOnly ? "基准集还没有可评测页面" : "还没有可评测页面"}
            description={benchmarkOnly ? "给文档添加“基准”标签并确认至少一页最终定稿，评测会自动出现。" : "完成识别并确认最终定稿后即可计算指标。"}
          />
        </Card>
      ) : null}

      {data?.sample_count ? (
        <>
          {data.truncated ? <Badge variant="outline">仅显示最近 200 个样本，建议使用基准集获得稳定可比结果</Badge> : null}
          <section className="grid gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader><CardTitle>模型 / Prompt 效果</CardTitle><CardDescription>按 CER 从低到高排列，样本过少时请谨慎比较。</CardDescription></CardHeader>
              <CardContent>
                <Table>
                  <TableHeader><TableRow><TableHead>方案</TableHead><TableHead className="text-right">页数</TableHead><TableHead className="text-right">CER</TableHead></TableRow></TableHeader>
                  <TableBody>{data.groups.map((group) => <TableRow key={`${group.provider}-${group.model}-${group.prompt_version}`}><TableCell><div className="font-medium">{group.model || group.provider || "未知模型"}</div><div className="text-xs text-muted-foreground">{group.prompt_version || "无 Prompt 版本"}</div></TableCell><TableCell className="text-right tabular-nums">{group.sample_count}</TableCell><TableCell className="text-right font-medium tabular-nums">{percent(group.cer)}</TableCell></TableRow>)}</TableBody>
                </Table>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle>错误构成</CardTitle><CardDescription>字符级编辑与行级漏识别、猜补、乱序。</CardDescription></CardHeader>
              <CardContent className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                <ErrorStat label="替换" value={data.substitution_count} />
                <ErrorStat label="漏字" value={data.omission_count} />
                <ErrorStat label="猜补字" value={data.addition_count} />
                <ErrorStat label="漏行" value={data.missed_line_count} />
                <ErrorStat label="猜补行" value={data.guessed_line_count} />
                <ErrorStat label="乱序行" value={data.reordered_line_count} />
              </CardContent>
            </Card>
          </section>

          <Card>
            <CardHeader><CardTitle>逐页样本</CardTitle><CardDescription>直接进入校对页复查高 CER 或行级异常。</CardDescription></CardHeader>
            <CardContent>
              <Table>
                <TableHeader><TableRow><TableHead>文档与页</TableHead><TableHead>方案</TableHead><TableHead className="text-right">CER</TableHead><TableHead className="hidden text-right md:table-cell">漏 / 补 / 序</TableHead><TableHead className="hidden text-right lg:table-cell">候选 / 活跃 / 定稿</TableHead></TableRow></TableHeader>
                <TableBody>{data.samples.map((sample) => <TableRow key={sample.page_id}><TableCell><Link className="font-medium hover:text-primary" to={`/review/${sample.document_id}/${sample.page_id}`}>{sample.document_title} · 第 {sample.page_no} 页</Link><div className="text-xs text-muted-foreground">定稿 {formatTime(sample.finalized_at)}</div></TableCell><TableCell><div>{sample.model || sample.provider || "未知"}</div><div className="text-xs text-muted-foreground">{sample.prompt_version || "--"}</div></TableCell><TableCell className="text-right font-medium tabular-nums">{percent(sample.cer)}</TableCell><TableCell className="hidden text-right tabular-nums md:table-cell">{sample.missed_lines} / {sample.guessed_lines} / {sample.reordered_lines}</TableCell><TableCell className="hidden text-right text-xs tabular-nums lg:table-cell">{duration(sample.candidate_seconds)} / {activeDuration(sample.review_seconds)} / {duration(sample.turnaround_seconds)}</TableCell></TableRow>)}</TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  );
}

function ErrorStat({ label, value }: { label: string; value: number }) {
  return <div className="rounded-lg border bg-muted/20 p-3"><div className="text-xs text-muted-foreground">{label}</div><div className="mt-1 text-xl font-semibold tabular-nums">{value}</div></div>;
}

function percent(value = 0) {
  return `${(value * 100).toFixed(1)}%`;
}

function duration(seconds = 0) {
  if (seconds < 60) return `${seconds.toFixed(1)} 秒`;
  if (seconds < 3600) return `${(seconds / 60).toFixed(1)} 分`;
  return `${(seconds / 3600).toFixed(1)} 小时`;
}

function activeDuration(seconds = 0) {
  return seconds > 0 ? duration(seconds) : "尚无记录";
}

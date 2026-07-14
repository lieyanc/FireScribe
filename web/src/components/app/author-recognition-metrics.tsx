import { Activity, FileWarning, PencilLine, ScanText } from "lucide-react";
import type { AuthorCommonError, AuthorRecognitionMetrics } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { EmptyState, MetricCard } from "@/components/app/chrome";

export function AuthorRecognitionMetricsPanel({
  metrics,
  loading,
}: {
  metrics?: AuthorRecognitionMetrics;
  loading?: boolean;
}) {
  if (loading) {
    return <Skeleton className="h-96 w-full" />;
  }
  if (!metrics?.sample_count) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>识别效果</CardTitle>
          <CardDescription>以人工稿和定稿作为参考文本，统计作者维度的字符识别质量。</CardDescription>
        </CardHeader>
        <CardContent>
          <EmptyState icon={<Activity />} title="暂无可评测样本" description="关联文档并完成人工校对后，这里会显示 CER、模型对比和常见错误。" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>识别效果</CardTitle>
        <CardDescription>人工校对稿作为参考；CER 为字符编辑距离除以参考字符数，数值越低越好。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <MetricCard icon={<Activity />} label="字符错误率（CER）" value={formatPercent(metrics.cer)} hint={`${metrics.edit_distance} 次编辑 / ${metrics.reference_char_count} 个参考字符`} />
          <MetricCard icon={<PencilLine />} label="形近字 / 替换" value={metrics.substitution_count} hint="识别字符被人工替换" />
          <MetricCard icon={<ScanText />} label="漏识别" value={metrics.omission_count} hint="人工稿补入的字符" />
          <MetricCard icon={<FileWarning />} label="模型猜补" value={metrics.addition_count} hint="人工稿删除的多余字符" />
        </section>

        <Tabs defaultValue="models">
          <TabsList>
            <TabsTrigger value="models">模型 / Prompt</TabsTrigger>
            <TabsTrigger value="trend">时间趋势</TabsTrigger>
            <TabsTrigger value="errors">常见错误</TabsTrigger>
          </TabsList>
          <TabsContent value="models">
            <Table>
              <TableHeader><TableRow><TableHead>Provider / 模型</TableHead><TableHead>Prompt</TableHead><TableHead className="text-right">样本</TableHead><TableHead className="text-right">CER</TableHead><TableHead className="text-right">编辑</TableHead></TableRow></TableHeader>
              <TableBody>{metrics.groups.map((group) => <TableRow key={`${group.provider}\u0000${group.model}\u0000${group.prompt_version}`}><TableCell><p className="font-medium">{group.model || "未记录模型"}</p><p className="text-xs text-muted-foreground">{group.provider || "未记录 Provider"}</p></TableCell><TableCell>{group.prompt_version || "--"}</TableCell><TableCell className="text-right tabular-nums">{group.sample_count}</TableCell><TableCell className="text-right font-medium tabular-nums">{formatPercent(group.cer)}</TableCell><TableCell className="text-right tabular-nums">{group.edit_distance} / {group.reference_char_count}</TableCell></TableRow>)}</TableBody>
            </Table>
          </TabsContent>
          <TabsContent value="trend">
            <Table>
              <TableHeader><TableRow><TableHead>日期</TableHead><TableHead className="text-right">样本</TableHead><TableHead className="text-right">参考字符</TableHead><TableHead className="text-right">编辑距离</TableHead><TableHead className="text-right">CER</TableHead></TableRow></TableHeader>
              <TableBody>{metrics.trend.map((point) => <TableRow key={point.date}><TableCell className="font-medium">{point.date}</TableCell><TableCell className="text-right tabular-nums">{point.sample_count}</TableCell><TableCell className="text-right tabular-nums">{point.reference_char_count}</TableCell><TableCell className="text-right tabular-nums">{point.edit_distance}</TableCell><TableCell className="text-right font-medium tabular-nums">{formatPercent(point.cer)}</TableCell></TableRow>)}</TableBody>
            </Table>
          </TabsContent>
          <TabsContent value="errors">
            {metrics.common_errors.length ? <Table><TableHeader><TableRow><TableHead>类型</TableHead><TableHead>识别为</TableHead><TableHead>人工校正为</TableHead><TableHead className="text-right">次数</TableHead></TableRow></TableHeader><TableBody>{metrics.common_errors.map((error) => <TableRow key={`${error.type}\u0000${error.source}\u0000${error.corrected}`}><TableCell><ErrorTypeBadge error={error} /></TableCell><TableCell className="font-mono">{visibleCharacter(error.source)}</TableCell><TableCell className="font-mono">{visibleCharacter(error.corrected)}</TableCell><TableCell className="text-right font-medium tabular-nums">{error.count}</TableCell></TableRow>)}</TableBody></Table> : <EmptyState icon={<Activity />} title="没有字符错误" description="当前校对样本与识别原文一致。" />}
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  );
}

function ErrorTypeBadge({ error }: { error: AuthorCommonError }) {
  if (error.type === "omission") return <Badge variant="warning">漏识别</Badge>;
  if (error.type === "addition") return <Badge variant="secondary">模型猜补</Badge>;
  return <Badge variant="outline">替换</Badge>;
}

function visibleCharacter(value: string) {
  if (!value) return "∅";
  if (value === " ") return "␠ 空格";
  if (value === "\n") return "↵ 换行";
  if (value === "\t") return "⇥ 制表符";
  return value;
}

function formatPercent(value: number) {
  return new Intl.NumberFormat("zh-CN", { style: "percent", maximumFractionDigits: 2 }).format(value);
}

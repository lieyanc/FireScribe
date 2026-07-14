import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, FileClock, ListTree } from "lucide-react";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { ScrollArea } from "../ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { EmptyState, ErrorMessage } from "./chrome";
import { StatusBadge } from "./status-badge";
import {
  getExportSnapshot,
  getProjectExportSnapshot,
  listDocumentExports,
  listProjectExports,
  type ExportFile,
  type ProjectExport,
} from "../../lib/api";
import { formatTime } from "../../lib/utils";

type HistoryItem = ExportFile | ProjectExport;

export function ExportHistoryCard({ scope, targetID }: { scope: "document" | "project"; targetID: string }) {
  const [selectedID, setSelectedID] = useState("");
  const history = useQuery<HistoryItem[]>({
    queryKey: [scope === "document" ? "document-exports" : "project-exports", targetID],
    queryFn: async () => scope === "document" ? await listDocumentExports(targetID) : await listProjectExports(targetID),
    enabled: Boolean(targetID),
    refetchInterval: (query) => query.state.data?.some((item) => ["queued", "running"].includes(item.status ?? "")) ? 2000 : false,
  });
  const snapshot = useQuery({
    queryKey: ["export-snapshot", scope, selectedID],
    queryFn: () => scope === "document" ? getExportSnapshot(selectedID) : getProjectExportSnapshot(selectedID),
    enabled: Boolean(selectedID),
  });
  const items = (history.data ?? []) as HistoryItem[];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="inline-flex items-center gap-2"><FileClock className="size-5" />导出历史</CardTitle>
        <CardDescription>每次成功导出会保存逐页文本版本和实际包含的批注快照，刷新后仍可追溯和下载。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 p-0">
        <ErrorMessage message={history.error?.message || snapshot.error?.message} />
        <Table>
          <TableHeader><TableRow><TableHead>格式 / 范围</TableHead><TableHead className="w-28">状态</TableHead><TableHead className="hidden w-40 md:table-cell">创建</TableHead><TableHead className="w-44 text-right">操作</TableHead></TableRow></TableHeader>
          <TableBody>
            {items.length ? items.map((item) => (
              <TableRow key={item.id}>
                <TableCell>
                  <div className="font-medium uppercase">{item.format}</div>
                  <div className="mt-1 text-xs text-muted-foreground">{exportOptionSummary(item)}</div>
                </TableCell>
                <TableCell><StatusBadge value={item.status || "succeeded"} /></TableCell>
                <TableCell className="hidden text-muted-foreground md:table-cell">{formatTime(item.created_at) || "--"}</TableCell>
                <TableCell><div className="flex justify-end gap-2">
                  <Button variant={selectedID === item.id ? "secondary" : "outline"} size="sm" onClick={() => setSelectedID((current) => current === item.id ? "" : item.id)}>
                    <ListTree data-icon="inline-start" />快照
                  </Button>
                  {item.download_url ? <Button asChild variant="outline" size="sm"><a href={item.download_url}><Download data-icon="inline-start" />下载</a></Button> : null}
                </div></TableCell>
              </TableRow>
            )) : <TableRow><TableCell colSpan={4}><EmptyState icon={<FileClock />} title={history.isLoading ? "正在加载导出历史" : "暂无导出历史"} description={history.isLoading ? undefined : "创建导出后会在此保留状态、选项和逐页来源。"} /></TableCell></TableRow>}
          </TableBody>
        </Table>

        {selectedID ? (
          <div className="border-t p-4">
            <div className="mb-3 text-sm font-medium">逐页来源快照</div>
            <ScrollArea className="max-h-80 rounded-md border">
              <div className="divide-y">
                {(snapshot.data ?? []).map((page) => (
                  <div key={`${page.document_id}-${page.page_id}-${page.ordinal}`} className="grid gap-2 p-3 text-sm md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
                    <div><div className="font-medium">{page.document_title} · 第 {page.page_no} 页</div><div className="mt-1 text-xs text-muted-foreground">page {page.page_id}</div></div>
                    <div><div>{page.text_version_kind} · {page.text_version_id}</div><div className="mt-1 text-xs text-muted-foreground">{page.annotations.length ? `${page.annotations.length} 条审校记录` : "未包含审校记录"}</div></div>
                    <Button asChild variant="ghost" size="sm"><a href={`/review/${page.document_id}/${page.page_id}`}>打开页面</a></Button>
                  </div>
                ))}
                {!snapshot.isLoading && !snapshot.data?.length ? <EmptyState title="该导出尚无快照" description="旧版本导出或未成功的任务可能没有逐页来源记录。" className="min-h-32" /> : null}
              </div>
            </ScrollArea>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function exportOptionSummary(item: HistoryItem) {
  const values = [item.text_scope === "final" ? "仅定稿" : "当前稿"];
  if (item.include_page_numbers) values.push("页码");
  if (item.include_annotations) values.push("批注");
  if (item.include_uncertain) values.push("存疑");
  return values.join(" · ");
}

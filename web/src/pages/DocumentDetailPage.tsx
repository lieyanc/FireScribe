import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Download, FileText, Play, RefreshCw } from "lucide-react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { exportDocument, getDocument, listPages, listRecognitionRuns, startRecognition } from "../lib/api";
import { formatTime } from "../lib/utils";

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
    },
  });

  const firstPageID = pages.data?.[0]?.page_id;

  return (
    <div className="space-y-4">
      <section className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-xl font-semibold">{doc.data?.title ?? "文档"}</h1>
            {doc.data ? <Badge value={doc.data.status} /> : null}
          </div>
          <div className="mt-1 text-sm text-muted-foreground">
            {doc.data?.page_count ?? 0} 页 · {formatTime(doc.data?.updated_at)}
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" onClick={() => pages.refetch()}>
            <RefreshCw className="h-4 w-4" />
            刷新
          </Button>
          <Button onClick={() => recognition.mutate()} disabled={recognition.isPending || !pages.data?.length}>
            <Play className="h-4 w-4" />
            识别
          </Button>
          <Button variant="secondary" disabled={!firstPageID} onClick={() => navigate(`/review/${documentID}/${firstPageID}`)}>
            <FileText className="h-4 w-4" />
            校对
          </Button>
          <Button variant="secondary" onClick={() => exportMutation.mutate("md")} disabled={exportMutation.isPending}>
            <Download className="h-4 w-4" />
            MD
          </Button>
          <Button variant="secondary" onClick={() => exportMutation.mutate("txt")} disabled={exportMutation.isPending}>
            <Download className="h-4 w-4" />
            TXT
          </Button>
        </div>
      </section>

      {recognition.error ? <p className="text-sm text-destructive">{recognition.error.message}</p> : null}
      {exportMutation.error ? <p className="text-sm text-destructive">{exportMutation.error.message}</p> : null}

      <section className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {pages.data?.map((page) => (
          <Link
            key={page.page_id}
            to={`/review/${documentID}/${page.page_id}`}
            className="panel block overflow-hidden transition-colors hover:border-primary/50 hover:bg-accent/25"
          >
            <div className="aspect-[4/3] bg-muted/60">
              <img src={page.thumbnail_url} alt={`第 ${page.page_no} 页`} className="h-full w-full object-contain" />
            </div>
            <div className="space-y-2 p-3">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">第 {page.page_no} 页</span>
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
        {!pages.data?.length ? <div className="text-sm text-muted-foreground">{pages.isLoading ? "加载中" : "暂无页面"}</div> : null}
      </section>

      <section className="panel overflow-hidden">
        <div className="border-b bg-muted/50 px-3 py-2 text-sm font-medium">识别运行</div>
        {runs.data?.length ? (
          runs.data.map((run) => (
            <div key={run.id} className="grid gap-2 border-b px-3 py-3 text-sm transition-colors last:border-b-0 hover:bg-muted/40 md:grid-cols-[1fr_120px_160px]">
              <div className="min-w-0 truncate">
                {run.provider} · {run.model || "未配置模型"}
              </div>
              <Badge value={run.status} />
              <div className="text-muted-foreground">{formatTime(run.created_at)}</div>
            </div>
          ))
        ) : (
          <div className="px-3 py-6 text-sm text-muted-foreground">暂无运行记录</div>
        )}
      </section>
    </div>
  );
}

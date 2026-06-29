import { FormEvent, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { FileSearch, Import, Search, Upload } from "lucide-react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Select } from "../components/ui/select";
import { importDocument, listDocuments, searchText } from "../lib/api";
import { formatTime } from "../lib/utils";

export function DocumentsPage() {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState("");
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const documents = useQuery({
    queryKey: ["documents", query, status],
    queryFn: () => listDocuments({ q: query, status }),
  });
  const searchResults = useQuery({
    queryKey: ["search", query],
    queryFn: () => searchText(query),
    enabled: query.trim().length > 0,
  });
  const upload = useMutation({
    mutationFn: importDocument,
    onSuccess: (doc) => {
      queryClient.invalidateQueries({ queryKey: ["documents"] });
      navigate(`/documents/${doc.id}`);
    },
  });

  function submitUpload(event: FormEvent) {
    event.preventDefault();
    if (!file) return;
    upload.mutate({ file, title });
  }

  return (
    <div className="space-y-4">
      <section className="panel p-4">
        <form className="grid gap-3 md:grid-cols-[1fr_180px_180px_auto]" onSubmit={submitUpload}>
          <Input placeholder="标题" value={title} onChange={(event) => setTitle(event.target.value)} />
          <Input
            type="file"
            accept="application/pdf,image/*"
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
          />
          <Select value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="">全部状态</option>
            <option value="ready">就绪</option>
            <option value="recognizing">识别中</option>
            <option value="reviewing">校对中</option>
            <option value="finalized">已定稿</option>
            <option value="failed">失败</option>
          </Select>
          <Button type="submit" disabled={!file || upload.isPending}>
            <Upload className="h-4 w-4" />
            导入
          </Button>
        </form>
      </section>

      <section className="flex flex-col gap-3 md:flex-row md:items-center">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            className="pl-9"
            placeholder="搜索标题、作者、来源或全文"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
      </section>

      {upload.error ? <p className="text-sm text-red-700">{upload.error.message}</p> : null}

      <section className="panel overflow-hidden">
        <div className="table-grid border-b border-border bg-muted px-3 py-2 text-xs font-medium text-muted-foreground">
          <div>标题</div>
          <div>状态</div>
          <div>页数</div>
          <div>更新</div>
          <div>操作</div>
        </div>
        {documents.data?.length ? (
          documents.data.map((doc) => (
            <div key={doc.id} className="table-grid items-center border-b border-border px-3 py-3 last:border-b-0">
              <Link to={`/documents/${doc.id}`} className="min-w-0 truncate font-medium hover:text-primary">
                {doc.title}
              </Link>
              <Badge value={doc.status} />
              <div className="text-sm text-muted-foreground">{doc.page_count}</div>
              <div className="text-sm text-muted-foreground">{formatTime(doc.updated_at)}</div>
              <div className="flex gap-2">
                <Button variant="secondary" size="sm" onClick={() => navigate(`/documents/${doc.id}`)}>
                  <Import className="h-4 w-4" />
                  打开
                </Button>
              </div>
            </div>
          ))
        ) : (
          <div className="px-4 py-10 text-center text-sm text-muted-foreground">
            {documents.isLoading ? "加载中" : "暂无文档"}
          </div>
        )}
      </section>

      {query.trim() ? (
        <section className="panel p-4">
          <div className="mb-3 flex items-center gap-2 text-sm font-medium">
            <FileSearch className="h-4 w-4" />
            全文结果
          </div>
          <div className="space-y-2">
            {searchResults.data?.length ? (
              searchResults.data.map((result) => (
                <Link
                  key={`${result.text_version_id}-${result.page_id}`}
                  to={`/review/${result.document_id}/${result.page_id}`}
                  className="block rounded-md border border-border bg-white px-3 py-2 text-sm hover:border-primary"
                >
                  <div className="font-medium">
                    {result.document_title} · 第 {result.page_no} 页
                  </div>
                  <div className="mt-1 text-muted-foreground">{result.snippet}</div>
                </Link>
              ))
            ) : (
              <div className="text-sm text-muted-foreground">{searchResults.isLoading ? "检索中" : "无匹配"}</div>
            )}
          </div>
        </section>
      ) : null}
    </div>
  );
}

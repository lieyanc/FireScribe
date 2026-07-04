import { FormEvent, MouseEvent, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ArrowUpRight, BookOpenText, CheckCircle2, Clock3, FileSearch, FileText, Search, Upload } from "lucide-react";
import { EmptyState, ErrorMessage, IconTooltipButton, MetricCard, PageHeader } from "../components/app/chrome";
import { TagChips } from "../components/app/tags";
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
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import { importDocument, listDocuments, listTags, searchText } from "../lib/api";
import { formatTime } from "../lib/utils";

const statusOptions = [
  { value: "all", label: "全部状态" },
  { value: "ready", label: "就绪" },
  { value: "recognizing", label: "识别中" },
  { value: "reviewing", label: "校对中" },
  { value: "finalized", label: "已定稿" },
  { value: "failed", label: "失败" },
];

function useDebouncedValue<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

export function DocumentsPage() {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [tag, setTag] = useState("");
  const debouncedQuery = useDebouncedValue(query, 300);
  const navigate = useNavigate();

  const documents = useQuery({
    queryKey: ["documents", debouncedQuery, status, tag],
    queryFn: () => listDocuments({ q: debouncedQuery, status, tag }),
  });
  const tags = useQuery({ queryKey: ["tags"], queryFn: listTags });
  const searchResults = useQuery({
    queryKey: ["search", debouncedQuery],
    queryFn: () => searchText(debouncedQuery),
    enabled: debouncedQuery.trim().length > 0,
  });

  const docs = documents.data ?? [];
  const totalPages = docs.reduce((sum, doc) => sum + doc.page_count, 0);
  const finalizedCount = docs.filter((doc) => doc.status === "finalized").length;
  const activeCount = docs.filter((doc) => ["importing", "recognizing", "reviewing"].includes(doc.status)).length;
  const loading = documents.isLoading;

  function openDocumentFromRow(event: MouseEvent<HTMLTableRowElement>, id: string) {
    if ((event.target as HTMLElement).closest("a,button")) return;
    navigate(`/documents/${id}`);
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="文档库"
        description={`${loading ? "正在同步" : `${docs.length} 份文档`} · ${totalPages} 页`}
      >
        <ImportDocumentDialog />
      </PageHeader>

      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard
          icon={<BookOpenText className="size-4" />}
          label="当前列表"
          value={loading ? <Skeleton className="h-5 w-10" /> : docs.length}
        />
        <MetricCard
          icon={<FileText className="size-4" />}
          label="页面"
          value={loading ? <Skeleton className="h-5 w-10" /> : totalPages}
        />
        <MetricCard
          icon={<CheckCircle2 className="size-4" />}
          label="已定稿"
          value={loading ? <Skeleton className="h-5 w-10" /> : finalizedCount}
          hint={loading ? undefined : activeCount ? `${activeCount} 项处理中` : "无处理项"}
        />
      </section>

      <div className="flex flex-col gap-2 sm:flex-row">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" />
          <Input
            className="pl-9"
            placeholder="搜索标题、作者、来源或全文"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        <Select value={status || "all"} onValueChange={(value) => setStatus(value === "all" ? "" : value)}>
          <SelectTrigger aria-label="状态筛选" className="sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {statusOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={tag || "all"} onValueChange={(value) => setTag(value === "all" ? "" : value)}>
          <SelectTrigger aria-label="标签筛选" className="sm:w-40">
            <SelectValue placeholder="标签" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部标签</SelectItem>
            {(tags.data ?? []).map((item) => (
              <SelectItem key={item.id} value={item.name}>
                {item.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <ErrorMessage message={documents.error?.message} />

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>标题</TableHead>
              <TableHead className="w-28">状态</TableHead>
              <TableHead className="hidden w-20 sm:table-cell">页数</TableHead>
              <TableHead className="hidden w-36 md:table-cell">更新</TableHead>
              <TableHead className="w-16 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              Array.from({ length: 3 }, (_, index) => (
                <TableRow key={index}>
                  <TableCell>
                    <Skeleton className="h-4 w-2/3 max-w-56" />
                  </TableCell>
                  <TableCell>
                    <Skeleton className="h-5 w-14" />
                  </TableCell>
                  <TableCell className="hidden sm:table-cell">
                    <Skeleton className="h-4 w-8" />
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    <Skeleton className="h-4 w-24" />
                  </TableCell>
                  <TableCell className="text-right">
                    <Skeleton className="ml-auto size-7" />
                  </TableCell>
                </TableRow>
              ))
            ) : docs.length ? (
              docs.map((doc) => (
                <TableRow
                  key={doc.id}
                  className="cursor-pointer transition-colors hover:bg-muted/50"
                  onClick={(event) => openDocumentFromRow(event, doc.id)}
                >
                  <TableCell className="w-full max-w-0 overflow-hidden">
                    <Link to={`/documents/${doc.id}`} className="block truncate font-medium hover:text-primary">
                      {doc.title}
                    </Link>
                    <TagChips tags={doc.tags} className="mt-1.5" />
                    <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground md:hidden">
                      <Clock3 className="size-3" />
                      {formatTime(doc.updated_at) || "--"}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge value={doc.status} />
                  </TableCell>
                  <TableCell className="hidden text-muted-foreground sm:table-cell">{doc.page_count}</TableCell>
                  <TableCell className="hidden text-muted-foreground md:table-cell">{formatTime(doc.updated_at)}</TableCell>
                  <TableCell className="text-right">
                    <IconTooltipButton
                      label="打开文档"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => navigate(`/documents/${doc.id}`)}
                    >
                      <ArrowUpRight className="size-4" />
                    </IconTooltipButton>
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={5}>
                  <EmptyState
                    icon={<FileText className="size-5" />}
                    title="暂无文档"
                    description="导入文档后会出现在这里。"
                  />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>

      {debouncedQuery.trim() ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <FileSearch className="size-4" />
              全文结果
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {searchResults.isLoading ? (
              <div className="space-y-2">
                {Array.from({ length: 2 }, (_, index) => (
                  <div key={index} className="rounded-md border px-3 py-2 shadow-sm">
                    <Skeleton className="h-4 w-48" />
                    <Skeleton className="mt-2 h-3 w-2/3" />
                  </div>
                ))}
              </div>
            ) : searchResults.data?.length ? (
              searchResults.data.map((result) => (
                <Link
                  key={`${result.text_version_id}-${result.page_id}`}
                  to={`/review/${result.document_id}/${result.page_id}`}
                  className="block rounded-md border bg-card px-3 py-2 text-sm shadow-sm transition-colors hover:border-primary/50 hover:bg-accent"
                >
                  <div className="font-medium">
                    {result.document_title} · 第 {result.page_no} 页
                  </div>
                  <div className="mt-1 line-clamp-2 text-muted-foreground">{result.snippet}</div>
                </Link>
              ))
            ) : (
              <EmptyState icon={<FileSearch className="size-5" />} title="无匹配" className="min-h-32" />
            )}
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

function ImportDocumentDialog() {
  const [open, setOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [fileKey, setFileKey] = useState(0);
  const [title, setTitle] = useState("");
  const [author, setAuthor] = useState("");
  const [source, setSource] = useState("");
  const [description, setDescription] = useState("");
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const upload = useMutation({
    mutationFn: importDocument,
    onSuccess: (doc) => {
      queryClient.invalidateQueries({ queryKey: ["documents"] });
      resetForm();
      setOpen(false);
      navigate(`/documents/${doc.id}`);
    },
  });

  function resetForm() {
    setFile(null);
    setFileKey((key) => key + 1);
    setTitle("");
    setAuthor("");
    setSource("");
    setDescription("");
  }

  function onOpenChange(nextOpen: boolean) {
    if (upload.isPending) return;
    setOpen(nextOpen);
    if (!nextOpen) upload.reset();
  }

  function submitUpload(event: FormEvent) {
    event.preventDefault();
    if (!file) return;
    upload.mutate({ file, title, author, source, description });
  }

  function updateFile(nextFile: File | null) {
    setFile(nextFile);
    if (nextFile && !title.trim()) {
      setTitle(nextFile.name.replace(/\.[^.]+$/, ""));
    }
  }

  return (
    <>
      <Button onClick={() => setOpen(true)}>
        <Upload className="size-4" />
        导入文档
      </Button>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>导入文档</DialogTitle>
            <DialogDescription>上传 PDF 或图片,系统会自动拆分页面并进入识别流程。</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={submitUpload}>
            <div className="grid gap-2">
              <Label htmlFor="document-file">文件</Label>
              <Input
                key={fileKey}
                id="document-file"
                type="file"
                accept="application/pdf,image/*"
                onChange={(event) => updateFile(event.target.files?.[0] ?? null)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="document-title">标题</Label>
              <Input id="document-title" value={title} onChange={(event) => setTitle(event.target.value)} />
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="document-author">作者</Label>
                <Input id="document-author" value={author} onChange={(event) => setAuthor(event.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="document-source">来源</Label>
                <Input id="document-source" value={source} onChange={(event) => setSource(event.target.value)} />
              </div>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="document-description">描述</Label>
              <Textarea
                id="document-description"
                className="min-h-20"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </div>
            <ErrorMessage message={upload.error?.message} />
            <DialogFooter>
              <Button type="button" variant="outline" disabled={upload.isPending} onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button type="submit" disabled={!file || upload.isPending}>
                <Upload className="size-4" />
                {upload.isPending ? "导入中" : "导入"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}

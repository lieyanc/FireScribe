import { FormEvent, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ArrowUpRight, BookOpenText, CheckCircle2, Clock3, FileSearch, FileText, FolderOpen, Search, Upload, X } from "lucide-react";
import { EmptyState, ErrorMessage, IconTooltipButton, MetricCard, PageHeader } from "../components/app/chrome";
import { StatusBadge } from "../components/app/status-badge";
import { TagChips } from "../components/app/tags";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "../components/ui/field";
import { Input } from "../components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "../components/ui/input-group";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import { importDocument, listDocuments, listTags, searchText } from "../lib/api";
import { formatBytes, formatTime } from "../lib/utils";

const statusOptions = [
  { value: "all", label: "全部状态" },
  { value: "new", label: "新建" },
  { value: "importing", label: "导入中" },
  { value: "ready", label: "就绪" },
  { value: "review_pending", label: "待校对" },
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
  const activeCount = docs.filter((doc) => ["importing", "recognizing", "review_pending", "reviewing"].includes(doc.status)).length;
  const loading = documents.isLoading;
  const hasFilters = Boolean(query.trim() || status || tag);

  function clearFilters() {
    setQuery("");
    setStatus("");
    setTag("");
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="文档库"
        description={`${loading ? "正在同步" : `${docs.length} 份文档`} · ${totalPages} 页`}
      >
        <ImportDocumentDialog />
      </PageHeader>

      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard
          icon={<BookOpenText />}
          label="当前列表"
          value={loading ? <Skeleton className="h-5 w-10" /> : docs.length}
        />
        <MetricCard
          icon={<FileText />}
          label="页面"
          value={loading ? <Skeleton className="h-5 w-10" /> : totalPages}
        />
        <MetricCard
          icon={<CheckCircle2 />}
          label="已定稿"
          value={loading ? <Skeleton className="h-5 w-10" /> : finalizedCount}
          hint={loading ? undefined : activeCount ? `${activeCount} 项处理中` : "无处理项"}
        />
      </section>

      <div className="flex flex-col gap-2 lg:flex-row">
        <InputGroup className="min-w-0 flex-1">
          <InputGroupAddon>
            <Search />
          </InputGroupAddon>
          <InputGroupInput
            aria-label="搜索文档与全文"
            placeholder="搜索标题、作者、来源或全文内容"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {query ? (
            <InputGroupAddon align="inline-end">
              <InputGroupButton size="icon-xs" aria-label="清除搜索" onClick={() => setQuery("")}>
                <X />
              </InputGroupButton>
            </InputGroupAddon>
          ) : null}
        </InputGroup>
        <Select value={status || "all"} onValueChange={(value) => setStatus(value === "all" ? "" : value)}>
          <SelectTrigger aria-label="状态筛选" className="sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {statusOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Select value={tag || "all"} onValueChange={(value) => setTag(value === "all" ? "" : value)}>
          <SelectTrigger aria-label="标签筛选" className="sm:w-40">
            <SelectValue placeholder="标签" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="all">全部标签</SelectItem>
              {(tags.data ?? []).map((item) => (
                <SelectItem key={item.id} value={item.name}>
                  {item.name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        {hasFilters ? (
          <Button type="button" variant="ghost" onClick={clearFilters}>
            <X data-icon="inline-start" />
            清除筛选
          </Button>
        ) : null}
      </div>

      <ErrorMessage message={documents.error?.message} title="文档列表加载失败" onRetry={() => void documents.refetch()} />
      <ErrorMessage message={tags.error?.message} title="标签加载失败" onRetry={() => void tags.refetch()} />

      <Card>
        <CardHeader>
          <CardTitle>文档列表</CardTitle>
          <CardDescription>
            {hasFilters ? `当前筛选得到 ${docs.length} 份文档。` : "点击任意文档继续识别、校对或导出。"}
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
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
                  className="transition-colors hover:bg-muted/50"
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
                    <StatusBadge value={doc.status} />
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
                      <ArrowUpRight />
                    </IconTooltipButton>
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={5}>
                  <EmptyState
                    icon={hasFilters ? <FileSearch /> : <FileText />}
                    title={hasFilters ? "没有符合条件的文档" : "从第一份文档开始"}
                    description={hasFilters ? "尝试调整搜索词、状态或标签。" : "导入 PDF、单张图片或整个图片文件夹。"}
                  >
                    {hasFilters ? (
                      <Button type="button" variant="outline" onClick={clearFilters}>清除筛选</Button>
                    ) : (
                      <ImportDocumentDialog label="导入第一份文档" />
                    )}
                  </EmptyState>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
          </Table>
        </CardContent>
      </Card>

      {debouncedQuery.trim() ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 [&_svg]:size-4">
              <FileSearch />
              全文结果
            </CardTitle>
            <CardDescription>在已保存的识别稿和定稿中查找“{debouncedQuery.trim()}”。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {searchResults.error ? (
              <ErrorMessage message={searchResults.error.message} title="全文搜索失败" onRetry={() => void searchResults.refetch()} />
            ) : searchResults.isLoading ? (
              <div className="flex flex-col gap-2">
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
              <EmptyState icon={<FileSearch />} title="全文中没有匹配内容" description="可以尝试更短或更常见的关键词。" className="min-h-32" />
            )}
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

function ImportDocumentDialog({ label = "导入文档" }: { label?: string }) {
  const [open, setOpen] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [fileKey, setFileKey] = useState(0);
  const [title, setTitle] = useState("");
  const [author, setAuthor] = useState("");
  const [source, setSource] = useState("");
  const [description, setDescription] = useState("");
  const folderInputRef = useRef<HTMLInputElement | null>(null);
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
    setFiles([]);
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
    if (!files.length) return;
    upload.mutate({ files, title, author, source, description });
  }

  function addFiles(incoming: FileList | null) {
    const list = incoming ? Array.from(incoming) : [];
    if (!list.length) return;
    setFiles((prev) => {
      const next = [...prev];
      for (const file of list) {
        if (!next.some((item) => fileIdentity(item) === fileIdentity(file))) {
          next.push(file);
        }
      }
      next.sort((a, b) => fileSortKey(a).localeCompare(fileSortKey(b), undefined, { numeric: true, sensitivity: "base" }));
      if (!title.trim() && next[0]) {
        setTitle(next[0].name.replace(/\.[^.]+$/, ""));
      }
      return next;
    });
    setFileKey((key) => key + 1);
  }

  function removeFile(index: number) {
    setFiles((prev) => prev.filter((_, i) => i !== index));
  }

  return (
    <>
      <Button onClick={() => setOpen(true)}>
        <Upload data-icon="inline-start" />
        {label}
      </Button>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>导入文档</DialogTitle>
            <DialogDescription>
              上传一个或多个 PDF 或图片,按上传顺序合并为同一文档;每张图片为一页,PDF 会自动拆分为多页。
            </DialogDescription>
          </DialogHeader>
          <form className="flex flex-col gap-4" onSubmit={submitUpload}>
            <FieldGroup>
              <Field data-invalid={upload.isError && !files.length ? true : undefined}>
              <FieldLabel htmlFor="document-file">文件</FieldLabel>
              <Input
                key={fileKey}
                id="document-file"
                type="file"
                multiple
                accept="application/pdf,image/*,.tif,.tiff,.bmp"
                aria-invalid={upload.isError && !files.length ? true : undefined}
                onChange={(event) => addFiles(event.target.files)}
              />
              <input
                ref={(node) => {
                  folderInputRef.current = node;
                  node?.setAttribute("webkitdirectory", "");
                  node?.setAttribute("directory", "");
                }}
                type="file"
                accept="image/*,.tif,.tiff,.bmp"
                multiple
                className="hidden"
                onChange={(event) => {
                  addFiles(event.target.files);
                  event.currentTarget.value = "";
                }}
              />
              <Button type="button" variant="outline" className="w-fit" disabled={upload.isPending} onClick={() => folderInputRef.current?.click()}>
                <FolderOpen data-icon="inline-start" />
                选择图片文件夹
              </Button>
              {files.length ? (
                <ul className="mt-1 flex flex-col gap-1.5 rounded-md border bg-muted/30 p-2">
                  {files.map((file, index) => (
                    <li key={`${file.name}-${file.size}-${file.lastModified}`} className="flex items-center gap-2 text-sm">
                      <span className="flex size-5 shrink-0 items-center justify-center rounded bg-muted text-xs tabular-nums text-muted-foreground">
                        {index + 1}
                      </span>
                      <FileText className="size-4 shrink-0 text-muted-foreground" />
                      <span className="min-w-0 flex-1 truncate" title={file.name}>
                        {file.name}
                      </span>
                      <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{formatBytes(file.size)}</span>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`移除 ${file.name}`}
                        disabled={upload.isPending}
                        onClick={() => removeFile(index)}
                      >
                        <X />
                      </Button>
                    </li>
                  ))}
                </ul>
              ) : (
                <FieldDescription>支持 PDF、常见图片格式和图片文件夹；多个文件会按名称自然排序后合并。</FieldDescription>
              )}
              </Field>
              <Field>
              <FieldLabel htmlFor="document-title">标题</FieldLabel>
              <Input id="document-title" value={title} onChange={(event) => setTitle(event.target.value)} />
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                <FieldLabel htmlFor="document-author">作者</FieldLabel>
                <Input id="document-author" value={author} onChange={(event) => setAuthor(event.target.value)} />
                </Field>
                <Field>
                <FieldLabel htmlFor="document-source">来源</FieldLabel>
                <Input id="document-source" value={source} onChange={(event) => setSource(event.target.value)} />
                </Field>
              </div>
              <Field>
              <FieldLabel htmlFor="document-description">描述</FieldLabel>
              <Textarea
                id="document-description"
                className="min-h-20"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
              </Field>
            </FieldGroup>
            <ErrorMessage message={upload.error?.message} />
            <DialogFooter>
              <Button type="button" variant="outline" disabled={upload.isPending} onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button type="submit" disabled={!files.length || upload.isPending}>
                {upload.isPending ? <Spinner data-icon="inline-start" /> : <Upload data-icon="inline-start" />}
                {upload.isPending ? "导入中" : files.length > 1 ? `导入 ${files.length} 个文件` : "导入"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}

function fileSortKey(file: File) {
  return file.webkitRelativePath || file.name;
}

function fileIdentity(file: File) {
  return `${fileSortKey(file)}\u0000${file.size}\u0000${file.lastModified}`;
}

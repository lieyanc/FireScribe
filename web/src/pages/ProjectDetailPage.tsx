import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ArrowDown, ArrowUp, Download, FilePlus2, FileText, FolderKanban, Pencil, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage, IconTooltipButton, MetricCard, PageHeader } from "../components/app/chrome";
import { StatusBadge } from "../components/app/status-badge";
import { ExportHistoryCard } from "../components/app/export-history-card";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "../components/ui/alert-dialog";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "../components/ui/field";
import { Input } from "../components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { Switch } from "../components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import { addProjectDocument, deleteProject, getProject, getProjectExport, listDocuments, patchProject, removeProjectDocument, reorderProjectDocuments, startProjectExport } from "../lib/api";

export function ProjectDetailPage() {
  const { projectID = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [documentID, setDocumentID] = useState("");
  const [format, setFormat] = useState<"md" | "txt" | "docx" | "pdf">("md");
  const [pageNumbers, setPageNumbers] = useState(true);
  const [textScope, setTextScope] = useState<"current" | "final">("current");
  const [includeAnnotations, setIncludeAnnotations] = useState(false);
  const [includeUncertain, setIncludeUncertain] = useState(false);
  const [exportID, setExportID] = useState("");
  const project = useQuery({ queryKey: ["project", projectID], queryFn: () => getProject(projectID), enabled: Boolean(projectID) });
  const documents = useQuery({ queryKey: ["documents", "project-picker"], queryFn: () => listDocuments({}), enabled: addOpen });
  const exported = useQuery({ queryKey: ["project-export", exportID], queryFn: () => getProjectExport(exportID), enabled: Boolean(exportID), refetchInterval: (query) => ["queued", "running"].includes(query.state.data?.status ?? "") ? 1000 : false });
  const refresh = () => { queryClient.invalidateQueries({ queryKey: ["project", projectID] }); queryClient.invalidateQueries({ queryKey: ["projects"] }); };
  const edit = useMutation({ mutationFn: () => patchProject(projectID, { name: name.trim(), description }), onSuccess: () => { refresh(); setEditOpen(false); toast.success("项目信息已更新"); } });
  const add = useMutation({ mutationFn: () => addProjectDocument(projectID, documentID), onSuccess: () => { refresh(); setDocumentID(""); setAddOpen(false); toast.success("文档已加入项目"); } });
  const remove = useMutation({ mutationFn: (id: string) => removeProjectDocument(projectID, id), onSuccess: refresh });
  const reorder = useMutation({ mutationFn: (ids: string[]) => reorderProjectDocuments(projectID, ids), onSuccess: refresh });
  const destroy = useMutation({ mutationFn: () => deleteProject(projectID), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["projects"] }); toast.success("项目已删除"); navigate("/projects"); } });
  const exportMutation = useMutation({ mutationFn: () => startProjectExport(projectID, { format, include_page_numbers: pageNumbers, text_scope: textScope, include_annotations: includeAnnotations, include_uncertain: includeUncertain }), onSuccess: (result) => { setExportID(result.id); queryClient.invalidateQueries({ queryKey: ["jobs"] }); queryClient.invalidateQueries({ queryKey: ["project-exports", projectID] }); toast.success("合并导出任务已创建"); } });

  useEffect(() => { if (project.data) { setName(project.data.name); setDescription(project.data.description); } }, [project.data]);
  useEffect(() => { if (exported.data?.status === "succeeded") toast.success("项目导出已完成"); }, [exported.data?.status]);
  const available = useMemo(() => (documents.data ?? []).filter((doc) => !(project.data?.documents ?? []).some((item) => item.id === doc.id)), [documents.data, project.data?.documents]);
  const item = project.data;

  function move(index: number, offset: number) {
    if (!item) return;
    const ids = item.documents.map((doc) => doc.id);
    [ids[index], ids[index + offset]] = [ids[index + offset], ids[index]];
    reorder.mutate(ids);
  }

  if (project.isLoading) return <div className="flex flex-col gap-6"><Skeleton className="h-9 w-64" /><Skeleton className="h-28 w-full" /><Skeleton className="h-80 w-full" /></div>;
  if (!item) return <ErrorMessage message={project.error?.message || "项目不存在"} title="无法打开项目" onRetry={() => void project.refetch()} />;

  return <div className="flex flex-col gap-6">
    <PageHeader title={item.name} description={item.description || "尚未填写项目说明。"}>
      <Button variant="outline" onClick={() => setEditOpen(true)}><Pencil />编辑</Button>
      <Button variant="outline" onClick={() => setDeleteOpen(true)}><Trash2 />删除</Button>
    </PageHeader>
    <section className="grid gap-3 md:grid-cols-3"><MetricCard icon={<FolderKanban />} label="文档" value={item.document_count} /><MetricCard icon={<FileText />} label="页面" value={item.page_count} /><MetricCard label="排列方式" value="手动排序" /></section>
    <ErrorMessage message={project.error?.message || edit.error?.message || add.error?.message || remove.error?.message || reorder.error?.message || exportMutation.error?.message || exported.error?.message || destroy.error?.message} />
    <Card><CardHeader className="flex flex-row items-start justify-between gap-4"><div><CardTitle>项目文档</CardTitle><CardDescription>导出时将按照下列顺序合并。</CardDescription></div><Button onClick={() => setAddOpen(true)}><FilePlus2 />添加文档</Button></CardHeader><CardContent className="p-0">
      <Table><TableHeader><TableRow><TableHead>文档</TableHead><TableHead className="w-28">状态</TableHead><TableHead className="hidden w-20 sm:table-cell">页数</TableHead><TableHead className="w-36 text-right">顺序</TableHead></TableRow></TableHeader><TableBody>
        {item.documents.length ? item.documents.map((doc, index) => <TableRow key={doc.id}><TableCell className="max-w-0"><Link className="block truncate font-medium hover:text-primary" to={`/documents/${doc.id}`}>{doc.title}</Link><div className="mt-1 text-xs text-muted-foreground">第 {index + 1} 项</div></TableCell><TableCell><StatusBadge value={doc.status} /></TableCell><TableCell className="hidden sm:table-cell">{doc.page_count}</TableCell><TableCell><div className="flex justify-end gap-1"><IconTooltipButton label="上移" variant="ghost" size="icon" disabled={index === 0 || reorder.isPending} onClick={() => move(index, -1)}><ArrowUp /></IconTooltipButton><IconTooltipButton label="下移" variant="ghost" size="icon" disabled={index === item.documents.length - 1 || reorder.isPending} onClick={() => move(index, 1)}><ArrowDown /></IconTooltipButton><IconTooltipButton label="移除" variant="ghost" size="icon" disabled={remove.isPending} onClick={() => remove.mutate(doc.id)}><X /></IconTooltipButton></div></TableCell></TableRow>) : <TableRow><TableCell colSpan={4}><EmptyState icon={<FilePlus2 />} title="项目中还没有文档" description="添加文档后即可排序并合并导出。"><Button onClick={() => setAddOpen(true)}>添加文档</Button></EmptyState></TableCell></TableRow>}
      </TableBody></Table>
    </CardContent></Card>
    <Card><CardHeader><CardTitle>合并导出</CardTitle><CardDescription>生成一个包含项目内全部文档的下载文件，所有选项会作为任务快照保存。</CardDescription></CardHeader><CardContent className="flex flex-col gap-5"><div className="grid gap-4 sm:grid-cols-2"><Field><FieldLabel>格式</FieldLabel><Select value={format} onValueChange={(value) => setFormat(value as typeof format)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="md">Markdown (.md)</SelectItem><SelectItem value="txt">纯文本 (.txt)</SelectItem><SelectItem value="docx">Word (.docx)</SelectItem><SelectItem value="pdf">PDF 审校版 (.pdf)</SelectItem></SelectContent></Select></Field><Field><FieldLabel>文本版本</FieldLabel><Select value={textScope} onValueChange={(value) => setTextScope(value as typeof textScope)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="current">当前稿</SelectItem><SelectItem value="final">仅最终定稿</SelectItem></SelectContent></Select><FieldDescription>{textScope === "final" ? "只导出各文档中已经确认定稿的页面。" : "逐页使用当前校对界面的最新有效文本。"}</FieldDescription></Field></div><div className="grid gap-3 sm:grid-cols-3"><Field orientation="horizontal" className="justify-between rounded-lg border p-3"><FieldLabel htmlFor="project-page-numbers">包含页码</FieldLabel><Switch id="project-page-numbers" checked={pageNumbers} onCheckedChange={setPageNumbers} /></Field><Field orientation="horizontal" className="justify-between rounded-lg border p-3"><div className="flex flex-col gap-1"><FieldLabel htmlFor="project-annotations">包含批注</FieldLabel><FieldDescription>附带页级和区域批注。</FieldDescription></div><Switch id="project-annotations" checked={includeAnnotations} onCheckedChange={setIncludeAnnotations} /></Field><Field orientation="horizontal" className="justify-between rounded-lg border p-3"><div className="flex flex-col gap-1"><FieldLabel htmlFor="project-uncertain">保留存疑标记</FieldLabel><FieldDescription>标出待处理字词。</FieldDescription></div><Switch id="project-uncertain" checked={includeUncertain} onCheckedChange={setIncludeUncertain} /></Field></div><div className="flex flex-wrap items-center gap-3"><Button disabled={!item.documents.length || exportMutation.isPending || ["queued", "running"].includes(exported.data?.status ?? "")} onClick={() => exportMutation.mutate()}>{exportMutation.isPending || ["queued", "running"].includes(exported.data?.status ?? "") ? <Spinner /> : <Download />}创建导出</Button>{exported.data ? <StatusBadge value={exported.data.status} /> : null}{exported.data?.status === "succeeded" && exported.data.download_url ? <Button asChild variant="outline"><a href={exported.data.download_url}><Download />下载文件</a></Button> : null}</div>{exported.data?.last_error ? <ErrorMessage message={exported.data.last_error} title="导出失败" /> : null}</CardContent></Card>
    <ExportHistoryCard scope="project" targetID={projectID} />

    <Dialog open={editOpen} onOpenChange={(open) => !edit.isPending && setEditOpen(open)}><DialogContent><form onSubmit={(e: FormEvent) => { e.preventDefault(); if (name.trim()) edit.mutate(); }}><DialogHeader><DialogTitle>编辑项目</DialogTitle><DialogDescription>更新项目名称与说明。</DialogDescription></DialogHeader><FieldGroup className="py-5"><Field><FieldLabel htmlFor="edit-project-name">名称</FieldLabel><Input id="edit-project-name" required value={name} onChange={(e) => setName(e.target.value)} /></Field><Field><FieldLabel htmlFor="edit-project-description">说明</FieldLabel><Textarea id="edit-project-description" value={description} onChange={(e) => setDescription(e.target.value)} /></Field></FieldGroup><DialogFooter><Button type="button" variant="outline" onClick={() => setEditOpen(false)}>取消</Button><Button type="submit" disabled={!name.trim() || edit.isPending}>{edit.isPending && <Spinner />}保存</Button></DialogFooter></form></DialogContent></Dialog>
    <Dialog open={addOpen} onOpenChange={(open) => !add.isPending && setAddOpen(open)}><DialogContent><DialogHeader><DialogTitle>添加文档</DialogTitle><DialogDescription>新文档会追加到当前项目末尾。</DialogDescription></DialogHeader><div className="py-5"><Field><FieldLabel>文档</FieldLabel><Select value={documentID} onValueChange={setDocumentID}><SelectTrigger><SelectValue placeholder={documents.isLoading ? "加载中…" : "选择文档"} /></SelectTrigger><SelectContent>{available.map((doc) => <SelectItem key={doc.id} value={doc.id}>{doc.title} · {doc.page_count} 页</SelectItem>)}</SelectContent></Select>{!documents.isLoading && !available.length ? <p className="text-sm text-muted-foreground">文档库中的文档都已加入此项目。</p> : null}</Field></div><DialogFooter><Button variant="outline" onClick={() => setAddOpen(false)}>取消</Button><Button disabled={!documentID || add.isPending} onClick={() => add.mutate()}>{add.isPending && <Spinner />}添加</Button></DialogFooter></DialogContent></Dialog>
    <AlertDialog open={deleteOpen} onOpenChange={(open) => !destroy.isPending && setDeleteOpen(open)}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>删除项目“{item.name}”？</AlertDialogTitle><AlertDialogDescription>项目结构和项目导出记录会被删除，原始文档不会受到影响。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction variant="destructive" disabled={destroy.isPending} onClick={(e) => { e.preventDefault(); destroy.mutate(); }}>{destroy.isPending && <Spinner />}删除项目</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
  </div>;
}

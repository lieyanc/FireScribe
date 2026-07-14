import { FormEvent, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ArrowUpRight, FolderKanban, Plus } from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage, MetricCard, PageHeader } from "../components/app/chrome";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../components/ui/dialog";
import { Field, FieldGroup, FieldLabel } from "../components/ui/field";
import { Input } from "../components/ui/input";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import { createProject, listProjects } from "../lib/api";
import { formatTime } from "../lib/utils";

export function ProjectsPage() {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const projects = useQuery({ queryKey: ["projects"], queryFn: listProjects });
  const create = useMutation({
    mutationFn: createProject,
    onSuccess: (project) => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      toast.success("项目已创建");
      setOpen(false);
      setName("");
      setDescription("");
      navigate(`/projects/${project.id}`);
    },
  });
  const items = projects.data ?? [];
  const documents = items.reduce((sum, item) => sum + item.document_count, 0);
  const pages = items.reduce((sum, item) => sum + item.page_count, 0);

  function submit(event: FormEvent) {
    event.preventDefault();
    if (name.trim()) create.mutate({ name: name.trim(), description });
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="项目" description="按顺序组织多份文档，并合并导出为一个文件。">
        <Button onClick={() => setOpen(true)}><Plus data-icon="inline-start" />新建项目</Button>
      </PageHeader>
      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard icon={<FolderKanban />} label="项目" value={projects.isLoading ? <Skeleton className="h-5 w-10" /> : items.length} />
        <MetricCard label="收录文档" value={projects.isLoading ? <Skeleton className="h-5 w-10" /> : documents} />
        <MetricCard label="合计页面" value={projects.isLoading ? <Skeleton className="h-5 w-10" /> : pages} />
      </section>
      <ErrorMessage message={projects.error?.message} title="项目列表加载失败" onRetry={() => void projects.refetch()} />
      <Card>
        <CardHeader><CardTitle>项目列表</CardTitle><CardDescription>进入项目可调整文档顺序和创建合并导出。</CardDescription></CardHeader>
        <CardContent className="p-0">
          <Table><TableHeader><TableRow><TableHead>名称</TableHead><TableHead className="w-24">文档</TableHead><TableHead className="hidden w-24 sm:table-cell">页数</TableHead><TableHead className="hidden w-36 md:table-cell">更新</TableHead><TableHead className="w-16" /></TableRow></TableHeader>
            <TableBody>
              {projects.isLoading ? Array.from({ length: 3 }, (_, i) => <TableRow key={i}><TableCell><Skeleton className="h-4 w-40" /></TableCell><TableCell><Skeleton className="h-4 w-8" /></TableCell><TableCell className="hidden sm:table-cell"><Skeleton className="h-4 w-8" /></TableCell><TableCell className="hidden md:table-cell"><Skeleton className="h-4 w-24" /></TableCell><TableCell /></TableRow>) : items.length ? items.map((project) => (
                <TableRow key={project.id}>
                  <TableCell className="max-w-0"><Link className="block truncate font-medium hover:text-primary" to={`/projects/${project.id}`}>{project.name}</Link>{project.description ? <div className="mt-1 truncate text-xs text-muted-foreground">{project.description}</div> : null}</TableCell>
                  <TableCell>{project.document_count}</TableCell><TableCell className="hidden sm:table-cell">{project.page_count}</TableCell><TableCell className="hidden text-muted-foreground md:table-cell">{formatTime(project.updated_at) || "--"}</TableCell>
                  <TableCell><Button asChild variant="ghost" size="icon"><Link aria-label={`打开 ${project.name}`} to={`/projects/${project.id}`}><ArrowUpRight /></Link></Button></TableCell>
                </TableRow>
              )) : <TableRow><TableCell colSpan={5}><EmptyState icon={<FolderKanban />} title="暂无项目" description="新建项目，将相关文档组织成有序合集。"><Button onClick={() => setOpen(true)}><Plus data-icon="inline-start" />新建项目</Button></EmptyState></TableCell></TableRow>}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <Dialog open={open} onOpenChange={(value) => !create.isPending && setOpen(value)}>
        <DialogContent><form onSubmit={submit}><DialogHeader><DialogTitle>新建项目</DialogTitle><DialogDescription>项目可容纳多份文档，并按照指定顺序合并导出。</DialogDescription></DialogHeader>
          <FieldGroup className="py-5"><Field><FieldLabel htmlFor="project-name">名称</FieldLabel><Input id="project-name" autoFocus required value={name} onChange={(e) => setName(e.target.value)} /></Field><Field><FieldLabel htmlFor="project-description">说明</FieldLabel><Textarea id="project-description" value={description} onChange={(e) => setDescription(e.target.value)} /></Field></FieldGroup>
          <ErrorMessage message={create.error?.message} />
          <DialogFooter className="mt-4"><Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={create.isPending}>取消</Button><Button type="submit" disabled={!name.trim() || create.isPending}>{create.isPending ? <Spinner data-icon="inline-start" /> : <Plus data-icon="inline-start" />}创建</Button></DialogFooter>
        </form></DialogContent>
      </Dialog>
    </div>
  );
}

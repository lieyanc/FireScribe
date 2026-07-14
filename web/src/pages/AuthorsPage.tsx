import { FormEvent, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ArrowUpRight, Plus, Signature } from "lucide-react";
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
import { createAuthorProfile, listAuthorProfiles } from "../lib/api";

export function AuthorsPage() {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [notes, setNotes] = useState("");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const profiles = useQuery({ queryKey: ["author-profiles"], queryFn: listAuthorProfiles });
  const create = useMutation({
    mutationFn: createAuthorProfile,
    onSuccess: (profile) => {
      queryClient.invalidateQueries({ queryKey: ["author-profiles"] });
      toast.success("作者档案已创建");
      navigate(`/authors/${profile.id}`);
    },
  });
  const items = profiles.data ?? [];

  function submit(event: FormEvent) {
    event.preventDefault();
    if (name.trim()) create.mutate({ name: name.trim(), notes });
  }

  return <div className="flex flex-col gap-6">
    <PageHeader title="作者档案" description="积累作者笔迹特征、专有词和历史校对样本，并在识别时自动注入上下文。">
      <Button onClick={() => setOpen(true)}><Plus data-icon="inline-start" />新建档案</Button>
    </PageHeader>
    <section className="grid gap-3 md:grid-cols-3">
      <MetricCard icon={<Signature />} label="作者" value={profiles.isLoading ? <Skeleton className="h-5 w-10" /> : items.length} />
      <MetricCard label="关联文档" value={items.reduce((sum, item) => sum + item.document_count, 0)} />
      <MetricCard label="训练样本" value={items.reduce((sum, item) => sum + item.correction_count, 0)} />
    </section>
    <ErrorMessage message={profiles.error?.message} title="作者档案加载失败" onRetry={() => void profiles.refetch()} />
    <Card><CardHeader><CardTitle>档案列表</CardTitle><CardDescription>进入档案可维护词表、关联文档并下载 JSONL 训练数据。</CardDescription></CardHeader><CardContent className="p-0">
      <Table><TableHeader><TableRow><TableHead>作者</TableHead><TableHead className="w-24">文档</TableHead><TableHead className="w-24">词条</TableHead><TableHead className="w-24">样本</TableHead><TableHead className="w-16" /></TableRow></TableHeader><TableBody>
        {profiles.isLoading ? Array.from({ length: 3 }, (_, index) => <TableRow key={index}><TableCell><Skeleton className="h-4 w-36" /></TableCell><TableCell><Skeleton className="h-4 w-8" /></TableCell><TableCell><Skeleton className="h-4 w-8" /></TableCell><TableCell><Skeleton className="h-4 w-8" /></TableCell><TableCell /></TableRow>) : items.length ? items.map((profile) => <TableRow key={profile.id}>
          <TableCell className="max-w-0"><Link className="block truncate font-medium hover:text-primary" to={`/authors/${profile.id}`}>{profile.name}</Link>{profile.notes ? <div className="mt-1 truncate text-xs text-muted-foreground">{profile.notes}</div> : null}</TableCell>
          <TableCell>{profile.document_count}</TableCell><TableCell>{profile.term_count}</TableCell><TableCell>{profile.correction_count}</TableCell>
          <TableCell><Button asChild variant="ghost" size="icon"><Link aria-label={`打开 ${profile.name}`} to={`/authors/${profile.id}`}><ArrowUpRight /></Link></Button></TableCell>
        </TableRow>) : <TableRow><TableCell colSpan={5}><EmptyState icon={<Signature />} title="暂无作者档案" description="为常见作者建立档案，让后续识别逐步利用已确认的词汇和校对经验。"><Button onClick={() => setOpen(true)}><Plus data-icon="inline-start" />新建档案</Button></EmptyState></TableCell></TableRow>}
      </TableBody></Table>
    </CardContent></Card>
    <Dialog open={open} onOpenChange={(value) => !create.isPending && setOpen(value)}><DialogContent><form onSubmit={submit}><DialogHeader><DialogTitle>新建作者档案</DialogTitle><DialogDescription>作者名用于档案检索；说明可记录年代、书写习惯或资料背景。</DialogDescription></DialogHeader><FieldGroup className="py-5"><Field><FieldLabel htmlFor="author-name">作者名</FieldLabel><Input id="author-name" autoFocus required value={name} onChange={(event) => setName(event.target.value)} /></Field><Field><FieldLabel htmlFor="author-notes">档案说明</FieldLabel><Textarea id="author-notes" value={notes} onChange={(event) => setNotes(event.target.value)} /></Field></FieldGroup><ErrorMessage message={create.error?.message} /><DialogFooter><Button type="button" variant="outline" onClick={() => setOpen(false)}>取消</Button><Button type="submit" disabled={!name.trim() || create.isPending}>{create.isPending ? <Spinner data-icon="inline-start" /> : <Plus data-icon="inline-start" />}创建</Button></DialogFooter></form></DialogContent></Dialog>
  </div>;
}

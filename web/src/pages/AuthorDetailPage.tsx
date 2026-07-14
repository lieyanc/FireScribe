import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Download, FilePlus2, RefreshCw, Save, Signature, Trash2, Unlink } from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage, MetricCard, PageHeader } from "../components/app/chrome";
import { AuthorRecognitionMetricsPanel } from "../components/app/author-recognition-metrics";
import { StatusBadge } from "../components/app/status-badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Field, FieldGroup, FieldLabel } from "../components/ui/field";
import { Input } from "../components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Textarea } from "../components/ui/textarea";
import {
  createAuthorTerm,
  deleteAuthorProfile,
  deleteAuthorTerm,
  getAuthorRecognitionMetrics,
  getAuthorProfile,
  listAuthorCorrections,
  listAuthorProfileDocuments,
  listAuthorTerms,
  listDocuments,
  patchAuthorProfile,
  setDocumentAuthorProfile,
  syncAuthorCorrections,
} from "../lib/api";

export function AuthorDetailPage() {
  const { profileID = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [notes, setNotes] = useState("");
  const [documentID, setDocumentID] = useState("");
  const [term, setTerm] = useState("");
  const [replacement, setReplacement] = useState("");
  const [termNote, setTermNote] = useState("");
  const [weight, setWeight] = useState("1");
  const profile = useQuery({ queryKey: ["author-profile", profileID], queryFn: () => getAuthorProfile(profileID), enabled: Boolean(profileID) });
  const terms = useQuery({ queryKey: ["author-terms", profileID], queryFn: () => listAuthorTerms(profileID), enabled: Boolean(profileID) });
  const linked = useQuery({ queryKey: ["author-documents", profileID], queryFn: () => listAuthorProfileDocuments(profileID), enabled: Boolean(profileID) });
  const documents = useQuery({ queryKey: ["documents", "author-picker"], queryFn: () => listDocuments({}) });
  const corrections = useQuery({ queryKey: ["author-corrections", profileID], queryFn: () => listAuthorCorrections(profileID), enabled: Boolean(profileID) });
  const metrics = useQuery({ queryKey: ["author-metrics", profileID], queryFn: () => getAuthorRecognitionMetrics(profileID), enabled: Boolean(profileID) });
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ["author-profile", profileID] });
    queryClient.invalidateQueries({ queryKey: ["author-profiles"] });
    queryClient.invalidateQueries({ queryKey: ["author-documents", profileID] });
    queryClient.invalidateQueries({ queryKey: ["author-corrections", profileID] });
    queryClient.invalidateQueries({ queryKey: ["author-metrics", profileID] });
  };
  const save = useMutation({ mutationFn: () => patchAuthorProfile(profileID, { name: name.trim(), notes }), onSuccess: () => { refresh(); toast.success("档案已保存"); } });
  const addTerm = useMutation({ mutationFn: () => createAuthorTerm(profileID, { term: term.trim(), replacement: replacement.trim(), note: termNote.trim(), weight: Number(weight) || 1 }), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["author-terms", profileID] }); refresh(); setTerm(""); setReplacement(""); setTermNote(""); setWeight("1"); toast.success("词条已添加"); } });
  const removeTerm = useMutation({ mutationFn: deleteAuthorTerm, onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["author-terms", profileID] }); refresh(); } });
  const associate = useMutation({ mutationFn: (input: { documentID: string; profileID: string }) => setDocumentAuthorProfile(input.documentID, input.profileID), onSuccess: () => { refresh(); setDocumentID(""); toast.success("文档关联已更新，历史校对样本已同步"); } });
  const sync = useMutation({ mutationFn: () => syncAuthorCorrections(profileID), onSuccess: (result) => { refresh(); toast.success(result.added ? `新增 ${result.added} 条训练样本` : "训练样本已是最新"); } });
  const destroy = useMutation({ mutationFn: () => deleteAuthorProfile(profileID), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["author-profiles"] }); navigate("/authors"); } });

  useEffect(() => { if (profile.data) { setName(profile.data.name); setNotes(profile.data.notes); } }, [profile.data]);
  const available = useMemo(() => (documents.data ?? []).filter((doc) => !(linked.data ?? []).some((item) => item.id === doc.id)), [documents.data, linked.data]);

  if (profile.isLoading) return <div className="flex flex-col gap-6"><Skeleton className="h-9 w-64" /><Skeleton className="h-32 w-full" /><Skeleton className="h-80 w-full" /></div>;
  if (!profile.data) return <ErrorMessage title="无法打开作者档案" message={profile.error?.message || "作者档案不存在"} />;
  const item = profile.data;
  const error = profile.error?.message || terms.error?.message || linked.error?.message || documents.error?.message || corrections.error?.message || metrics.error?.message || save.error?.message || addTerm.error?.message || removeTerm.error?.message || associate.error?.message || sync.error?.message || destroy.error?.message;

  function submitTerm(event: FormEvent) {
    event.preventDefault();
    if (term.trim()) addTerm.mutate();
  }

  return <div className="flex flex-col gap-6">
    <PageHeader title={item.name} description="作者笔迹档案会作为可审计的识别上下文快照写入每次 OCR run。">
      <Button asChild variant="outline"><a href={`/api/author-profiles/${profileID}/training-data`}><Download data-icon="inline-start" />下载 JSONL</a></Button>
      <Button variant="outline" disabled={sync.isPending} onClick={() => sync.mutate()}>{sync.isPending ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}同步样本</Button>
    </PageHeader>
    <section className="grid gap-3 md:grid-cols-3"><MetricCard icon={<Signature />} label="关联文档" value={item.document_count} /><MetricCard label="专有词/误识别" value={item.term_count} /><MetricCard label="校对训练样本" value={item.correction_count} /></section>
    <ErrorMessage message={error} />
    <AuthorRecognitionMetricsPanel metrics={metrics.data} loading={metrics.isLoading} />
    <Card><CardHeader><CardTitle>档案信息</CardTitle><CardDescription>说明中可记录笔迹风格、常用称谓和时代背景；只应写可靠信息。</CardDescription></CardHeader><CardContent><FieldGroup><Field><FieldLabel htmlFor="profile-name">作者名</FieldLabel><Input id="profile-name" value={name} onChange={(event) => setName(event.target.value)} /></Field><Field><FieldLabel htmlFor="profile-notes">档案说明</FieldLabel><Textarea id="profile-notes" value={notes} onChange={(event) => setNotes(event.target.value)} /></Field><div className="flex justify-between gap-3"><Button variant="destructive" disabled={destroy.isPending} onClick={() => { if (window.confirm(`删除作者档案“${item.name}”？关联文档不会被删除。`)) destroy.mutate(); }}><Trash2 data-icon="inline-start" />删除档案</Button><Button disabled={!name.trim() || save.isPending} onClick={() => save.mutate()}>{save.isPending ? <Spinner data-icon="inline-start" /> : <Save data-icon="inline-start" />}保存</Button></div></FieldGroup></CardContent></Card>
    <Card><CardHeader><CardTitle>专有词与常见误识别</CardTitle><CardDescription>“正确词”会提示识别器优先核对；可选填写模型常误识别成的字词和说明。</CardDescription></CardHeader><CardContent className="flex flex-col gap-5"><form className="grid gap-3 md:grid-cols-[1fr_1fr_1fr_7rem_auto]" onSubmit={submitTerm}><Input aria-label="正确词" placeholder="正确词/专名" value={term} onChange={(event) => setTerm(event.target.value)} /><Input aria-label="常见误识别" placeholder="常见误识别（可选）" value={replacement} onChange={(event) => setReplacement(event.target.value)} /><Input aria-label="说明" placeholder="说明（可选）" value={termNote} onChange={(event) => setTermNote(event.target.value)} /><Input aria-label="权重" type="number" min="0.1" max="100" step="0.1" value={weight} onChange={(event) => setWeight(event.target.value)} /><Button type="submit" disabled={!term.trim() || addTerm.isPending}>{addTerm.isPending && <Spinner data-icon="inline-start" />}添加</Button></form>
      {(terms.data ?? []).length ? <Table><TableHeader><TableRow><TableHead>正确词</TableHead><TableHead>常见误识别</TableHead><TableHead>说明</TableHead><TableHead className="w-20">权重</TableHead><TableHead className="w-16" /></TableRow></TableHeader><TableBody>{(terms.data ?? []).map((entry) => <TableRow key={entry.id}><TableCell className="font-medium">{entry.term}</TableCell><TableCell>{entry.replacement || "--"}</TableCell><TableCell className="text-muted-foreground">{entry.note || "--"}</TableCell><TableCell>{entry.weight}</TableCell><TableCell><Button variant="ghost" size="icon" aria-label={`删除 ${entry.term}`} onClick={() => removeTerm.mutate(entry.id)}><Trash2 /></Button></TableCell></TableRow>)}</TableBody></Table> : <EmptyState icon={<Signature />} title="还没有词条" description="添加专有名词或已知的常见误识别，下一次识别会自动使用。" />}
    </CardContent></Card>
    <Card><CardHeader><CardTitle>关联文档</CardTitle><CardDescription>关联后，新增人工稿/定稿会自动积累为训练样本；已有版本也会立即回填。</CardDescription></CardHeader><CardContent className="flex flex-col gap-4"><div className="flex gap-3"><Select value={documentID} onValueChange={setDocumentID}><SelectTrigger className="max-w-xl"><SelectValue placeholder="选择文档" /></SelectTrigger><SelectContent>{available.map((doc) => <SelectItem key={doc.id} value={doc.id}>{doc.title} · {doc.author || "未填作者"}</SelectItem>)}</SelectContent></Select><Button disabled={!documentID || associate.isPending} onClick={() => associate.mutate({ documentID, profileID })}>{associate.isPending ? <Spinner data-icon="inline-start" /> : <FilePlus2 data-icon="inline-start" />}关联</Button></div>
      {(linked.data ?? []).length ? <Table><TableHeader><TableRow><TableHead>文档</TableHead><TableHead className="w-28">状态</TableHead><TableHead className="w-20">页数</TableHead><TableHead className="w-16" /></TableRow></TableHeader><TableBody>{(linked.data ?? []).map((doc) => <TableRow key={doc.id}><TableCell><Link className="font-medium hover:text-primary" to={`/documents/${doc.id}`}>{doc.title}</Link></TableCell><TableCell><StatusBadge value={doc.status} /></TableCell><TableCell>{doc.page_count}</TableCell><TableCell><Button variant="ghost" size="icon" aria-label={`取消关联 ${doc.title}`} onClick={() => associate.mutate({ documentID: doc.id, profileID: "" })}><Unlink /></Button></TableCell></TableRow>)}</TableBody></Table> : <EmptyState icon={<FilePlus2 />} title="尚未关联文档" description="从文档库选择属于这位作者的资料。" />}
    </CardContent></Card>
    <Card><CardHeader><CardTitle>最近训练样本</CardTitle><CardDescription>JSONL 下载包含页面图像 URL、识别 provider/model/prompt 和校对前后文本。</CardDescription></CardHeader><CardContent className="p-0"><Table><TableHeader><TableRow><TableHead>文档 / 页</TableHead><TableHead>识别原文</TableHead><TableHead>校对结果</TableHead><TableHead className="hidden w-40 md:table-cell">来源模型</TableHead></TableRow></TableHeader><TableBody>{(corrections.data ?? []).length ? (corrections.data ?? []).map((entry) => <TableRow key={entry.id}><TableCell className="align-top"><Link className="font-medium hover:text-primary" to={`/review/${entry.document_id}/${entry.page_id}`}>{entry.document_title} · 第 {entry.page_no} 页</Link></TableCell><TableCell className="max-w-xs align-top"><p className="line-clamp-3 whitespace-pre-wrap text-xs text-muted-foreground">{entry.source_text}</p></TableCell><TableCell className="max-w-xs align-top"><p className="line-clamp-3 whitespace-pre-wrap text-xs">{entry.corrected_text}</p></TableCell><TableCell className="hidden align-top text-xs text-muted-foreground md:table-cell">{entry.provider || "--"}<br />{entry.model || "--"}</TableCell></TableRow>) : <TableRow><TableCell colSpan={4}><EmptyState icon={<RefreshCw />} title="暂无训练样本" description="完成关联文档的人工校对或定稿后，样本会自动出现在这里。" /></TableCell></TableRow>}</TableBody></Table></CardContent></Card>
  </div>;
}

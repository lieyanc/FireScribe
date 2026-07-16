import { useEffect, useMemo, useState, type ComponentProps } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Ban, CheckCheck, GitCompareArrows, Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage } from "@/components/app/chrome";
import { StatusBadge } from "@/components/app/status-badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  adoptCrossCheck,
  cancelJob,
  getCrossCheck,
  getJob,
  listCrossChecks,
  listPromptVersions,
  listProviderAdapters,
  listRecognizerProfiles,
  startCrossCheck,
  type CrossCheck,
  type CrossCheckAdoption,
  type CrossCheckConflict,
  type CrossCheckInput,
  type CrossCheckPage,
  type CrossCheckVariant,
  type PageDetail,
} from "@/lib/api";
import { formatTime } from "@/lib/format";
import { parsePageSpec } from "@/lib/pages";
import { cn } from "@/lib/utils";

const ACTIVE_CROSS_CHECK_STATUSES = new Set(["queued", "running"]);

const CONFLICT_KIND_LABELS: Record<CrossCheckConflict["kind"], string> = {
  omitted: "合并稿未收录",
  partial: "部分模型缺失",
  divergent: "模型间分歧",
};

const PAGE_STATUS_BADGES: Record<
  CrossCheckPage["status"],
  { label: string; variant: ComponentProps<typeof Badge>["variant"] }
> = {
  pending: { label: "待处理", variant: "secondary" },
  consensus: { label: "一致", variant: "success" },
  disagreement: { label: "分歧", variant: "warning" },
  failed: { label: "失败", variant: "destructive" },
  canceled: { label: "已取消", variant: "destructive" },
};

function CrossCheckPageStatusBadge({ status }: { status: CrossCheckPage["status"] }) {
  const config = PAGE_STATUS_BADGES[status] ?? { label: status, variant: "outline" as const };
  return <Badge variant={config.variant}>{config.label}</Badge>;
}

function agreementLabel(value: number | null) {
  if (value === null || value === undefined || Number.isNaN(value)) return "--";
  return `${(value * 100).toFixed(1)}%`;
}

function conflictKindLabel(kind: string) {
  return CONFLICT_KIND_LABELS[kind as CrossCheckConflict["kind"]] ?? kind;
}

function variantSourceLabel(variant: CrossCheckVariant, profileNames: Map<string, string>, adapterNames: Map<string, string>) {
  if (variant.provider_adapter_id) return adapterNames.get(variant.provider_adapter_id) ?? "Provider Adapter";
  if (variant.recognizer_profile_id) return profileNames.get(variant.recognizer_profile_id) ?? "模型";
  return "默认模型";
}

// 页可以直接采纳 = 全体一致、尚未采纳、也没有人工版本(人工工作永远不被覆盖)。
function pageAdoptable(page: CrossCheckPage) {
  return (
    page.status === "consensus" &&
    !page.adopted_version_id &&
    page.effective_kind !== "manual" &&
    page.effective_kind !== "final"
  );
}

type VariantDraft = {
  key: string;
  name: string;
  source: string;
  promptVersionID: string;
  imageSource: "original" | "enhanced";
};

function newVariant(): VariantDraft {
  return {
    key: crypto.randomUUID(),
    name: "",
    source: "",
    promptVersionID: "active",
    imageSource: "original",
  };
}

export function CrossCheckCard({ documentID, pages }: { documentID: string; pages: PageDetail[] }) {
  const queryClient = useQueryClient();
  const profiles = useQuery({ queryKey: ["recognizer-profiles"], queryFn: listRecognizerProfiles });
  const adapters = useQuery({ queryKey: ["provider-adapters"], queryFn: listProviderAdapters });
  const prompts = useQuery({ queryKey: ["prompt-versions"], queryFn: listPromptVersions });
  const checks = useQuery({
    queryKey: ["cross-checks", documentID],
    queryFn: () => listCrossChecks(documentID),
    enabled: Boolean(documentID),
    refetchInterval: (query) => query.state.data?.some((item) => ACTIVE_CROSS_CHECK_STATUSES.has(item.status)) ? 1500 : false,
  });
  const [selectedID, setSelectedID] = useState("");
  const [name, setName] = useState("");
  const [pageScope, setPageScope] = useState<"all" | "custom">("all");
  const [pageSpec, setPageSpec] = useState("1");
  const [mergeProfileID, setMergeProfileID] = useState("default");
  const [variants, setVariants] = useState<VariantDraft[]>(() => [newVariant(), newVariant()]);
  const [adoptAllOpen, setAdoptAllOpen] = useState(false);
  const [bulkAdoption, setBulkAdoption] = useState<CrossCheckAdoption | null>(null);

  useEffect(() => {
    if (!checks.data?.length) return;
    setSelectedID((current) => checks.data.some((item) => item.id === current) ? current : checks.data[0].id);
  }, [checks.data]);

  useEffect(() => {
    setBulkAdoption(null);
  }, [selectedID]);

  const selectedCheck = useQuery({
    queryKey: ["cross-check", selectedID],
    queryFn: () => getCrossCheck(selectedID),
    enabled: Boolean(selectedID),
    refetchInterval: (query) => ACTIVE_CROSS_CHECK_STATUSES.has(query.state.data?.status ?? "") ? 1200 : false,
  });

  const detail = selectedCheck.data;
  const detailActive = detail ? ACTIVE_CROSS_CHECK_STATUSES.has(detail.status) : false;
  const jobID = detail?.job_id ?? "";
  const job = useQuery({
    queryKey: ["job", jobID],
    queryFn: () => getJob(jobID),
    enabled: Boolean(jobID) && detailActive,
    refetchInterval: 1500,
  });

  const enabledAdapters = (adapters.data ?? []).filter((adapter) => adapter.is_enabled);
  const pageSelection = useMemo(
    () => pageScope === "custom" ? parsePageSpec(pageSpec, pages) : { pageIDs: [] as string[], error: "" },
    [pageScope, pageSpec, pages],
  );
  const profileNames = useMemo(() => new Map((profiles.data ?? []).map((profile) => [profile.id, profile.name])), [profiles.data]);
  const adapterNames = useMemo(() => new Map((adapters.data ?? []).map((adapter) => [adapter.id, adapter.name])), [adapters.data]);
  const trimmedNames = variants.map((variant) => variant.name.trim());
  // 名称留空由后端按 Profile/Adapter 自动命名(自动重名会加后缀);仅显式填写的重名会被后端拒绝,提前拦下。
  const duplicateName = trimmedNames.some((value, index) => value && trimmedNames.indexOf(value) !== index);
  const validVariants =
    variants.length >= 2 &&
    variants.length <= 8 &&
    !duplicateName &&
    variants.every((variant) => variant.source);
  const canCreate = validVariants && !pageSelection.error;

  const createCheck = useMutation({
    mutationFn: () => {
      const input: CrossCheckInput = {
        name: name.trim() || undefined,
        page_ids: pageScope === "custom" ? pageSelection.pageIDs : undefined,
        merge_profile_id: mergeProfileID === "default" ? undefined : mergeProfileID,
        variants: variants.map((variant) => {
          const [kind, id] = variant.source.split(":", 2);
          return {
            name: variant.name.trim() || undefined,
            recognizer_profile_id: kind === "profile" ? id : undefined,
            provider_adapter_id: kind === "adapter" ? id : undefined,
            prompt_version_id: variant.promptVersionID === "active" ? undefined : variant.promptVersionID,
            image_source: variant.imageSource,
          };
        }),
      };
      return startCrossCheck(documentID, input);
    },
    onSuccess: async ({ cross_check }) => {
      setSelectedID(cross_check.id);
      setName("");
      await queryClient.invalidateQueries({ queryKey: ["cross-checks", documentID] });
      toast.success("交叉核验已开始", { description: "识别与比对会在后台运行，完成后可逐页拍板。" });
    },
    onError: (error: Error) => toast.error("创建交叉核验失败", { description: error.message }),
  });

  const cancel = useMutation({
    mutationFn: (jobID: string) => cancelJob(jobID),
    onSuccess: () => {
      toast("正在取消交叉核验");
      queryClient.invalidateQueries({ queryKey: ["cross-checks", documentID] });
      if (selectedID) queryClient.invalidateQueries({ queryKey: ["cross-check", selectedID] });
    },
    onError: (error: Error) => toast.error("取消失败", { description: error.message }),
  });

  const adopt = useMutation({
    mutationFn: ({ checkID, pageIDs }: { checkID: string; pageIDs?: string[] }) => adoptCrossCheck(checkID, pageIDs),
    onSuccess: (adoption, variables) => {
      queryClient.setQueryData(["cross-check", variables.checkID], adoption.cross_check);
      queryClient.invalidateQueries({ queryKey: ["cross-checks", documentID] });
      queryClient.invalidateQueries({ queryKey: ["pages", documentID] });
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
      if (variables.pageIDs?.length) {
        if (adoption.adopted_page_ids.length) {
          toast.success("已采纳为定稿");
        } else if (adoption.skipped[0]) {
          toast.error("该页未被采纳", { description: adoption.skipped[0].reason });
        }
        return;
      }
      setBulkAdoption(adoption);
      setAdoptAllOpen(false);
      if (adoption.adopted_page_ids.length) {
        toast.success(`已采纳 ${adoption.adopted_page_ids.length} 页一致页`);
      } else {
        toast.info("没有可直接采纳的页", { description: "详见下方采纳结果。" });
      }
    },
    onError: (error: Error) => {
      setAdoptAllOpen(false);
      toast.error("采纳失败", { description: error.message });
    },
  });

  function updateVariant(key: string, patch: Partial<VariantDraft>) {
    setVariants((current) => current.map((variant) => variant.key === key ? { ...variant, ...patch } : variant));
  }

  const jobProgress = job.data && job.data.progress_total > 0
    ? Math.round((job.data.progress_current / job.data.progress_total) * 100)
    : 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="inline-flex items-center gap-2">
          <GitCompareArrows className="size-5" />
          多模型交叉核验
        </CardTitle>
        <CardDescription>
          多个 Profile 依次识别同一批页面并逐页比对：全部一致的页可一键采纳为定稿，出现分歧的页会自动生成保守合并稿并进入审校队列，最终由你逐页拍板。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <FieldGroup>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="cross-check-name">核验名称（可选）</FieldLabel>
              <Input
                id="cross-check-name"
                placeholder="如 双模型互校"
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="cross-check-merger">合并识别器 Profile</FieldLabel>
              <Select value={mergeProfileID} onValueChange={setMergeProfileID}>
                <SelectTrigger id="cross-check-merger"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="default">使用默认 Profile</SelectItem>
                    {profiles.data?.map((profile) => (
                      <SelectItem key={profile.id} value={profile.id}>{profile.name}</SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>分歧页会用该 Profile 生成保守合并稿。</FieldDescription>
            </Field>
          </FieldGroup>

          <Field data-invalid={pageScope === "custom" && Boolean(pageSelection.error)}>
            <FieldLabel>页面范围</FieldLabel>
            <div className="flex flex-wrap items-center gap-2">
              <ToggleGroup
                type="single"
                variant="outline"
                size="sm"
                value={pageScope}
                onValueChange={(value) => value && setPageScope(value as "all" | "custom")}
                aria-label="页面范围"
              >
                <ToggleGroupItem value="all">全部页</ToggleGroupItem>
                <ToggleGroupItem value="custom">选择页</ToggleGroupItem>
              </ToggleGroup>
              {pageScope === "custom" ? (
                <Input
                  className="w-44"
                  aria-label="页码"
                  aria-invalid={Boolean(pageSelection.error)}
                  placeholder="1,3-5"
                  value={pageSpec}
                  onChange={(event) => setPageSpec(event.target.value)}
                />
              ) : null}
            </div>
            <FieldDescription>
              {pageScope === "custom" ? "支持逗号和范围，例如 1,3-5；所有模型使用相同页面。" : "对文档全部页面执行交叉核验。"}
            </FieldDescription>
            <FieldError>{pageScope === "custom" ? pageSelection.error : ""}</FieldError>
          </Field>

          <div className="flex items-center justify-between gap-3">
            <div className="text-sm font-medium">参与模型（2–8 个）</div>
            <Button
              variant="outline"
              size="sm"
              disabled={variants.length >= 8}
              onClick={() => setVariants((current) => [...current, newVariant()])}
            >
              <Plus />
              添加模型
            </Button>
          </div>

          <div className="flex flex-col gap-3">
            {variants.map((variant, index) => (
              <FieldGroup key={variant.key} className="rounded-lg border p-4">
                <div className="flex items-center justify-between gap-3">
                  <Badge variant="secondary">模型 {index + 1}</Badge>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`删除 ${variant.name || `模型 ${index + 1}`}`}
                    disabled={variants.length <= 2}
                    onClick={() => setVariants((current) => current.filter((item) => item.key !== variant.key))}
                  >
                    <Trash2 />
                  </Button>
                </div>
                <FieldGroup className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                  <Field>
                    <FieldLabel htmlFor={`${variant.key}-name`}>名称（可选）</FieldLabel>
                    <Input
                      id={`${variant.key}-name`}
                      placeholder="留空自动命名"
                      value={variant.name}
                      onChange={(event) => updateVariant(variant.key, { name: event.target.value })}
                    />
                  </Field>
                  <Field data-invalid={!variant.source}>
                    <FieldLabel htmlFor={`${variant.key}-source`}>Profile / Adapter</FieldLabel>
                    <Select value={variant.source} onValueChange={(source) => updateVariant(variant.key, { source })}>
                      <SelectTrigger id={`${variant.key}-source`} aria-invalid={!variant.source}>
                        <SelectValue placeholder="选择识别来源" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectLabel>模型</SelectLabel>
                          {profiles.data?.map((profile) => (
                            <SelectItem key={profile.id} value={`profile:${profile.id}`}>
                              {profile.provider_name ? `${profile.provider_name} · ` : ""}{profile.name}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                        <SelectGroup>
                          <SelectLabel>通用 HTTP 适配器</SelectLabel>
                          {enabledAdapters.map((adapter) => (
                            <SelectItem key={adapter.id} value={`adapter:${adapter.id}`}>{adapter.name}</SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    {!profiles.isLoading && !adapters.isLoading && !profiles.data?.length && !enabledAdapters.length ? (
                      <FieldError>请先在设置中创建 Profile 或启用 Adapter。</FieldError>
                    ) : null}
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`${variant.key}-prompt`}>Prompt</FieldLabel>
                    <Select
                      value={variant.promptVersionID}
                      onValueChange={(promptVersionID) => updateVariant(variant.key, { promptVersionID })}
                    >
                      <SelectTrigger id={`${variant.key}-prompt`}><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value="active">Profile 默认 / 激活版本</SelectItem>
                          {prompts.data?.map((prompt) => (
                            <SelectItem key={prompt.id} value={prompt.id}>{prompt.version}</SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`${variant.key}-image-source`}>图像来源</FieldLabel>
                    <Select
                      value={variant.imageSource}
                      onValueChange={(imageSource) => updateVariant(variant.key, { imageSource: imageSource as VariantDraft["imageSource"] })}
                    >
                      <SelectTrigger id={`${variant.key}-image-source`}><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value="original">原图</SelectItem>
                          <SelectItem value="enhanced">增强图</SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                </FieldGroup>
              </FieldGroup>
            ))}
          </div>

          {duplicateName ? <div className="text-sm text-destructive">自定义的模型名称需互不相同；留空将自动命名。</div> : null}
          <ErrorMessage message={createCheck.error?.message} />
          <Button className="self-start" disabled={!canCreate || createCheck.isPending} onClick={() => createCheck.mutate()}>
            {createCheck.isPending ? <Spinner /> : <GitCompareArrows />}
            {createCheck.isPending ? "创建中" : "创建并运行"}
          </Button>
        </FieldGroup>

        <div className="grid items-start gap-4 xl:grid-cols-[minmax(16rem,0.65fr)_minmax(0,1.35fr)]">
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center justify-between gap-2">
              <div className="text-sm font-medium">历史核验</div>
              <Button variant="ghost" size="icon-sm" aria-label="刷新核验" onClick={() => void checks.refetch()}>
                {checks.isFetching ? <Spinner /> : <RefreshCw />}
              </Button>
            </div>
            <ErrorMessage message={checks.error?.message} />
            {checks.data?.map((check) => (
              <Button
                key={check.id}
                variant={selectedID === check.id ? "secondary" : "ghost"}
                className="h-auto justify-between gap-3 px-3 py-2 text-left"
                onClick={() => setSelectedID(check.id)}
              >
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{check.name}</span>
                  <span className="mt-1 block text-xs font-normal text-muted-foreground">
                    {check.consensus_pages} 一致 / {check.disagreement_pages} 分歧 / {check.failed_pages} 失败 · {formatTime(check.created_at)}
                  </span>
                </span>
                <StatusBadge value={check.status} />
              </Button>
            ))}
            {!checks.isLoading && !checks.data?.length ? (
              <EmptyState title="暂无核验" description="上方创建后会在这里保留历史结果。" className="min-h-36" />
            ) : null}
          </div>

          <div className="min-w-0">
            {selectedCheck.isLoading ? (
              <div className="flex min-h-48 items-center justify-center"><Spinner /></div>
            ) : detail ? (
              <div className="flex flex-col gap-4 rounded-lg border p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="truncate font-medium">{detail.name}</h3>
                      <StatusBadge value={detail.status} />
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      {detail.page_ids.length} 页 · {detail.consensus_pages} 一致 / {detail.disagreement_pages} 分歧 / {detail.failed_pages} 失败 · 创建于 {formatTime(detail.created_at)}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    {detailActive ? (
                      <Button variant="outline" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate(detail.job_id)}>
                        {cancel.isPending ? <Spinner /> : <Ban />}
                        取消
                      </Button>
                    ) : detail.consensus_pages > 0 ? (
                      <Button size="sm" disabled={adopt.isPending} onClick={() => setAdoptAllOpen(true)}>
                        {adopt.isPending && !adopt.variables?.pageIDs?.length ? <Spinner /> : <CheckCheck />}
                        一键采纳全部一致页
                      </Button>
                    ) : null}
                  </div>
                </div>

                {detailActive ? (
                  <div className="flex flex-col gap-1">
                    <div className="flex items-center gap-3">
                      <Progress value={jobProgress} className="flex-1" />
                      <span className="text-xs tabular-nums text-muted-foreground">
                        {job.data ? `${job.data.progress_current}/${job.data.progress_total}` : "…"}
                      </span>
                    </div>
                    {job.data?.progress_message ? (
                      <div className="text-xs text-muted-foreground">{job.data.progress_message}</div>
                    ) : null}
                  </div>
                ) : null}
                <ErrorMessage message={detail.error} />
                <ErrorMessage message={selectedCheck.error?.message} />

                <div className="flex flex-col gap-1.5">
                  <div className="text-xs font-medium text-muted-foreground">参与模型</div>
                  <div className="flex flex-wrap gap-2">
                    {detail.variants.map((variant) => (
                      <div key={variant.id} className="flex items-center gap-2 rounded-md border px-2 py-1 text-xs">
                        <span className="font-medium">{variant.name}</span>
                        <span className="text-muted-foreground">
                          {variantSourceLabel(variant, profileNames, adapterNames)}
                          {variant.image_source === "enhanced" ? " · 增强图" : ""}
                        </span>
                        <StatusBadge value={variant.status} />
                      </div>
                    ))}
                  </div>
                  {detail.variants.filter((variant) => variant.error).map((variant) => (
                    <div key={variant.id} className="text-xs text-destructive">{variant.name}：{variant.error}</div>
                  ))}
                </div>

                {bulkAdoption ? (
                  <div className="flex flex-col gap-1 rounded-md border bg-muted/40 p-3 text-sm">
                    <div className="font-medium">
                      采纳结果：成功 {bulkAdoption.adopted_page_ids.length} 页
                      {bulkAdoption.skipped.length ? `，跳过 ${bulkAdoption.skipped.length} 页` : ""}
                    </div>
                    {bulkAdoption.skipped.map((skip) => (
                      <div key={skip.page_id} className="text-xs text-muted-foreground">第 {skip.page_no} 页：{skip.reason}</div>
                    ))}
                  </div>
                ) : null}

                {detail.pages?.length ? (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-20">页码</TableHead>
                        <TableHead className="w-28">状态</TableHead>
                        <TableHead>分歧摘要</TableHead>
                        <TableHead className="w-28">拍板</TableHead>
                        <TableHead className="w-24 text-right">操作</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {detail.pages.map((page) => (
                        <TableRow key={page.page_id}>
                          <TableCell className="tabular-nums">第 {page.page_no} 页</TableCell>
                          <TableCell>
                            <div className="flex flex-col items-start gap-1">
                              <CrossCheckPageStatusBadge status={page.status} />
                              {page.status === "disagreement" && page.agreement != null ? (
                                <span className="text-xs tabular-nums text-muted-foreground">一致度 {agreementLabel(page.agreement)}</span>
                              ) : null}
                            </div>
                          </TableCell>
                          <TableCell>
                            <ConflictSummary conflicts={page.conflicts} />
                            {page.error ? (
                              <div className={cn("mt-1 text-xs", page.status === "failed" ? "text-destructive" : "text-muted-foreground")}>
                                {page.error}
                              </div>
                            ) : null}
                          </TableCell>
                          <TableCell><DecisionBadge page={page} /></TableCell>
                          <TableCell className="text-right">
                            {pageAdoptable(page) ? (
                              <Button
                                size="sm"
                                variant="outline"
                                disabled={adopt.isPending}
                                onClick={() => adopt.mutate({ checkID: detail.id, pageIDs: [page.page_id] })}
                              >
                                {adopt.isPending && adopt.variables?.pageIDs?.[0] === page.page_id ? <Spinner /> : <CheckCheck />}
                                采纳
                              </Button>
                            ) : page.status === "disagreement" ? (
                              <Button size="sm" variant="secondary" asChild>
                                <Link to={`/review/${detail.document_id}/${page.page_id}`}>去审校</Link>
                              </Button>
                            ) : null}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                ) : (
                  <EmptyState title={detailActive ? "逐页结果生成中" : "暂无逐页结果"} className="min-h-32" />
                )}
              </div>
            ) : (
              <EmptyState title="选择一次核验" description="查看各模型状态、逐页比对结果并逐页拍板。" className="min-h-48" />
            )}
          </div>
        </div>
      </CardContent>

      <AlertDialog open={adoptAllOpen} onOpenChange={(open) => !adopt.isPending && setAdoptAllOpen(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>一键采纳全部一致页？</AlertDialogTitle>
            <AlertDialogDescription>
              将为所有模型输出完全一致、且尚无人工版本的页面创建定稿。分歧页与已人工编辑的页面不会受影响，会被跳过并说明原因。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={adopt.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction disabled={adopt.isPending} onClick={() => detail && adopt.mutate({ checkID: detail.id })}>
              {adopt.isPending ? <Spinner /> : <CheckCheck />}
              {adopt.isPending ? "采纳中" : "确认采纳"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

function ConflictSummary({ conflicts }: { conflicts: CrossCheckConflict[] }) {
  if (!conflicts?.length) return <span className="text-xs text-muted-foreground">--</span>;
  const visible = conflicts.slice(0, 3);
  return (
    <div className="flex flex-col gap-1">
      {visible.map((conflict, index) => (
        <div key={index} className="flex min-w-0 items-center gap-1.5 text-xs">
          <Badge variant="outline" className="shrink-0">{conflictKindLabel(conflict.kind)}</Badge>
          <span className="min-w-0 truncate" title={conflict.text}>「{conflict.text}」</span>
        </div>
      ))}
      {conflicts.length > visible.length ? (
        <span className="text-xs text-muted-foreground">还有 {conflicts.length - visible.length} 处，详见审校页。</span>
      ) : null}
    </div>
  );
}

function DecisionBadge({ page }: { page: CrossCheckPage }) {
  if (page.adopted_version_id) return <Badge variant="success">已采纳</Badge>;
  if (page.effective_kind === "manual" || page.effective_kind === "final") return <Badge variant="secondary">已人工定稿</Badge>;
  // 只有一致页(可采纳)和分歧页(可审校)有拍板动作;pending/failed/canceled 无事可做。
  if (page.status !== "consensus" && page.status !== "disagreement") {
    return <span className="text-xs text-muted-foreground">--</span>;
  }
  return <Badge variant="outline">待拍板</Badge>;
}

// 审校页「分歧」标签里的只读摘要:拍板仍走保存定稿流程,这里不做写操作。
export function CrossCheckReviewPanel({ crossCheck, page }: { crossCheck: CrossCheck; page: CrossCheckPage }) {
  const conflicts = page.conflicts ?? [];
  const visibleConflicts = conflicts.slice(0, 6);
  const hiddenConflicts = conflicts.length - visibleConflicts.length;
  return (
    <Card>
      <CardHeader className="gap-1.5 border-b p-3">
        <CardTitle className="flex flex-wrap items-center gap-2 text-sm [&_svg]:size-4">
          <GitCompareArrows />
          交叉核验
          <CrossCheckPageStatusBadge status={page.status} />
          {page.agreement != null ? (
            <span className="text-xs font-normal tabular-nums text-muted-foreground">一致度 {agreementLabel(page.agreement)}</span>
          ) : null}
        </CardTitle>
        <CardDescription className="text-xs">
          {crossCheck.name} · {formatTime(crossCheck.finished_at || crossCheck.created_at)} · 参与模型：
          {crossCheck.variants.map((variant) => variant.name).join("、") || "--"}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 p-3">
        {visibleConflicts.length ? (
          <ul className="flex flex-col gap-1.5">
            {visibleConflicts.map((conflict, index) => (
              <li key={index} className="flex flex-wrap items-center gap-1.5 text-xs">
                <Badge variant="outline" className="shrink-0">{conflictKindLabel(conflict.kind)}</Badge>
                <span className="min-w-0 break-all">「{conflict.text}」</span>
                <span className="text-muted-foreground">
                  见于 {conflict.present_in.join("、")}
                  {conflict.absent_from?.length ? `；缺于 ${conflict.absent_from.join("、")}` : ""}
                </span>
              </li>
            ))}
          </ul>
        ) : null}
        {hiddenConflicts > 0 ? (
          <div className="text-xs text-muted-foreground">还有 {hiddenConflicts} 处分歧未展开，完整清单见本页批注。</div>
        ) : null}
        {page.status === "disagreement" ? (
          <p className="text-xs text-muted-foreground">
            {page.merged_version_id
              ? "保守合并稿已生成为候选稿，可在识别/版本列表中查看；修改后保存定稿即完成拍板。"
              : "自动合并未生成合并稿，请对照各模型识别结果人工定稿。"}
          </p>
        ) : null}
        {page.status === "consensus" ? (
          <p className="text-xs text-muted-foreground">
            {page.adopted_version_id
              ? "各模型输出完全一致，该页已采纳为定稿。"
              : "各模型输出完全一致；可在文档详情页一键采纳，或直接在此确认定稿。"}
          </p>
        ) : null}
        {page.error ? (
          <p className={cn("text-xs", page.status === "failed" ? "text-destructive" : "text-muted-foreground")}>
            {page.error}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

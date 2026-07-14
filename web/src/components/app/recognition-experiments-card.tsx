import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FlaskConical, Plus, RefreshCw, Trash2, Trophy } from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage } from "@/components/app/chrome";
import { StatusBadge } from "@/components/app/status-badge";
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
import {
  createRecognitionExperiment,
  getRecognitionExperiment,
  listPromptVersions,
  listProviderAdapters,
  listRecognitionExperiments,
  listRecognizerProfiles,
  selectRecognitionExperimentWinner,
  type ExperimentVariant,
  type PageDetail,
  type RecognitionExperimentInput,
} from "@/lib/api";
import { formatTime } from "@/lib/utils";

const ACTIVE_EXPERIMENT_STATUSES = new Set(["queued", "running"]);

type VariantDraft = {
  key: string;
  name: string;
  source: string;
  promptVersionID: string;
  imageSource: "original" | "enhanced";
};

let variantSequence = 0;

function newVariant(position: number): VariantDraft {
  variantSequence += 1;
  return {
    key: `variant-${variantSequence}`,
    name: `方案 ${String.fromCharCode(65 + position)}`,
    source: "",
    promptVersionID: "active",
    imageSource: "original",
  };
}

function parsePageSpec(spec: string, pages: PageDetail[]) {
  const byNumber = new Map(pages.map((page) => [page.page_no, page.page_id]));
  const selected = new Set<number>();
  const invalid: string[] = [];

  for (const rawPart of spec.split(/[，,]/)) {
    const part = rawPart.trim();
    if (!part) continue;
    const range = part.match(/^(\d+)\s*-\s*(\d+)$/);
    if (range) {
      const start = Number(range[1]);
      const end = Number(range[2]);
      if (start > end || end - start > 10000) {
        invalid.push(part);
        continue;
      }
      for (let pageNo = start; pageNo <= end; pageNo += 1) selected.add(pageNo);
      continue;
    }
    if (/^\d+$/.test(part)) {
      selected.add(Number(part));
      continue;
    }
    invalid.push(part);
  }

  const missing = [...selected].filter((pageNo) => !byNumber.has(pageNo));
  if (invalid.length) return { pageIDs: [] as string[], error: `无法识别页码：${invalid.join("、")}` };
  if (missing.length) return { pageIDs: [] as string[], error: `文档中不存在第 ${missing.join("、")} 页` };
  return {
    pageIDs: [...selected].sort((left, right) => left - right).map((pageNo) => byNumber.get(pageNo)!),
    error: selected.size ? "" : "请至少选择一页。",
  };
}

function sourceLabel(variant: ExperimentVariant, profileNames: Map<string, string>, adapterNames: Map<string, string>) {
  const profileID = variant.recognizer_profile_id || variant.profile_id;
  if (variant.provider_adapter_id) return adapterNames.get(variant.provider_adapter_id) ?? "Provider Adapter";
  if (profileID) return profileNames.get(profileID) ?? "Recognizer Profile";
  return "未记录来源";
}

function confidenceLabel(value: number | null) {
  if (value === null || value === undefined || Number.isNaN(value)) return "--";
  const normalized = value <= 1 ? value * 100 : value;
  return `${normalized.toFixed(1)}%`;
}

function durationLabel(value: number) {
  if (!value) return "--";
  return value >= 1000 ? `${(value / 1000).toFixed(1)} 秒` : `${value} ms`;
}

export function RecognitionExperimentsCard({ documentID, pages }: { documentID: string; pages: PageDetail[] }) {
  const queryClient = useQueryClient();
  const profiles = useQuery({ queryKey: ["recognizer-profiles"], queryFn: listRecognizerProfiles });
  const adapters = useQuery({ queryKey: ["provider-adapters"], queryFn: listProviderAdapters });
  const prompts = useQuery({ queryKey: ["prompt-versions"], queryFn: listPromptVersions });
  const experiments = useQuery({
    queryKey: ["recognition-experiments", documentID],
    queryFn: () => listRecognitionExperiments(documentID),
    enabled: Boolean(documentID),
    refetchInterval: (query) => query.state.data?.some((item) => ACTIVE_EXPERIMENT_STATUSES.has(item.status)) ? 1500 : false,
  });
  const [selectedID, setSelectedID] = useState("");
  const [name, setName] = useState("");
  const [pageSpec, setPageSpec] = useState("1");
  const [variants, setVariants] = useState<VariantDraft[]>(() => [newVariant(0), newVariant(1)]);

  useEffect(() => {
    if (!experiments.data?.length) return;
    setSelectedID((current) => experiments.data.some((item) => item.id === current) ? current : experiments.data[0].id);
  }, [experiments.data]);

  const selectedExperiment = useQuery({
    queryKey: ["recognition-experiment", selectedID],
    queryFn: () => getRecognitionExperiment(selectedID),
    enabled: Boolean(selectedID),
    refetchInterval: (query) => ACTIVE_EXPERIMENT_STATUSES.has(query.state.data?.status ?? "") ? 1200 : false,
  });

  const enabledAdapters = (adapters.data ?? []).filter((adapter) => adapter.is_enabled);
  const pageSelection = useMemo(() => parsePageSpec(pageSpec, pages), [pageSpec, pages]);
  const profileNames = useMemo(() => new Map((profiles.data ?? []).map((profile) => [profile.id, profile.name])), [profiles.data]);
  const adapterNames = useMemo(() => new Map((adapters.data ?? []).map((adapter) => [adapter.id, adapter.name])), [adapters.data]);
  const validVariants = variants.length >= 2 && variants.every((variant) => variant.name.trim() && variant.source);
  const canCreate = Boolean(name.trim() && !pageSelection.error && validVariants);

  const createExperiment = useMutation({
    mutationFn: () => {
      const input: RecognitionExperimentInput = {
        name: name.trim(),
        page_ids: pageSelection.pageIDs,
        variants: variants.map((variant) => {
          const [kind, id] = variant.source.split(":", 2);
          return {
            name: variant.name.trim(),
            recognizer_profile_id: kind === "profile" ? id : undefined,
            provider_adapter_id: kind === "adapter" ? id : undefined,
            prompt_version_id: variant.promptVersionID === "active" ? undefined : variant.promptVersionID,
            image_source: variant.imageSource,
          };
        }),
      };
      return createRecognitionExperiment(documentID, input);
    },
    onSuccess: async (created) => {
      setSelectedID(created.id);
      setName("");
      await queryClient.invalidateQueries({ queryKey: ["recognition-experiments", documentID] });
      queryClient.setQueryData(["recognition-experiment", created.id], created);
      toast.success("A/B 实验已创建", { description: "结果会在后台持续更新。" });
    },
    onError: (error: Error) => toast.error("创建实验失败", { description: error.message }),
  });

  const selectWinner = useMutation({
    mutationFn: ({ experimentID, variantID }: { experimentID: string; variantID: string }) =>
      selectRecognitionExperimentWinner(experimentID, variantID),
    onSuccess: (updated) => {
      queryClient.setQueryData(["recognition-experiment", updated.id], updated);
      queryClient.invalidateQueries({ queryKey: ["recognition-experiments", documentID] });
      toast.success("Winner 已更新");
    },
    onError: (error: Error) => toast.error("选择 Winner 失败", { description: error.message }),
  });

  function updateVariant(key: string, patch: Partial<VariantDraft>) {
    setVariants((current) => current.map((variant) => variant.key === key ? { ...variant, ...patch } : variant));
  }

  const detail = selectedExperiment.data;
  const completedVariants = detail?.variants.filter((variant) => !ACTIVE_EXPERIMENT_STATUSES.has(variant.status ?? "queued")).length ?? 0;
  const progress = detail?.variants.length ? Math.round((completedVariants / detail.variants.length) * 100) : 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="inline-flex items-center gap-2">
          <FlaskConical />
          Prompt / Profile A/B 实验
        </CardTitle>
        <CardDescription>
          在同一组页面上并行比较多个 Profile 或 Provider Adapter，可分别指定 Prompt 与原图/增强图，完成后显式选择 Winner。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <FieldGroup>
          <FieldGroup className="grid gap-4 md:grid-cols-[minmax(0,1fr)_minmax(15rem,0.7fr)]">
            <Field>
              <FieldLabel htmlFor="experiment-name">实验名称</FieldLabel>
              <Input
                id="experiment-name"
                placeholder="如 Prompt v3 与 v4 对比"
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </Field>
            <Field data-invalid={Boolean(pageSelection.error)}>
              <FieldLabel htmlFor="experiment-pages">页面</FieldLabel>
              <div className="flex gap-2">
                <Input
                  id="experiment-pages"
                  aria-invalid={Boolean(pageSelection.error)}
                  placeholder="1,3-5"
                  value={pageSpec}
                  onChange={(event) => setPageSpec(event.target.value)}
                />
                <Button
                  type="button"
                  variant="outline"
                  disabled={!pages.length}
                  onClick={() => setPageSpec(pages.length ? `1-${pages[pages.length - 1].page_no}` : "")}
                >
                  全部
                </Button>
              </div>
              <FieldDescription>支持逗号和范围，例如 1,3-5；所有 Variant 使用相同页面。</FieldDescription>
              <FieldError>{pageSelection.error}</FieldError>
            </Field>
          </FieldGroup>

          <div className="flex items-center justify-between gap-3">
            <div className="text-sm font-medium">Variants（至少 2 个）</div>
            <Button variant="outline" size="sm" onClick={() => setVariants((current) => [...current, newVariant(current.length)])}>
              <Plus data-icon="inline-start" />
              添加 Variant
            </Button>
          </div>

          <div className="flex flex-col gap-3">
            {variants.map((variant, index) => (
              <FieldGroup key={variant.key} className="rounded-lg border p-4">
                <div className="flex items-center justify-between gap-3">
                  <Badge variant="secondary">Variant {index + 1}</Badge>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`删除 ${variant.name || `Variant ${index + 1}`}`}
                    disabled={variants.length <= 2}
                    onClick={() => setVariants((current) => current.filter((item) => item.key !== variant.key))}
                  >
                    <Trash2 />
                  </Button>
                </div>
                <FieldGroup className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                  <Field>
                    <FieldLabel htmlFor={`${variant.key}-name`}>名称</FieldLabel>
                    <Input
                      id={`${variant.key}-name`}
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
                          <SelectLabel>Recognizer Profiles</SelectLabel>
                          {profiles.data?.map((profile) => (
                            <SelectItem key={profile.id} value={`profile:${profile.id}`}>{profile.name}</SelectItem>
                          ))}
                        </SelectGroup>
                        <SelectGroup>
                          <SelectLabel>Provider Adapters</SelectLabel>
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

          <ErrorMessage message={createExperiment.error?.message} />
          <Button className="self-start" disabled={!canCreate || createExperiment.isPending} onClick={() => createExperiment.mutate()}>
            {createExperiment.isPending ? <Spinner data-icon="inline-start" /> : <FlaskConical data-icon="inline-start" />}
            {createExperiment.isPending ? "创建中" : "创建并运行实验"}
          </Button>
        </FieldGroup>

        <div className="grid items-start gap-4 xl:grid-cols-[minmax(16rem,0.65fr)_minmax(0,1.35fr)]">
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center justify-between gap-2">
              <div className="text-sm font-medium">历史实验</div>
              <Button variant="ghost" size="icon-sm" aria-label="刷新实验" onClick={() => void experiments.refetch()}>
                {experiments.isFetching ? <Spinner /> : <RefreshCw />}
              </Button>
            </div>
            <ErrorMessage message={experiments.error?.message} />
            {experiments.data?.map((experiment) => (
              <Button
                key={experiment.id}
                variant={selectedID === experiment.id ? "secondary" : "ghost"}
                className="h-auto justify-between gap-3 px-3 py-2 text-left"
                onClick={() => setSelectedID(experiment.id)}
              >
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{experiment.name}</span>
                  <span className="mt-1 block text-xs font-normal text-muted-foreground">
                    {experiment.page_ids.length} 页 · {experiment.variants.length} 方案 · {formatTime(experiment.created_at)}
                  </span>
                </span>
                <StatusBadge value={experiment.status} />
              </Button>
            ))}
            {!experiments.isLoading && !experiments.data?.length ? (
              <EmptyState title="暂无实验" description="上方创建后会在这里保留历史结果。" className="min-h-36" />
            ) : null}
          </div>

          <div className="min-w-0">
            {selectedExperiment.isLoading ? (
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
                      {detail.page_ids.length} 页 · 创建于 {formatTime(detail.created_at)}
                    </div>
                  </div>
                  {detail.winner_variant_id ? <Badge variant="secondary"><Trophy /> 已选 Winner</Badge> : null}
                </div>
                {ACTIVE_EXPERIMENT_STATUSES.has(detail.status) ? (
                  <div className="flex items-center gap-3">
                    <Progress value={progress} className="flex-1" />
                    <span className="text-xs tabular-nums text-muted-foreground">{completedVariants}/{detail.variants.length}</span>
                  </div>
                ) : null}
                <ErrorMessage message={detail.error} />
                <ErrorMessage message={selectedExperiment.error?.message} />
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>方案</TableHead>
                      <TableHead>来源</TableHead>
                      <TableHead className="text-right">置信度</TableHead>
                      <TableHead className="text-right">耗时</TableHead>
                      <TableHead className="text-right">人工修改量</TableHead>
                      <TableHead className="w-28 text-right">Winner</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {detail.variants.map((variant) => {
                      const winner = variant.selected_winner || detail.winner_variant_id === variant.id;
                      return (
                        <TableRow key={variant.id}>
                          <TableCell>
                            <div className="flex items-center gap-2 font-medium">
                              <span>{variant.name}</span>
                              {variant.status ? <StatusBadge value={variant.status} /> : null}
                            </div>
                            <div className="mt-1 text-xs text-muted-foreground">
                              {variant.image_source === "enhanced" ? "增强图" : "原图"} · {variant.run_ids?.length ?? 0} 次运行
                            </div>
                            {variant.error ? <div className="mt-1 max-w-xs text-xs text-destructive">{variant.error}</div> : null}
                          </TableCell>
                          <TableCell>{sourceLabel(variant, profileNames, adapterNames)}</TableCell>
                          <TableCell className="text-right tabular-nums">{confidenceLabel(variant.avg_confidence)}</TableCell>
                          <TableCell className="text-right tabular-nums">{durationLabel(variant.duration_ms)}</TableCell>
                          <TableCell className="text-right tabular-nums">
                            {variant.manual_edit_distance === null || variant.manual_edit_distance === undefined
                              ? "未统计"
                              : variant.manual_edit_distance}
                          </TableCell>
                          <TableCell className="text-right">
                            <Button
                              variant={winner ? "secondary" : "outline"}
                              size="sm"
                              disabled={ACTIVE_EXPERIMENT_STATUSES.has(detail.status) || selectWinner.isPending}
                              onClick={() => selectWinner.mutate({ experimentID: detail.id, variantID: variant.id })}
                            >
                              {selectWinner.isPending && selectWinner.variables?.variantID === variant.id
                                ? <Spinner data-icon="inline-start" />
                                : <Trophy data-icon="inline-start" />}
                              {winner ? "Winner" : "选择"}
                            </Button>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <EmptyState title="选择一个实验" description="查看各方案的置信度、耗时、人工修改量并选择 Winner。" className="min-h-48" />
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

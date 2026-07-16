import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Pencil, Plus, RefreshCw, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ErrorMessage, PageHeader } from "../components/app/chrome";
import { ProviderAdaptersCard } from "../components/app/provider-adapters-card";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "../components/ui/alert-dialog";
import { Button } from "../components/ui/button";
import { Badge } from "../components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "../components/ui/field";
import { Input } from "../components/ui/input";
import { Skeleton } from "../components/ui/skeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Spinner } from "../components/ui/spinner";
import { Switch } from "../components/ui/switch";
import { Textarea } from "../components/ui/textarea";
import {
  activatePromptVersion,
  createLLMModel,
  createLLMProvider,
  createPromptVersion,
  deleteLLMModel,
  deleteLLMProvider,
  getSettings,
  listLLMProviders,
  listPromptVersions,
  updateLLMModel,
  updateLLMProvider,
  updateSettings,
  type LLMModelInput,
  type LLMProvider,
  type LLMProviderInput,
  type PromptVersion,
  type RecognizerProfile,
  type Settings,
  type SettingsInput,
} from "../lib/api";
import { cn } from "../lib/utils";

type FormState = {
  request_timeout_seconds: number;
  pdf_render_dpi: number;
};

type FormErrors = Partial<Record<keyof FormState, string>>;

function toForm(settings: Settings): FormState {
  return {
    request_timeout_seconds: settings.request_timeout_seconds,
    pdf_render_dpi: settings.pdf_render_dpi,
  };
}

function validateForm(form: FormState): FormErrors {
  const errors: FormErrors = {};
  if (!Number.isInteger(form.request_timeout_seconds) || form.request_timeout_seconds < 10 || form.request_timeout_seconds > 3600) {
    errors.request_timeout_seconds = "请输入 10–3600 之间的整数秒数。";
  }
  if (!Number.isInteger(form.pdf_render_dpi) || form.pdf_render_dpi < 72) {
    errors.pdf_render_dpi = "请输入不小于 72 的整数 DPI。";
  }
  return errors;
}

function formsEqual(left: FormState, right: FormState) {
  return (Object.keys(left) as Array<keyof FormState>).every((key) => Object.is(left[key], right[key]));
}

export function SettingsPage() {
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ["settings"], queryFn: getSettings });
  const [form, setForm] = useState<FormState | null>(null);
  const [baseline, setBaseline] = useState<FormState | null>(null);
  const [reloadDialogOpen, setReloadDialogOpen] = useState(false);

  useEffect(() => {
    if (settings.data && form === null) {
      const next = toForm(settings.data);
      setForm(next);
      setBaseline(next);
    }
  }, [form, settings.data]);

  const errors = useMemo(() => (form ? validateForm(form) : {}), [form]);
  const isDirty = Boolean(form && baseline && !formsEqual(form, baseline));
  const isValid = Boolean(form && Object.keys(errors).length === 0);

  function replaceForm(nextSettings: Settings) {
    const next = toForm(nextSettings);
    setForm(next);
    setBaseline(next);
  }

  async function reload() {
    const result = await settings.refetch();
    if (result.isSuccess && result.data) {
      replaceForm(result.data);
      setReloadDialogOpen(false);
      toast.success("已重新加载最新设置");
      return;
    }
    toast.error("重新加载失败", { description: result.error?.message ?? "未能获取最新设置。" });
  }

  const save = useMutation({
    mutationFn: (input: SettingsInput) => updateSettings(input),
    onSuccess: (updated) => {
      queryClient.setQueryData(["settings"], updated);
      replaceForm(updated);
      toast.success("设置已保存");
    },
    onError: (error: Error) => toast.error("保存失败", { description: error.message }),
  });

  function setField<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((previous) => (previous ? { ...previous, [key]: value } : previous));
  }

  function submit() {
    if (!form || !baseline || !isDirty || !isValid) return;
    save.mutate({
      request_timeout_seconds: form.request_timeout_seconds,
      pdf_render_dpi: form.pdf_render_dpi,
    });
  }

  function requestReload() {
    if (isDirty) {
      setReloadDialogOpen(true);
      return;
    }
    void reload();
  }

  const saveDisabled = !isDirty || !isValid || save.isPending || settings.isFetching;

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="设置"
        description={isDirty ? "存在未保存的更改；检查无误后保存。" : "配置识别接口、模型、图像处理与提示词。"}
      >
        <Button variant="secondary" disabled={settings.isFetching || save.isPending} onClick={requestReload}>
          {settings.isFetching ? <Spinner /> : <RefreshCw />}
          重新加载
        </Button>
        <Button disabled={saveDisabled} onClick={submit}>
          {save.isPending ? <Spinner /> : <Save />}
          {save.isPending ? "保存中" : "保存更改"}
        </Button>
      </PageHeader>

      <ErrorMessage message={settings.error?.message} />

      {!form ? (
        <Card>
          <CardHeader>
            <Skeleton className="h-5 w-24" />
            <Skeleton className="h-4 w-64 max-w-full" />
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
            <Skeleton className="h-32 w-full" />
          </CardContent>
        </Card>
      ) : (
        <>
          <LLMProvidersCard />

          <Card>
            <CardHeader>
              <CardTitle>图像与请求</CardTitle>
              <CardDescription>控制 PDF 清晰度与单次模型请求的最长等待时间。</CardDescription>
            </CardHeader>
            <CardContent>
              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <NumberField
                  id="pdf-render-dpi"
                  label="PDF 渲染 DPI"
                  hint="分辨率越高越清晰，也会占用更多处理时间。"
                  error={errors.pdf_render_dpi}
                  min={72}
                  step={1}
                  value={form.pdf_render_dpi}
                  onChange={(value) => setField("pdf_render_dpi", value)}
                />
                <NumberField
                  id="request-timeout"
                  label="请求超时（秒）"
                  hint="允许范围为 10–3600 秒。"
                  error={errors.request_timeout_seconds}
                  min={10}
                  max={3600}
                  step={1}
                  value={form.request_timeout_seconds}
                  onChange={(value) => setField("request_timeout_seconds", value)}
                />
              </FieldGroup>
            </CardContent>
          </Card>

          <ProviderAdaptersCard />
          <PromptLibraryCard settings={settings.data} />
        </>
      )}

      <AlertDialog open={reloadDialogOpen} onOpenChange={setReloadDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的更改？</AlertDialogTitle>
            <AlertDialogDescription>重新加载会用服务端最新设置覆盖当前表单。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={settings.isFetching}>继续编辑</AlertDialogCancel>
            <AlertDialogAction
              disabled={settings.isFetching}
              onClick={(event) => {
                event.preventDefault();
                void reload();
              }}
            >
              {settings.isFetching ? <Spinner /> : <RefreshCw />}
              {settings.isFetching ? "重新加载中" : "放弃并重新加载"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

const defaultModelParams = `{"temperature":0,"max_tokens":4096,"max_image_edge":0,"retry_attempts":3,"timeout_seconds":120}`;

type ProviderForm = LLMProviderInput & { api_key: string };
type ModelForm = LLMModelInput;

function emptyProviderForm(): ProviderForm {
  return {
    name: "",
    driver: "openai-compatible",
    base_url: "https://api.openai.com/v1",
    api_key: "",
  };
}

function emptyModelForm(): ModelForm {
  return {
    name: "",
    model: "",
    params_json: defaultModelParams,
    prompt_version_id: "",
    is_default: false,
  };
}

function LLMProvidersCard() {
  const queryClient = useQueryClient();
  const providers = useQuery({
    queryKey: ["llm-providers"],
    queryFn: () => listLLMProviders(true),
  });
  const prompts = useQuery({ queryKey: ["prompt-versions"], queryFn: listPromptVersions });

  const [selectedProviderID, setSelectedProviderID] = useState("");
  const [editingProviderID, setEditingProviderID] = useState("");
  const [providerForm, setProviderForm] = useState<ProviderForm>(emptyProviderForm);
  const [editingModelID, setEditingModelID] = useState("");
  const [modelForm, setModelForm] = useState<ModelForm>(emptyModelForm);

  useEffect(() => {
    if (!providers.data?.length) return;
    setSelectedProviderID((current) => {
      if (providers.data.some((item) => item.id === current)) return current;
      return providers.data[0].id;
    });
  }, [providers.data]);

  const selectedProvider = providers.data?.find((item) => item.id === selectedProviderID) ?? null;
  const models = selectedProvider?.models ?? [];

  const saveProvider = useMutation({
    mutationFn: (input: ProviderForm) => {
      const payload: LLMProviderInput = {
        name: input.name,
        driver: input.driver,
        base_url: input.base_url,
      };
      if (input.api_key.trim()) payload.api_key = input.api_key.trim();
      return editingProviderID ? updateLLMProvider(editingProviderID, payload) : createLLMProvider({ ...payload, api_key: input.api_key.trim() || undefined });
    },
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: ["llm-providers"] });
      await queryClient.invalidateQueries({ queryKey: ["recognizer-profiles"] });
      setEditingProviderID(saved.id);
      setSelectedProviderID(saved.id);
      setProviderForm(providerToForm(saved));
      toast.success("接口已保存", { description: "API Key 已保留但不会回显。" });
    },
    onError: (error: Error) => toast.error("保存接口失败", { description: error.message }),
  });

  const removeProvider = useMutation({
    mutationFn: deleteLLMProvider,
    onSuccess: async (_, id) => {
      await queryClient.invalidateQueries({ queryKey: ["llm-providers"] });
      await queryClient.invalidateQueries({ queryKey: ["recognizer-profiles"] });
      if (selectedProviderID === id) setSelectedProviderID("");
      if (editingProviderID === id) {
        setEditingProviderID("");
        setProviderForm(emptyProviderForm());
      }
      toast.success("接口已删除");
    },
    onError: (error: Error) => toast.error("删除接口失败", { description: error.message }),
  });

  const saveModel = useMutation({
    mutationFn: (input: ModelForm) => {
      if (!selectedProviderID) throw new Error("请先选择接口");
      return editingModelID
        ? updateLLMModel(editingModelID, input)
        : createLLMModel(selectedProviderID, input);
    },
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: ["llm-providers"] });
      await queryClient.invalidateQueries({ queryKey: ["recognizer-profiles"] });
      setEditingModelID(saved.id);
      setModelForm(modelToForm(saved));
      toast.success("模型已保存");
    },
    onError: (error: Error) => toast.error("保存模型失败", { description: error.message }),
  });

  const removeModel = useMutation({
    mutationFn: deleteLLMModel,
    onSuccess: async (_, id) => {
      await queryClient.invalidateQueries({ queryKey: ["llm-providers"] });
      await queryClient.invalidateQueries({ queryKey: ["recognizer-profiles"] });
      if (editingModelID === id) {
        setEditingModelID("");
        setModelForm(emptyModelForm());
      }
      toast.success("模型已删除");
    },
    onError: (error: Error) => toast.error("删除模型失败", { description: error.message }),
  });

  function editProvider(provider: LLMProvider) {
    setEditingProviderID(provider.id);
    setSelectedProviderID(provider.id);
    setProviderForm(providerToForm(provider));
  }

  function newProvider() {
    setEditingProviderID("");
    setProviderForm(emptyProviderForm());
  }

  function editModel(model: RecognizerProfile) {
    setEditingModelID(model.id);
    setModelForm(modelToForm(model));
  }

  function newModel() {
    setEditingModelID("");
    setModelForm(emptyModelForm());
  }

  const realDriver = providerForm.driver === "openai-compatible";
  const providerValid =
    providerForm.name.trim()
    && (!realDriver || (providerForm.base_url?.trim() && (editingProviderID || providerForm.api_key.trim())));
  const modelValid =
    Boolean(selectedProviderID)
    && Boolean(modelForm.name.trim())
    && (selectedProvider?.driver !== "openai-compatible" || Boolean(modelForm.model?.trim()));

  return (
    <Card>
      <CardHeader>
        <CardTitle>识别接口与模型</CardTitle>
        <CardDescription>
          先配置接口（Base URL + API Key），再在同一接口下添加多个模型。识别时选择模型；密钥只写不回显。
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6 xl:grid-cols-2">
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium">接口</h3>
            <Button variant="secondary" size="sm" onClick={newProvider}>
              <Plus />新建接口
            </Button>
          </div>
          <ErrorMessage message={providers.error?.message} />
          <div className="flex max-h-56 flex-col gap-2 overflow-y-auto">
            {providers.data?.map((provider) => (
              <div
                key={provider.id}
                className={cn(
                  "flex items-center gap-2 rounded-lg border p-3",
                  selectedProviderID === provider.id && "border-primary bg-accent",
                )}
              >
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left"
                  onClick={() => {
                    setSelectedProviderID(provider.id);
                    editProvider(provider);
                  }}
                >
                  <span className="flex items-center gap-2 text-sm font-medium">
                    <span className="truncate">{provider.name}</span>
                    {provider.api_key_set ? <Badge variant="outline">已配置密钥</Badge> : null}
                  </span>
                  <span className="mt-1 block truncate text-xs text-muted-foreground">
                    {provider.driver} · {provider.model_count} 个模型
                    {provider.base_url ? ` · ${provider.base_url}` : ""}
                  </span>
                </button>
                <Button variant="ghost" size="icon-sm" aria-label="编辑接口" onClick={() => editProvider(provider)}>
                  <Pencil />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="删除接口"
                  disabled={removeProvider.isPending}
                  onClick={() => removeProvider.mutate(provider.id)}
                >
                  <Trash2 />
                </Button>
              </div>
            ))}
            {!providers.isLoading && !providers.data?.length ? (
              <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
                暂无接口。新建 OpenAI 兼容接口或 Mock 后，再添加模型。
              </p>
            ) : null}
          </div>

          <FieldGroup className="rounded-lg border p-4">
            <Field>
              <FieldLabel htmlFor="provider-name">接口名称</FieldLabel>
              <Input
                id="provider-name"
                value={providerForm.name}
                onChange={(event) => setProviderForm((value) => ({ ...value, name: event.target.value }))}
                placeholder="如 OpenAI / 自建网关"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="provider-driver">驱动</FieldLabel>
              <Select
                value={providerForm.driver}
                onValueChange={(driver) =>
                  setProviderForm((value) => ({ ...value, driver: driver as ProviderForm["driver"] }))
                }
              >
                <SelectTrigger id="provider-driver"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="openai-compatible">OpenAI Compatible</SelectItem>
                  <SelectItem value="mock">Mock</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field data-disabled={!realDriver}>
              <FieldLabel htmlFor="provider-base-url">Base URL</FieldLabel>
              <Input
                id="provider-base-url"
                disabled={!realDriver}
                value={providerForm.base_url}
                onChange={(event) => setProviderForm((value) => ({ ...value, base_url: event.target.value }))}
                placeholder="https://api.openai.com/v1"
              />
            </Field>
            <Field data-disabled={!realDriver}>
              <FieldLabel htmlFor="provider-api-key">API Key</FieldLabel>
              <Input
                id="provider-api-key"
                type="password"
                autoComplete="new-password"
                disabled={!realDriver}
                placeholder={editingProviderID ? "已配置时留空保持不变" : "输入 API Key"}
                value={providerForm.api_key}
                onChange={(event) => setProviderForm((value) => ({ ...value, api_key: event.target.value }))}
              />
              <FieldDescription>密钥写入本地 secrets.json，不会出现在列表或运行快照中。</FieldDescription>
            </Field>
            <Button
              className="self-start"
              disabled={!providerValid || saveProvider.isPending}
              onClick={() => saveProvider.mutate(providerForm)}
            >
              {saveProvider.isPending ? <Spinner /> : <Save />}
              {saveProvider.isPending ? "保存中" : editingProviderID ? "更新接口" : "创建接口"}
            </Button>
          </FieldGroup>
        </div>

        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium">
              模型
              {selectedProvider ? (
                <span className="ml-2 font-normal text-muted-foreground">· {selectedProvider.name}</span>
              ) : null}
            </h3>
            <Button variant="secondary" size="sm" disabled={!selectedProviderID} onClick={newModel}>
              <Plus />新建模型
            </Button>
          </div>

          <div className="flex max-h-56 flex-col gap-2 overflow-y-auto">
            {models.map((model) => (
              <div
                key={model.id}
                className={cn(
                  "flex items-center gap-2 rounded-lg border p-3",
                  editingModelID === model.id && "border-primary bg-accent",
                )}
              >
                <button type="button" className="min-w-0 flex-1 text-left" onClick={() => editModel(model)}>
                  <span className="flex items-center gap-2 text-sm font-medium">
                    <span className="truncate">{model.name}</span>
                    {model.is_default ? <Badge variant="secondary">默认</Badge> : null}
                  </span>
                  <span className="mt-1 block truncate text-xs text-muted-foreground">
                    {model.model || "mock"}
                  </span>
                </button>
                <Button variant="ghost" size="icon-sm" aria-label="编辑模型" onClick={() => editModel(model)}>
                  <Pencil />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="删除模型"
                  disabled={removeModel.isPending}
                  onClick={() => removeModel.mutate(model.id)}
                >
                  <Trash2 />
                </Button>
              </div>
            ))}
            {selectedProviderID && !models.length ? (
              <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
                该接口下暂无模型。添加模型后即可用于识别与 A/B 实验。
              </p>
            ) : null}
            {!selectedProviderID ? (
              <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">请先选择或创建接口。</p>
            ) : null}
          </div>

          <FieldGroup className="rounded-lg border p-4">
            <Field>
              <FieldLabel htmlFor="model-name">显示名称</FieldLabel>
              <Input
                id="model-name"
                disabled={!selectedProviderID}
                value={modelForm.name}
                onChange={(event) => setModelForm((value) => ({ ...value, name: event.target.value }))}
                placeholder="如 gpt-4o 高精度"
              />
            </Field>
            <Field data-disabled={selectedProvider?.driver !== "openai-compatible"}>
              <FieldLabel htmlFor="model-id">模型 ID</FieldLabel>
              <Input
                id="model-id"
                disabled={!selectedProviderID || selectedProvider?.driver !== "openai-compatible"}
                value={modelForm.model}
                onChange={(event) => setModelForm((value) => ({ ...value, model: event.target.value }))}
                placeholder="如 gpt-4o-mini"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="model-prompt">默认 Prompt</FieldLabel>
              <Select
                value={modelForm.prompt_version_id || "active"}
                onValueChange={(id) =>
                  setModelForm((value) => ({ ...value, prompt_version_id: id === "active" ? "" : id }))
                }
                disabled={!selectedProviderID}
              >
                <SelectTrigger id="model-prompt"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="active">使用激活版本</SelectItem>
                  {prompts.data?.map((prompt) => (
                    <SelectItem key={prompt.id} value={prompt.id}>{prompt.version}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="model-params">参数 JSON</FieldLabel>
              <Textarea
                id="model-params"
                className="min-h-28 font-mono text-xs"
                spellCheck={false}
                disabled={!selectedProviderID}
                value={modelForm.params_json}
                onChange={(event) => setModelForm((value) => ({ ...value, params_json: event.target.value }))}
              />
            </Field>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>设为默认模型</FieldTitle>
                <FieldDescription>未指定模型时使用此默认；仅全局一个默认。</FieldDescription>
              </FieldContent>
              <Switch
                disabled={!selectedProviderID}
                checked={Boolean(modelForm.is_default)}
                onCheckedChange={(checked) => setModelForm((value) => ({ ...value, is_default: checked }))}
              />
            </Field>
            <Button
              className="self-start"
              disabled={!modelValid || saveModel.isPending}
              onClick={() => saveModel.mutate(modelForm)}
            >
              {saveModel.isPending ? <Spinner /> : <Save />}
              {saveModel.isPending ? "保存中" : editingModelID ? "更新模型" : "创建模型"}
            </Button>
          </FieldGroup>
        </div>
      </CardContent>
    </Card>
  );
}

function providerToForm(provider: LLMProvider): ProviderForm {
  return {
    name: provider.name,
    driver: provider.driver,
    base_url: provider.base_url || "https://api.openai.com/v1",
    api_key: "",
  };
}

function modelToForm(model: RecognizerProfile): ModelForm {
  return {
    name: model.name,
    model: model.model,
    params_json: model.params_json || defaultModelParams,
    prompt_version_id: model.prompt_version_id || "",
    is_default: model.is_default,
  };
}

function PromptLibraryCard({ settings }: { settings?: Settings }) {
  const queryClient = useQueryClient();
  const versions = useQuery({ queryKey: ["prompt-versions"], queryFn: listPromptVersions });
  const [selectedID, setSelectedID] = useState("");
  const [draftVersion, setDraftVersion] = useState("");
  const [draftContent, setDraftContent] = useState("");
  const draftInitialized = useRef(false);

  useEffect(() => {
    if (!versions.data?.length) return;
    setSelectedID((current) => {
      if (versions.data.some((item) => item.id === current)) return current;
      return versions.data.find((item) => item.is_active)?.id ?? versions.data[0].id;
    });
  }, [versions.data]);

  useEffect(() => {
    if (draftInitialized.current || !settings?.prompt) return;
    draftInitialized.current = true;
    setDraftContent(settings.prompt);
  }, [settings?.prompt]);

  const selected = useMemo(
    () => versions.data?.find((item) => item.id === selectedID) ?? null,
    [selectedID, versions.data],
  );

  const createVersion = useMutation({
    mutationFn: createPromptVersion,
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["prompt-versions"] });
      setSelectedID(created.id);
      setDraftVersion("");
      toast.success("Prompt 版本已保存", { description: "保存为历史版本，不会自动切换当前识别配置。" });
    },
    onError: (error: Error) => toast.error("保存 Prompt 版本失败", { description: error.message }),
  });

  const activateVersion = useMutation({
    mutationFn: activatePromptVersion,
    onSuccess: async (activated) => {
      setSelectedID(activated.id);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["prompt-versions"] }),
        queryClient.invalidateQueries({ queryKey: ["settings"] }),
      ]);
      toast.success(`已激活 ${activated.version}`, {
        description: "提示词文件与 config.json 已同步，后续识别运行将记录此版本和内容哈希。",
      });
    },
    onError: (error: Error) => toast.error("激活 Prompt 版本失败", { description: error.message }),
  });

  function saveDraft() {
    const version = draftVersion.trim();
    const content = draftContent.trim();
    if (!version || !content || createVersion.isPending) return;
    createVersion.mutate({ version, content });
  }

  function copySelectedToDraft(item: PromptVersion) {
    draftInitialized.current = true;
    setDraftContent(item.content);
    setDraftVersion("");
    toast.info("已载入草稿", { description: "请填写新版本名称后保存。" });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Prompt 版本库</CardTitle>
        <CardDescription>
          每个版本保存完整内容、SHA-256 与创建时间；激活版本会写入 {settings?.prompt_path || "prompt_path"}。
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <ErrorMessage message={versions.error?.message} />
        <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(22rem,0.9fr)]">
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="new-prompt-version">新版本名称</FieldLabel>
              <Input
                id="new-prompt-version"
                maxLength={128}
                placeholder="如 vlm_transcribe_page_v2"
                value={draftVersion}
                onChange={(event) => setDraftVersion(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="new-prompt-content">提示词内容</FieldLabel>
              <Textarea
                id="new-prompt-content"
                className="min-h-72 font-mono text-sm"
                spellCheck={false}
                value={draftContent}
                onChange={(event) => {
                  draftInitialized.current = true;
                  setDraftContent(event.target.value);
                }}
              />
              <FieldDescription>保存只创建不可变历史快照；确认后再从右侧激活。</FieldDescription>
            </Field>
            <Button
              className="self-start"
              disabled={!draftVersion.trim() || !draftContent.trim() || createVersion.isPending}
              onClick={saveDraft}
            >
              {createVersion.isPending ? <Spinner /> : <Save />}
              {createVersion.isPending ? "保存中" : "保存为新版本"}
            </Button>
          </FieldGroup>

          <div className="flex min-w-0 flex-col gap-3">
            <div className="flex items-center justify-between gap-3">
              <h3 className="text-sm font-medium">历史版本</h3>
              {versions.isFetching ? <Spinner /> : <span className="text-xs text-muted-foreground">{versions.data?.length ?? 0} 个</span>}
            </div>
            <div className="flex max-h-64 flex-col gap-2 overflow-y-auto pr-1">
              {versions.data?.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  aria-pressed={selectedID === item.id}
                  className={cn(
                    "rounded-lg border p-3 text-left transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    selectedID === item.id ? "border-primary bg-accent" : "border-border",
                  )}
                  onClick={() => setSelectedID(item.id)}
                >
                  <span className="flex items-center justify-between gap-3">
                    <span className="truncate text-sm font-medium">{item.version}</span>
                    {item.is_active ? <Badge variant="success">当前激活</Badge> : null}
                  </span>
                  <span className="mt-1 block truncate font-mono text-xs text-muted-foreground">{item.sha256}</span>
                  <span className="mt-1 block text-xs text-muted-foreground">{formatPromptTime(item.created_at)}</span>
                </button>
              ))}
              {!versions.isLoading && versions.data?.length === 0 ? (
                <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">尚无 Prompt 版本。</p>
              ) : null}
            </div>

            {selected ? (
              <div className="flex flex-col gap-3 rounded-lg border bg-muted/25 p-3">
                <div className="grid gap-1 text-xs text-muted-foreground">
                  <span>版本：{selected.version}</span>
                  <span className="break-all font-mono">SHA-256：{selected.sha256}</span>
                  <span>创建：{formatPromptTime(selected.created_at)}</span>
                  {selected.activated_at ? <span>最近激活：{formatPromptTime(selected.activated_at)}</span> : null}
                </div>
                <Textarea className="min-h-40 font-mono text-xs" readOnly spellCheck={false} value={selected.content} />
                <div className="flex flex-wrap gap-2">
                  <Button variant="secondary" size="sm" onClick={() => copySelectedToDraft(selected)}>
                    <Copy />
                    基于此版本创建
                  </Button>
                  <Button
                    size="sm"
                    disabled={selected.is_active || activateVersion.isPending}
                    onClick={() => activateVersion.mutate(selected.id)}
                  >
                    {activateVersion.isPending ? <Spinner /> : <Check />}
                    {selected.is_active ? "当前已激活" : activateVersion.isPending ? "激活中" : "激活此版本"}
                  </Button>
                </div>
              </div>
            ) : null}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function formatPromptTime(value: string) {
  if (!value) return "-";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString("zh-CN");
}

function NumberField({
  id,
  label,
  hint,
  error,
  value,
  onChange,
  disabled,
  min,
  max,
  step,
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  value: number;
  onChange: (value: number) => void;
  disabled?: boolean;
  min?: number;
  max?: number;
  step?: number;
}) {
  return (
    <Field data-disabled={disabled} data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        inputMode="decimal"
        disabled={disabled}
        aria-invalid={Boolean(error)}
        min={min}
        max={max}
        step={step}
        value={Number.isFinite(value) ? value : ""}
        onChange={(event) => onChange(event.target.valueAsNumber)}
      />
      {hint ? <FieldDescription>{hint}</FieldDescription> : null}
      <FieldError>{error}</FieldError>
    </Field>
  );
}

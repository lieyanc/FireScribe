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
  createRecognizerProfile,
  createPromptVersion,
  deleteRecognizerProfile,
  getSettings,
  listPromptVersions,
  listRecognizerProfiles,
  updateRecognizerProfile,
  updateSettings,
  type PromptVersion,
  type RecognizerProfile,
  type RecognizerProfileInput,
  type Settings,
  type SettingsInput,
} from "../lib/api";

type FormState = {
  use_mock_ocr: boolean;
  request_timeout_seconds: number;
  pdf_render_dpi: number;
  base_url: string;
  model: string;
  temperature: number;
  max_tokens: number;
  max_image_edge: number;
  retry_attempts: number;
};

type FormErrors = Partial<Record<keyof FormState | "api_key", string>>;

function toForm(settings: Settings): FormState {
  return {
    use_mock_ocr: settings.use_mock_ocr,
    request_timeout_seconds: settings.request_timeout_seconds,
    pdf_render_dpi: settings.pdf_render_dpi,
    base_url: settings.openai.base_url,
    model: settings.openai.model,
    temperature: settings.openai.temperature,
    max_tokens: settings.openai.max_tokens,
    max_image_edge: settings.openai.max_image_edge,
    retry_attempts: settings.openai.retry_attempts,
  };
}

function validateForm(form: FormState, apiKey: string, apiKeySet: boolean): FormErrors {
  const errors: FormErrors = {};
  const baseURL = form.base_url.trim();

  if (!Number.isInteger(form.request_timeout_seconds) || form.request_timeout_seconds < 10 || form.request_timeout_seconds > 3600) {
    errors.request_timeout_seconds = "请输入 10–3600 之间的整数秒数。";
  }
  if (!Number.isInteger(form.pdf_render_dpi) || form.pdf_render_dpi < 72) {
    errors.pdf_render_dpi = "请输入不小于 72 的整数 DPI。";
  }
  if (baseURL && !/^https?:\/\//i.test(baseURL)) {
    errors.base_url = "Base URL 必须以 http:// 或 https:// 开头。";
  }
  if (!form.use_mock_ocr && !form.model.trim()) {
    errors.model = "使用真实识别时必须填写模型名称。";
  }
  if (!form.use_mock_ocr && !apiKeySet && !apiKey.trim()) {
    errors.api_key = "使用真实识别时必须配置 API Key。";
  }
  if (!Number.isFinite(form.temperature) || form.temperature < 0 || form.temperature > 2) {
    errors.temperature = "请输入 0–2 之间的数值。";
  }
  if (!Number.isInteger(form.max_tokens) || form.max_tokens < 1) {
    errors.max_tokens = "请输入不小于 1 的整数。";
  }
  if (!Number.isInteger(form.max_image_edge) || form.max_image_edge < 0) {
    errors.max_image_edge = "请输入不小于 0 的整数。";
  }
  if (!Number.isInteger(form.retry_attempts) || form.retry_attempts < 0) {
    errors.retry_attempts = "请输入不小于 0 的整数。";
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
  const [apiKey, setApiKey] = useState("");
  const [reloadDialogOpen, setReloadDialogOpen] = useState(false);

  // 后台 refetch 只更新查询缓存；首次载入、显式重载和保存成功才会替换编辑中的表单。
  useEffect(() => {
    if (settings.data && form === null) {
      const next = toForm(settings.data);
      setForm(next);
      setBaseline(next);
      setApiKey("");
    }
  }, [form, settings.data]);

  const apiKeySet = settings.data?.openai.api_key_set ?? false;
  const errors = useMemo(() => (form ? validateForm(form, apiKey, apiKeySet) : {}), [apiKey, apiKeySet, form]);
  const isDirty = Boolean(form && baseline && (!formsEqual(form, baseline) || apiKey.trim()));
  const isValid = Boolean(form && Object.keys(errors).length === 0);

  function replaceForm(nextSettings: Settings) {
    const next = toForm(nextSettings);
    setForm(next);
    setBaseline(next);
    setApiKey("");
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
    const payload: SettingsInput = {
      use_mock_ocr: form.use_mock_ocr,
      request_timeout_seconds: form.request_timeout_seconds,
      pdf_render_dpi: form.pdf_render_dpi,
      openai: {
        base_url: form.base_url,
        model: form.model,
        temperature: form.temperature,
        max_tokens: form.max_tokens,
        max_image_edge: form.max_image_edge,
        retry_attempts: form.retry_attempts,
      },
    };
    if (apiKey.trim()) {
      payload.openai = { ...payload.openai, api_key: apiKey.trim() };
    }
    save.mutate(payload);
  }

  function requestReload() {
    if (isDirty) {
      setReloadDialogOpen(true);
      return;
    }
    void reload();
  }

  const openaiDisabled = form?.use_mock_ocr ?? false;
  const saveDisabled = !isDirty || !isValid || save.isPending || settings.isFetching;

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="设置"
        description={isDirty ? "存在未保存的更改；检查无误后保存。" : "配置识别引擎、图像处理与提示词。"}
      >
        <Button variant="secondary" disabled={settings.isFetching || save.isPending} onClick={requestReload}>
          {settings.isFetching ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}
          重新加载
        </Button>
        <Button disabled={saveDisabled} onClick={submit}>
          {save.isPending ? <Spinner data-icon="inline-start" /> : <Save data-icon="inline-start" />}
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
          <Card>
            <CardHeader>
              <CardTitle>识别引擎</CardTitle>
              <CardDescription>选择模拟模式，或配置 OpenAI 兼容的视觉模型。</CardDescription>
            </CardHeader>
            <CardContent>
              <FieldGroup>
                <Field orientation="horizontal">
                  <FieldContent>
                    <FieldTitle>使用模拟识别</FieldTitle>
                    <FieldDescription>开启后不调用真实模型，仅返回占位文本，适合本地联调。</FieldDescription>
                  </FieldContent>
                  <Switch
                    id="use-mock-ocr"
                    aria-label="使用模拟识别"
                    checked={form.use_mock_ocr}
                    onCheckedChange={(checked) => setField("use_mock_ocr", checked)}
                  />
                </Field>

                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <Field data-disabled={openaiDisabled} data-invalid={Boolean(errors.base_url)}>
                    <FieldLabel htmlFor="base-url">Base URL</FieldLabel>
                    <Input
                      id="base-url"
                      disabled={openaiDisabled}
                      aria-invalid={Boolean(errors.base_url)}
                      placeholder="https://api.openai.com/v1"
                      value={form.base_url}
                      onChange={(event) => setField("base_url", event.target.value)}
                    />
                    <FieldError>{errors.base_url}</FieldError>
                  </Field>
                  <Field data-disabled={openaiDisabled} data-invalid={Boolean(errors.model)}>
                    <FieldLabel htmlFor="model">模型</FieldLabel>
                    <Input
                      id="model"
                      disabled={openaiDisabled}
                      aria-invalid={Boolean(errors.model)}
                      placeholder="如 gpt-4o-mini"
                      value={form.model}
                      onChange={(event) => setField("model", event.target.value)}
                    />
                    <FieldError>{errors.model}</FieldError>
                  </Field>
                </FieldGroup>

                <Field data-disabled={openaiDisabled} data-invalid={Boolean(errors.api_key)}>
                  <FieldLabel htmlFor="api-key">API Key</FieldLabel>
                  <Input
                    id="api-key"
                    type="password"
                    autoComplete="new-password"
                    disabled={openaiDisabled}
                    aria-invalid={Boolean(errors.api_key)}
                    placeholder={apiKeySet ? "已配置（留空则保持不变）" : "输入新的 API Key"}
                    value={apiKey}
                    onChange={(event) => setApiKey(event.target.value)}
                  />
                  <FieldDescription>密钥不会被回显；只有输入新密钥时才会更新。</FieldDescription>
                  <FieldError>{errors.api_key}</FieldError>
                </Field>

                <FieldGroup className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                  <NumberField
                    id="temperature"
                    label="Temperature"
                    disabled={openaiDisabled}
                    error={errors.temperature}
                    min={0}
                    max={2}
                    step={0.1}
                    value={form.temperature}
                    onChange={(value) => setField("temperature", value)}
                  />
                  <NumberField
                    id="max-tokens"
                    label="Max Tokens"
                    disabled={openaiDisabled}
                    error={errors.max_tokens}
                    min={1}
                    step={1}
                    value={form.max_tokens}
                    onChange={(value) => setField("max_tokens", value)}
                  />
                  <NumberField
                    id="max-image-edge"
                    label="最大图像边长"
                    hint="0 表示不缩放。"
                    disabled={openaiDisabled}
                    error={errors.max_image_edge}
                    min={0}
                    step={1}
                    value={form.max_image_edge}
                    onChange={(value) => setField("max_image_edge", value)}
                  />
                  <NumberField
                    id="retry-attempts"
                    label="重试次数"
                    disabled={openaiDisabled}
                    error={errors.retry_attempts}
                    min={0}
                    step={1}
                    value={form.retry_attempts}
                    onChange={(value) => setField("retry_attempts", value)}
                  />
                </FieldGroup>
              </FieldGroup>
            </CardContent>
          </Card>

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

          <RecognizerProfilesCard />
          <ProviderAdaptersCard />
          <PromptLibraryCard settings={settings.data} />
        </>
      )}

      <AlertDialog open={reloadDialogOpen} onOpenChange={setReloadDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的更改？</AlertDialogTitle>
            <AlertDialogDescription>重新加载会用服务端最新设置覆盖当前表单，包括尚未保存的 API Key。</AlertDialogDescription>
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
              {settings.isFetching ? <Spinner data-icon="inline-start" /> : <RefreshCw data-icon="inline-start" />}
              {settings.isFetching ? "重新加载中" : "放弃并重新加载"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

const defaultProfileParams = `{"temperature":0,"max_tokens":4096,"max_image_edge":0,"retry_attempts":3,"timeout_seconds":120}`;

type ProfileForm = RecognizerProfileInput & { api_key: string };

function emptyProfileForm(): ProfileForm {
  return {
    name: "",
    driver: "openai-compatible",
    base_url: "https://api.openai.com/v1",
    api_key: "",
    model: "",
    params_json: defaultProfileParams,
    prompt_version_id: "",
    is_default: false,
  };
}

function RecognizerProfilesCard() {
  const queryClient = useQueryClient();
  const profiles = useQuery({ queryKey: ["recognizer-profiles"], queryFn: listRecognizerProfiles });
  const prompts = useQuery({ queryKey: ["prompt-versions"], queryFn: listPromptVersions });
  const [editingID, setEditingID] = useState("");
  const [form, setForm] = useState<ProfileForm>(emptyProfileForm);

  const saveProfile = useMutation({
    mutationFn: (input: ProfileForm) => {
      const payload: RecognizerProfileInput = { ...input };
      if (!input.api_key.trim()) delete payload.api_key;
      return editingID ? updateRecognizerProfile(editingID, payload) : createRecognizerProfile(payload);
    },
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: ["recognizer-profiles"] });
      setEditingID(saved.id);
      setForm(profileToForm(saved));
      toast.success("识别器配置已保存", { description: "API Key 已保留但不会回显。" });
    },
    onError: (error: Error) => toast.error("保存识别器失败", { description: error.message }),
  });
  const removeProfile = useMutation({
    mutationFn: deleteRecognizerProfile,
    onSuccess: async (_, id) => {
      await queryClient.invalidateQueries({ queryKey: ["recognizer-profiles"] });
      if (editingID === id) {
        setEditingID("");
        setForm(emptyProfileForm());
      }
      toast.success("识别器配置已删除");
    },
    onError: (error: Error) => toast.error("删除识别器失败", { description: error.message }),
  });

  function edit(profile: RecognizerProfile) {
    setEditingID(profile.id);
    setForm(profileToForm(profile));
  }

  const realDriver = form.driver === "openai-compatible";
  const valid = form.name.trim() && (!realDriver || (form.base_url?.trim() && form.model?.trim() && (editingID || form.api_key.trim())));

  return (
    <Card>
      <CardHeader>
        <CardTitle>识别器 Profiles</CardTitle>
        <CardDescription>注册多个受控驱动配置，用于按运行选择模型并开展 Prompt A/B；不会加载或执行本地插件代码。</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-5 xl:grid-cols-[minmax(0,0.8fr)_minmax(22rem,1.2fr)]">
        <div className="flex flex-col gap-2">
          <Button variant="secondary" className="justify-start" onClick={() => { setEditingID(""); setForm(emptyProfileForm()); }}>
            <Plus data-icon="inline-start" />新建配置
          </Button>
          <ErrorMessage message={profiles.error?.message} />
          {profiles.data?.map((profile) => (
            <div key={profile.id} className={`flex items-center gap-2 rounded-lg border p-3 ${editingID === profile.id ? "border-primary bg-accent" : ""}`}>
              <button type="button" className="min-w-0 flex-1 text-left" onClick={() => edit(profile)}>
                <span className="flex items-center gap-2 text-sm font-medium">
                  <span className="truncate">{profile.name}</span>
                  {profile.is_default ? <Badge variant="secondary">默认</Badge> : null}
                </span>
                <span className="mt-1 block truncate text-xs text-muted-foreground">{profile.driver} · {profile.model || "mock"}</span>
              </button>
              <Button variant="ghost" size="icon-sm" aria-label="编辑" onClick={() => edit(profile)}><Pencil /></Button>
              <Button variant="ghost" size="icon-sm" aria-label="删除" disabled={removeProfile.isPending} onClick={() => removeProfile.mutate(profile.id)}><Trash2 /></Button>
            </div>
          ))}
          {!profiles.isLoading && !profiles.data?.length ? <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">暂无 Profile，识别仍使用旧版全局设置。</p> : null}
        </div>

        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="profile-name">名称</FieldLabel>
            <Input id="profile-name" value={form.name} onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="profile-driver">驱动</FieldLabel>
              <Select value={form.driver} onValueChange={(driver) => setForm((value) => ({ ...value, driver: driver as ProfileForm["driver"] }))}>
                <SelectTrigger id="profile-driver"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="openai-compatible">OpenAI Compatible</SelectItem><SelectItem value="mock">Mock</SelectItem></SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="profile-prompt">默认 Prompt</FieldLabel>
              <Select value={form.prompt_version_id || "active"} onValueChange={(id) => setForm((value) => ({ ...value, prompt_version_id: id === "active" ? "" : id }))}>
                <SelectTrigger id="profile-prompt"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="active">使用激活版本</SelectItem>{prompts.data?.map((prompt) => <SelectItem key={prompt.id} value={prompt.id}>{prompt.version}</SelectItem>)}</SelectContent>
              </Select>
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field data-disabled={!realDriver}>
              <FieldLabel htmlFor="profile-base-url">Base URL</FieldLabel>
              <Input id="profile-base-url" disabled={!realDriver} value={form.base_url} onChange={(event) => setForm((value) => ({ ...value, base_url: event.target.value }))} />
            </Field>
            <Field data-disabled={!realDriver}>
              <FieldLabel htmlFor="profile-model">模型</FieldLabel>
              <Input id="profile-model" disabled={!realDriver} value={form.model} onChange={(event) => setForm((value) => ({ ...value, model: event.target.value }))} />
            </Field>
          </div>
          <Field data-disabled={!realDriver}>
            <FieldLabel htmlFor="profile-api-key">API Key</FieldLabel>
            <Input id="profile-api-key" type="password" disabled={!realDriver} autoComplete="new-password" placeholder={editingID ? "已配置时留空保持不变" : "输入 API Key"} value={form.api_key} onChange={(event) => setForm((value) => ({ ...value, api_key: event.target.value }))} />
            <FieldDescription>密钥仅写入本地配置库，不会出现在列表、运行快照或 API 响应中。</FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="profile-params">参数 JSON</FieldLabel>
            <Textarea id="profile-params" className="min-h-28 font-mono text-xs" spellCheck={false} value={form.params_json} onChange={(event) => setForm((value) => ({ ...value, params_json: event.target.value }))} />
          </Field>
          <Field orientation="horizontal">
            <FieldContent><FieldTitle>设为默认 Profile</FieldTitle><FieldDescription>旧版空请求也会选择这个 Profile；未设置时继续使用全局 Settings。</FieldDescription></FieldContent>
            <Switch checked={Boolean(form.is_default)} onCheckedChange={(checked) => setForm((value) => ({ ...value, is_default: checked }))} />
          </Field>
          <Button className="self-start" disabled={!valid || saveProfile.isPending} onClick={() => saveProfile.mutate(form)}>
            {saveProfile.isPending ? <Spinner data-icon="inline-start" /> : <Save data-icon="inline-start" />}{saveProfile.isPending ? "保存中" : editingID ? "更新 Profile" : "创建 Profile"}
          </Button>
        </FieldGroup>
      </CardContent>
    </Card>
  );
}

function profileToForm(profile: RecognizerProfile): ProfileForm {
  return {
    name: profile.name,
    driver: profile.driver,
    base_url: profile.base_url,
    api_key: "",
    model: profile.model,
    params_json: profile.params_json || defaultProfileParams,
    prompt_version_id: profile.prompt_version_id || "",
    is_default: profile.is_default,
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
              {createVersion.isPending ? <Spinner data-icon="inline-start" /> : <Save data-icon="inline-start" />}
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
                  className={`rounded-lg border p-3 text-left transition-colors hover:bg-accent ${
                    selectedID === item.id ? "border-primary bg-accent" : "border-border"
                  }`}
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
                    <Copy data-icon="inline-start" />
                    基于此版本创建
                  </Button>
                  <Button
                    size="sm"
                    disabled={selected.is_active || activateVersion.isPending}
                    onClick={() => activateVersion.mutate(selected.id)}
                  >
                    {activateVersion.isPending ? <Spinner data-icon="inline-start" /> : <Check data-icon="inline-start" />}
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

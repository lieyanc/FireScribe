import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, RefreshCw, Save, Settings2 } from "lucide-react";
import { ErrorMessage, PageHeader } from "../components/app/chrome";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Skeleton } from "../components/ui/skeleton";
import { Switch } from "../components/ui/switch";
import { Textarea } from "../components/ui/textarea";
import { toast } from "../components/ui/toaster";
import { getSettings, updateSettings, type Settings, type SettingsInput } from "../lib/api";

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
  prompt: string;
};

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
    prompt: settings.prompt,
  };
}

export function SettingsPage() {
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ["settings"], queryFn: getSettings });
  const [form, setForm] = useState<FormState | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [originalPrompt, setOriginalPrompt] = useState("");

  // 仅在首次加载时填充表单:后台 refetch(如窗口聚焦)不得覆盖未保存的
  // 编辑和已输入的 API key;显式“重新加载”按钮会主动重置。
  useEffect(() => {
    if (settings.data && form === null) {
      setForm(toForm(settings.data));
      setOriginalPrompt(settings.data.prompt);
      setApiKey("");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings.data]);

  async function reload() {
    const result = await settings.refetch();
    if (result.data) {
      setForm(toForm(result.data));
      setOriginalPrompt(result.data.prompt);
      setApiKey("");
    }
  }

  const save = useMutation({
    mutationFn: (input: SettingsInput) => updateSettings(input),
    onSuccess: (updated) => {
      toast({ title: "设置已保存", variant: "success" });
      queryClient.setQueryData(["settings"], updated);
      setForm(toForm(updated));
      setOriginalPrompt(updated.prompt);
      setApiKey("");
    },
    onError: (error: Error) => toast({ title: "保存失败", description: error.message, variant: "error" }),
  });

  function setField<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev));
  }

  function submit() {
    if (!form) return;
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
    if (form.prompt !== originalPrompt) {
      payload.prompt = form.prompt;
    }
    if (apiKey.trim()) {
      payload.openai = { ...payload.openai, api_key: apiKey.trim() };
    }
    save.mutate(payload);
  }

  const apiKeySet = settings.data?.openai.api_key_set ?? false;
  const openaiDisabled = form?.use_mock_ocr ?? false;

  return (
    <div className="space-y-5">
      <PageHeader title="设置" description="识别引擎、图像处理与提示词配置">
        <Button variant="secondary" disabled={settings.isFetching} onClick={() => void reload()}>
          <RefreshCw className={settings.isFetching ? "size-4 animate-spin" : "size-4"} />
          重新加载
        </Button>
        <Button disabled={!form || save.isPending} onClick={submit}>
          <Save className="size-4" />
          {save.isPending ? "保存中" : "保存"}
        </Button>
      </PageHeader>

      <ErrorMessage message={settings.error?.message} />

      {!form ? (
        <Card>
          <CardContent className="space-y-3 p-6">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-2/3" />
            <Skeleton className="h-24 w-full" />
          </CardContent>
        </Card>
      ) : (
        <>
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="inline-flex items-center gap-2 text-base">
                <Settings2 className="size-4 text-muted-foreground" />
                识别引擎
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-5">
              <div className="flex items-start justify-between gap-4">
                <div className="space-y-0.5">
                  <Label htmlFor="use-mock-ocr">使用模拟识别</Label>
                  <p className="text-xs text-muted-foreground">开启后不调用真实模型,仅返回占位文本,便于本地联调。</p>
                </div>
                <Switch
                  id="use-mock-ocr"
                  aria-label="使用模拟识别"
                  checked={form.use_mock_ocr}
                  onCheckedChange={(checked) => setField("use_mock_ocr", checked)}
                />
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="base-url">Base URL</Label>
                  <Input
                    id="base-url"
                    disabled={openaiDisabled}
                    value={form.base_url}
                    onChange={(event) => setField("base_url", event.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="model">模型</Label>
                  <Input
                    id="model"
                    disabled={openaiDisabled}
                    placeholder="如 gpt-4o-mini"
                    value={form.model}
                    onChange={(event) => setField("model", event.target.value)}
                  />
                </div>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="api-key" className="inline-flex items-center gap-1.5">
                  <KeyRound className="size-3.5 text-muted-foreground" />
                  API Key
                </Label>
                <Input
                  id="api-key"
                  type="password"
                  autoComplete="off"
                  disabled={openaiDisabled}
                  placeholder={apiKeySet ? "已配置(留空则保持不变)" : "未配置"}
                  value={apiKey}
                  onChange={(event) => setApiKey(event.target.value)}
                />
                <p className="text-xs text-muted-foreground">仅在输入新密钥时更新;密钥不会被回显。</p>
              </div>

              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <NumberField
                  id="temperature"
                  label="Temperature"
                  disabled={openaiDisabled}
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
                  min={1}
                  step={1}
                  value={form.max_tokens}
                  onChange={(value) => setField("max_tokens", value)}
                />
                <NumberField
                  id="max-image-edge"
                  label="最大图像边长"
                  hint="0 = 不缩放"
                  disabled={openaiDisabled}
                  min={0}
                  step={1}
                  value={form.max_image_edge}
                  onChange={(value) => setField("max_image_edge", value)}
                />
                <NumberField
                  id="retry-attempts"
                  label="重试次数"
                  disabled={openaiDisabled}
                  min={0}
                  step={1}
                  value={form.retry_attempts}
                  onChange={(value) => setField("retry_attempts", value)}
                />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">图像与请求</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              <NumberField
                id="pdf-render-dpi"
                label="PDF 渲染 DPI"
                hint="PDF 光栅化分辨率,越高越清晰但越慢"
                min={72}
                step={1}
                value={form.pdf_render_dpi}
                onChange={(value) => setField("pdf_render_dpi", value)}
              />
              <NumberField
                id="request-timeout"
                label="请求超时(秒)"
                min={1}
                step={1}
                value={form.request_timeout_seconds}
                onChange={(value) => setField("request_timeout_seconds", value)}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">识别提示词</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <Textarea
                className="min-h-64 font-mono text-xs leading-relaxed"
                spellCheck={false}
                value={form.prompt}
                onChange={(event) => setField("prompt", event.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                保存后会写入提示词文件 {settings.data?.prompt_path}。
              </p>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

function NumberField({
  id,
  label,
  hint,
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
  value: number;
  onChange: (value: number) => void;
  disabled?: boolean;
  min?: number;
  max?: number;
  step?: number;
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="number"
        inputMode="decimal"
        disabled={disabled}
        min={min}
        max={max}
        step={step}
        value={Number.isFinite(value) ? value : ""}
        onChange={(event) => {
          const next = event.target.valueAsNumber;
          onChange(Number.isNaN(next) ? 0 : next);
        }}
      />
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { EmptyState, ErrorMessage } from "@/components/app/chrome";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  createProviderAdapter,
  deleteProviderAdapter,
  listProviderAdapters,
  updateProviderAdapter,
  type ProviderAdapter,
  type ProviderAdapterInput,
} from "@/lib/api";
import { cn } from "@/lib/utils";

type AdapterForm = ProviderAdapterInput & { secret: string };

const defaultRequestConfig = `{
  "model_path": "model",
  "prompt_path": "prompt",
  "image_path": "image",
  "metadata_path": "page",
  "image_format": "data_url",
  "max_image_edge": 0,
  "static": {}
}`;

const defaultResponseConfig = `{
  "text_path": "text",
  "confidence_path": "confidence",
  "metadata_path": "metadata"
}`;

function emptyAdapterForm(): AdapterForm {
  return {
    name: "",
    engine: "generic-http-json",
    endpoint: "",
    model: "",
    auth_type: "bearer",
    secret: "",
    timeout_seconds: 120,
    request_config_json: defaultRequestConfig,
    response_config_json: defaultResponseConfig,
    is_enabled: true,
  };
}

function adapterToForm(adapter: ProviderAdapter): AdapterForm {
  return {
    name: adapter.name,
    engine: adapter.engine,
    endpoint: adapter.endpoint,
    model: adapter.model,
    auth_type: adapter.auth_type,
    secret: "",
    timeout_seconds: adapter.timeout_seconds,
    request_config_json: adapter.request_config_json,
    response_config_json: adapter.response_config_json,
    is_enabled: adapter.is_enabled,
  };
}

function jsonError(value: string) {
  try {
    JSON.parse(value);
    return "";
  } catch (error) {
    return error instanceof Error ? error.message : "JSON 格式无效";
  }
}

export function ProviderAdaptersCard() {
  const queryClient = useQueryClient();
  const adapters = useQuery({ queryKey: ["provider-adapters"], queryFn: listProviderAdapters });
  const [editingID, setEditingID] = useState("");
  const [form, setForm] = useState<AdapterForm>(emptyAdapterForm);

  const requestError = jsonError(form.request_config_json);
  const responseError = jsonError(form.response_config_json);
  const editingAdapter = adapters.data?.find((adapter) => adapter.id === editingID);
  const valid = Boolean(
    form.name.trim()
      && form.engine.trim()
      && form.endpoint.trim()
      && form.model.trim()
      && Number.isInteger(form.timeout_seconds)
      && form.timeout_seconds >= 5
      && form.timeout_seconds <= 3600
      && (form.auth_type === "none" || form.secret.trim() || editingAdapter?.secret_set)
      && !requestError
      && !responseError,
  );

  const saveAdapter = useMutation({
    mutationFn: (input: AdapterForm) => {
      const payload: ProviderAdapterInput = { ...input };
      if (!input.secret.trim()) delete payload.secret;
      return editingID ? updateProviderAdapter(editingID, payload) : createProviderAdapter(payload);
    },
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: ["provider-adapters"] });
      setEditingID(saved.id);
      setForm(adapterToForm(saved));
      toast.success("Provider Adapter 已保存", { description: "密钥已保留，但不会通过 API 回显。" });
    },
    onError: (error: Error) => toast.error("保存 Adapter 失败", { description: error.message }),
  });

  const removeAdapter = useMutation({
    mutationFn: deleteProviderAdapter,
    onSuccess: async (_, id) => {
      await queryClient.invalidateQueries({ queryKey: ["provider-adapters"] });
      if (editingID === id) {
        setEditingID("");
        setForm(emptyAdapterForm());
      }
      toast.success("Provider Adapter 已删除");
    },
    onError: (error: Error) => toast.error("删除 Adapter 失败", { description: error.message }),
  });

  function edit(adapter: ProviderAdapter) {
    setEditingID(adapter.id);
    setForm(adapterToForm(adapter));
    saveAdapter.reset();
  }

  function createNew() {
    setEditingID("");
    setForm(emptyAdapterForm());
    saveAdapter.reset();
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Provider Adapters</CardTitle>
        <CardDescription>
          以纯数据配置外部识别服务的请求与响应映射。Adapter 不加载插件代码，密钥只写入且不回显。
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-5 xl:grid-cols-[minmax(0,0.8fr)_minmax(24rem,1.2fr)]">
        <div className="flex min-w-0 flex-col gap-2">
          <Button variant="secondary" className="justify-start" onClick={createNew}>
            <Plus data-icon="inline-start" />
            新建 Adapter
          </Button>
          <ErrorMessage message={adapters.error?.message} />
          {adapters.data?.map((adapter) => (
            <div
              key={adapter.id}
              className={cn(
                "flex items-center gap-2 rounded-lg border p-3",
                editingID === adapter.id && "border-primary bg-accent",
              )}
            >
              <button type="button" className="min-w-0 flex-1 text-left" onClick={() => edit(adapter)}>
                <span className="flex items-center gap-2 text-sm font-medium">
                  <span className="truncate">{adapter.name}</span>
                  <Badge variant={adapter.is_enabled ? "secondary" : "outline"}>
                    {adapter.is_enabled ? "启用" : "停用"}
                  </Badge>
                  {adapter.secret_set ? <Badge variant="outline">已配置密钥</Badge> : null}
                </span>
                <span className="mt-1 block truncate text-xs text-muted-foreground">
                  {adapter.engine} · {adapter.model || "无固定模型"}
                </span>
              </button>
              <Button variant="ghost" size="icon-sm" aria-label={`编辑 ${adapter.name}`} onClick={() => edit(adapter)}>
                <Pencil />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`删除 ${adapter.name}`}
                disabled={removeAdapter.isPending}
                onClick={() => removeAdapter.mutate(adapter.id)}
              >
                <Trash2 />
              </Button>
            </div>
          ))}
          {!adapters.isLoading && !adapters.data?.length ? (
            <EmptyState
              title="暂无 Provider Adapter"
              description="新建后可在普通识别和 A/B 实验中选择。"
              className="min-h-40"
            />
          ) : null}
        </div>

        <FieldGroup>
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="adapter-name">名称</FieldLabel>
              <Input
                id="adapter-name"
                placeholder="如 私有 OCR 服务"
                value={form.name}
                onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="adapter-engine">引擎标识</FieldLabel>
              <Select value={form.engine} onValueChange={(engine) => setForm((value) => ({ ...value, engine }))}>
                <SelectTrigger id="adapter-engine"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="generic-http-json">Generic HTTP JSON</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          </FieldGroup>

          <Field>
            <FieldLabel htmlFor="adapter-endpoint">Endpoint</FieldLabel>
            <Input
              id="adapter-endpoint"
              type="url"
              placeholder="https://ocr.example.com/v1/recognize"
              value={form.endpoint}
              onChange={(event) => setForm((value) => ({ ...value, endpoint: event.target.value }))}
            />
          </Field>

          <FieldGroup className="grid gap-4 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="adapter-model">模型</FieldLabel>
              <Input
                id="adapter-model"
                placeholder="服务要求的模型名称"
                value={form.model}
                onChange={(event) => setForm((value) => ({ ...value, model: event.target.value }))}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="adapter-auth-type">鉴权方式</FieldLabel>
              <Select
                value={form.auth_type || "none"}
                onValueChange={(value) => setForm((current) => ({ ...current, auth_type: value }))}
              >
                <SelectTrigger id="adapter-auth-type"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="none">无鉴权</SelectItem>
                    <SelectItem value="bearer">Bearer Token</SelectItem>
                    <SelectItem value="x-api-key">X-API-Key</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="adapter-timeout">超时（秒）</FieldLabel>
              <Input
                id="adapter-timeout"
                type="number"
                min={5}
                max={3600}
                step={1}
                value={Number.isFinite(form.timeout_seconds) ? form.timeout_seconds : ""}
                onChange={(event) => setForm((value) => ({ ...value, timeout_seconds: event.target.valueAsNumber }))}
              />
            </Field>
          </FieldGroup>

          <Field data-disabled={form.auth_type === "none"}>
            <FieldLabel htmlFor="adapter-secret">密钥</FieldLabel>
            <Input
              id="adapter-secret"
              type="password"
              autoComplete="new-password"
              disabled={form.auth_type === "none"}
              placeholder={editingID ? "留空保持现有密钥" : "输入 Token / API Key"}
              value={form.secret}
              onChange={(event) => setForm((value) => ({ ...value, secret: event.target.value }))}
            />
            <FieldDescription>Bearer 与 X-API-Key 需要密钥；保存后只返回是否已配置，不会回显内容。</FieldDescription>
          </Field>

          <Field data-invalid={Boolean(requestError)}>
            <FieldLabel htmlFor="adapter-request-config">请求配置 JSON</FieldLabel>
            <Textarea
              id="adapter-request-config"
              className="min-h-44 font-mono text-xs"
              spellCheck={false}
              aria-invalid={Boolean(requestError)}
              value={form.request_config_json}
              onChange={(event) => setForm((value) => ({ ...value, request_config_json: event.target.value }))}
            />
            <FieldDescription>配置模型、Prompt、图像和页面元数据写入请求 JSON 的数据路径。</FieldDescription>
            <FieldError>{requestError}</FieldError>
          </Field>

          <Field data-invalid={Boolean(responseError)}>
            <FieldLabel htmlFor="adapter-response-config">响应映射 JSON</FieldLabel>
            <Textarea
              id="adapter-response-config"
              className="min-h-32 font-mono text-xs"
              spellCheck={false}
              aria-invalid={Boolean(responseError)}
              value={form.response_config_json}
              onChange={(event) => setForm((value) => ({ ...value, response_config_json: event.target.value }))}
            />
            <FieldDescription>通过数据路径提取文本、置信度与可选元数据。</FieldDescription>
            <FieldError>{responseError}</FieldError>
          </Field>

          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>启用 Adapter</FieldTitle>
              <FieldDescription>停用后保留配置，但不会出现在新的识别或实验来源列表中。</FieldDescription>
            </FieldContent>
            <Switch
              aria-label="启用 Provider Adapter"
              checked={form.is_enabled}
              onCheckedChange={(checked) => setForm((value) => ({ ...value, is_enabled: checked }))}
            />
          </Field>

          <ErrorMessage message={saveAdapter.error?.message} />
          <Button className="self-start" disabled={!valid || saveAdapter.isPending} onClick={() => saveAdapter.mutate(form)}>
            {saveAdapter.isPending ? <Spinner data-icon="inline-start" /> : <Save data-icon="inline-start" />}
            {saveAdapter.isPending ? "保存中" : editingID ? "更新 Adapter" : "创建 Adapter"}
          </Button>
        </FieldGroup>
      </CardContent>
    </Card>
  );
}

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { CheckCircle2, Download, GitCommit, KeyRound, RefreshCw, RotateCw, Server, X } from "lucide-react";
import { EmptyState, ErrorMessage, MetricCard, PageHeader } from "../components/app/chrome";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Progress } from "../components/ui/progress";
import { ScrollArea } from "../components/ui/scroll-area";
import { applyUpdate, checkUpdate, dismissUpdate, getAdminToken, getUpdateStatus, getVersion, setAdminToken } from "../lib/api";
import { formatTime } from "../lib/utils";

const BUSY_STATE_LABELS: Record<string, string> = {
  checking: "正在检查更新…",
  downloading: "正在下载更新…",
  applying: "正在应用更新,请勿关闭…",
};

export function SystemPage() {
  const queryClient = useQueryClient();
  const [token, setToken] = useState(getAdminToken);
  const version = useQuery({ queryKey: ["version"], queryFn: getVersion });
  const status = useQuery({
    queryKey: ["update-status"],
    queryFn: getUpdateStatus,
    refetchInterval: (query) => {
      const state = query.state.data?.state;
      return state === "checking" || state === "downloading" || state === "applying" ? 1500 : 5000;
    },
  });
  const check = useMutation({
    mutationFn: checkUpdate,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["update-status"] }),
  });
  const apply = useMutation({
    mutationFn: applyUpdate,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["update-status"] }),
  });
  const dismiss = useMutation({
    mutationFn: dismissUpdate,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["update-status"] }),
  });

  const snapshot = status.data;
  const result = check.data;

  const currentVersion = snapshot?.current_version;
  const seenVersion = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!currentVersion) {
      return;
    }
    if (seenVersion.current && seenVersion.current !== currentVersion) {
      window.location.reload();
      return;
    }
    seenVersion.current = currentVersion;
  }, [currentVersion]);

  const state = snapshot?.state ?? "";
  const busy = ["checking", "downloading", "applying"].includes(state);
  const ready = state === "ready";
  const hasUpdate = ready || result?.has_update;
  const progress = Math.max(0, Math.min(Math.round(snapshot?.progress ?? 0), 100));
  const showProgress = state === "downloading" || state === "applying";
  const upToDate = !busy && !hasUpdate && result?.has_update === false;
  const latest = snapshot?.latest_version || result?.latest_version || "";
  const notes = snapshot?.release_notes || result?.release_notes || "";
  const error = snapshot?.error || result?.error || status.error?.message || check.error?.message || apply.error?.message;
  const commit = version.data?.commit || "";

  return (
    <div className="space-y-5">
      <PageHeader title="系统" description={busy ? BUSY_STATE_LABELS[state] : "版本信息与 OTA 更新"}>
        <Button variant="secondary" disabled={check.isPending || busy} onClick={() => check.mutate()}>
          <RefreshCw className={check.isPending ? "size-4 animate-spin" : "size-4"} />
          检查
        </Button>
        {hasUpdate ? (
          <Button disabled={apply.isPending || busy} onClick={() => apply.mutate()}>
            {ready ? <RotateCw className="size-4" /> : <Download className="size-4" />}
            {ready ? "重启应用" : "下载更新"}
          </Button>
        ) : null}
        {ready ? (
          <Button variant="ghost" disabled={dismiss.isPending} onClick={() => dismiss.mutate()}>
            <X className="size-4" />
            忽略
          </Button>
        ) : null}
      </PageHeader>

      <ErrorMessage message={error} />

      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard icon={<Server className="size-4" />} label="当前版本" value={snapshot?.current_version || version.data?.version || "dev"} />
        <MetricCard icon={<Download className="size-4" />} label="最新版本" value={latest || (result?.has_update === false ? "已是最新" : "--")} />
        <MetricCard
          icon={<GitCommit className="size-4" />}
          label="Commit"
          value={commit ? <span title={commit}>{commit.slice(0, 7)}</span> : "--"}
        />
      </section>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">版本信息</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <Info label="构建时间" value={version.data?.build_time || ""} />
          <Info label="更新通道" value={version.data?.update_channel || result?.channel || ""} />
          <Info label="更新来源" value={version.data?.update_source === "proxy" ? "代理镜像" : version.data?.update_source ? "GitHub 直连" : ""} />
          <Info label="仓库" value={version.data?.update_repo || ""} />
          <Info label="上次检查" value={formatTime(snapshot?.last_check)} />
          <div className="min-w-0 md:col-span-2">
            <div className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
              <KeyRound className="size-3" />
              管理令牌(远程触发更新时需与服务端 update.admin_token 一致)
            </div>
            <Input
              type="password"
              value={token}
              placeholder="本机访问无需填写"
              onChange={(e) => {
                setToken(e.target.value);
                setAdminToken(e.target.value.trim());
              }}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="space-y-3 pb-3">
          <div className="flex items-center justify-between gap-3">
            <CardTitle className="text-base">更新状态</CardTitle>
            <Badge value={snapshot?.state || "idle"} />
          </div>
          {upToDate ? (
            <div className="inline-flex items-center gap-1.5 text-sm font-medium text-primary">
              <CheckCircle2 className="size-4" />
              已是最新版本,无需更新
            </div>
          ) : null}
          {busy ? <p className="text-sm text-muted-foreground">{BUSY_STATE_LABELS[state]}</p> : null}
        </CardHeader>
        <CardContent className="space-y-4">
          {showProgress ? (
            <div className="flex items-center gap-3">
              <Progress value={progress} className="flex-1" />
              {state === "downloading" ? <span className="w-10 text-right text-xs tabular-nums text-muted-foreground">{progress}%</span> : null}
            </div>
          ) : null}
          {notes ? (
            <ScrollArea className="h-72 rounded-md border bg-muted/50">
              <pre className="whitespace-pre-wrap p-3 text-sm">{notes}</pre>
            </ScrollArea>
          ) : (
            <EmptyState title="暂无发布说明" className="min-h-32" />
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Info({ label, value }: { label: string; value?: string }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm font-medium">{value || "--"}</div>
    </div>
  );
}

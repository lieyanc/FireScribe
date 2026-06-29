import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Download, RefreshCw, RotateCw, X } from "lucide-react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { applyUpdate, checkUpdate, dismissUpdate, getUpdateStatus, getVersion } from "../lib/api";
import { formatTime } from "../lib/utils";

export function SystemPage() {
  const queryClient = useQueryClient();
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
  const busy = ["checking", "downloading", "applying"].includes(snapshot?.state ?? "");
  const ready = snapshot?.state === "ready";
  const hasUpdate = ready || result?.has_update;
  const progress = Math.round(snapshot?.progress ?? 0);
  const latest = snapshot?.latest_version || result?.latest_version || "";
  const notes = snapshot?.release_notes || result?.release_notes || "";
  const error = snapshot?.error || result?.error || status.error?.message || check.error?.message || apply.error?.message;

  return (
    <div className="space-y-4">
      <section className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-xl font-semibold">系统</h1>
          <p className="text-sm text-muted-foreground">版本信息与 OTA 更新</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" disabled={check.isPending || busy} onClick={() => check.mutate()}>
            <RefreshCw className={check.isPending ? "h-4 w-4 animate-spin" : "h-4 w-4"} />
            检查
          </Button>
          {hasUpdate ? (
            <Button disabled={apply.isPending || busy} onClick={() => apply.mutate()}>
              {ready ? <RotateCw className="h-4 w-4" /> : <Download className="h-4 w-4" />}
              {ready ? "重启应用" : "下载更新"}
            </Button>
          ) : null}
          {ready ? (
            <Button variant="ghost" disabled={dismiss.isPending} onClick={() => dismiss.mutate()}>
              <X className="h-4 w-4" />
              忽略
            </Button>
          ) : null}
        </div>
      </section>

      {error ? <p className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</p> : null}

      <section className="panel p-4">
        <div className="grid gap-3 md:grid-cols-2">
          <Info label="当前版本" value={snapshot?.current_version || version.data?.version || "dev"} />
          <Info label="最新版本" value={latest || (result?.has_update === false ? "已是最新" : "")} />
          <Info label="Commit" value={version.data?.commit || ""} />
          <Info label="构建时间" value={version.data?.build_time || ""} />
          <Info label="更新通道" value={version.data?.update_channel || result?.channel || ""} />
          <Info label="仓库" value={version.data?.update_repo || ""} />
        </div>
      </section>

      <section className="panel p-4">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Badge value={snapshot?.state || "idle"} />
            {result?.has_update === false ? (
              <span className="inline-flex items-center gap-1 text-sm text-primary">
                <CheckCircle2 className="h-4 w-4" />
                已是最新
              </span>
            ) : null}
          </div>
          {snapshot?.last_check ? <span className="text-xs text-muted-foreground">{formatTime(snapshot.last_check)}</span> : null}
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-muted">
          <div className="h-full bg-primary transition-all" style={{ width: `${Math.max(0, Math.min(progress, 100))}%` }} />
        </div>
        {notes ? <pre className="mt-4 max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-muted/70 p-3 text-sm">{notes}</pre> : null}
      </section>
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

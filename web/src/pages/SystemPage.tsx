import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Download, FileText, GitCommit, RefreshCw, RotateCw, Server, X } from "lucide-react";
import { toast } from "sonner";
import { ErrorMessage, MetricCard, PageHeader } from "../components/app/chrome";
import { Alert, AlertDescription, AlertTitle } from "../components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "../components/ui/alert-dialog";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "../components/ui/empty";
import { Field, FieldLabel } from "../components/ui/field";
import { Progress } from "../components/ui/progress";
import { ScrollArea } from "../components/ui/scroll-area";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { applyUpdate, checkUpdate, dismissUpdate, getUpdateStatus, getVersion } from "../lib/api";
import { formatTime, statusLabel } from "../lib/format";

const BUSY_STATE_LABELS: Record<string, string> = {
  checking: "正在检查更新…",
  downloading: "正在下载并验证更新…",
  applying: "正在应用更新，请勿关闭…",
};

function updateStateVariant(state: string): "default" | "secondary" | "destructive" | "outline" {
  if (state === "failed") return "destructive";
  if (["checking", "downloading", "applying", "ready"].includes(state)) return "default";
  return "secondary";
}

export function SystemPage() {
  const queryClient = useQueryClient();
  const [applyDialogOpen, setApplyDialogOpen] = useState(false);
  const [dismissDialogOpen, setDismissDialogOpen] = useState(false);
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
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ["update-status"] });
      if (result.error) {
        toast.error("检查更新失败", { description: result.error });
      } else if (result.has_update) {
        toast.success(`发现新版本 ${result.latest_version || ""}`.trim());
      } else {
        toast.success("当前已是最新版本");
      }
    },
    onError: (error: Error) => toast.error("检查更新失败", { description: error.message }),
  });

  const snapshot = status.data;
  const result = check.data;
  const state = snapshot?.state ?? "idle";
  const busy = ["checking", "downloading", "applying"].includes(state);
  const ready = state === "ready";
  const hasUpdate = ready || Boolean(result?.has_update);
  const latest = snapshot?.latest_version || result?.latest_version || "";
  const notes = snapshot?.release_notes || result?.release_notes || "";
  const error =
    snapshot?.error ||
    result?.error ||
    version.error?.message ||
    status.error?.message ||
    check.error?.message;
  const upToDate = !error && !busy && !hasUpdate && result?.has_update === false;
  const progressSource = state === "downloading" ? snapshot?.download_progress : snapshot?.progress;
  const progress = Math.max(0, Math.min(Math.round(progressSource ?? 0), 100));
  const showProgress = state === "downloading" || state === "applying";
  const commit = version.data?.commit || "";

  const apply = useMutation({
    mutationFn: applyUpdate,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["update-status"] });
      setApplyDialogOpen(false);
      toast.success(ready ? "重启请求已提交" : "更新任务已启动");
    },
    onError: (mutationError: Error) => toast.error(ready ? "重启失败" : "启动更新失败", { description: mutationError.message }),
  });
  const dismiss = useMutation({
    mutationFn: dismissUpdate,
    onSuccess: () => {
      check.reset();
      void queryClient.invalidateQueries({ queryKey: ["update-status"] });
      setDismissDialogOpen(false);
      toast.success("已忽略本次待应用更新");
    },
    onError: (mutationError: Error) => toast.error("忽略更新失败", { description: mutationError.message }),
  });

  const currentVersion = snapshot?.current_version;
  const seenVersion = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!currentVersion) return;
    if (seenVersion.current && seenVersion.current !== currentVersion) {
      window.location.reload();
      return;
    }
    seenVersion.current = currentVersion;
  }, [currentVersion]);

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="系统" description={busy ? BUSY_STATE_LABELS[state] : "查看版本信息并安全管理 OTA 更新。"}>
        <Button variant="secondary" disabled={check.isPending || busy || apply.isPending} onClick={() => check.mutate()}>
          {check.isPending ? <Spinner /> : <RefreshCw />}
          {check.isPending ? "检查中" : "检查更新"}
        </Button>

        {hasUpdate ? (
          <AlertDialog open={applyDialogOpen} onOpenChange={setApplyDialogOpen}>
            <AlertDialogTrigger asChild>
              <Button disabled={apply.isPending || busy}>
                {ready ? <RotateCw /> : <Download />}
                {ready ? "重启应用" : "下载并更新"}
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{ready ? "确认重启并应用更新？" : "确认下载并应用更新？"}</AlertDialogTitle>
                <AlertDialogDescription>
                  {ready
                    ? `版本 ${latest || "更新"} 已准备就绪。应用会重启服务，当前连接将短暂中断。`
                    : `将下载、校验并应用版本 ${latest || "最新版本"}。稳定通道可能在完成后自动重启服务。`}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel disabled={apply.isPending}>取消</AlertDialogCancel>
                <AlertDialogAction
                  disabled={apply.isPending}
                  onClick={(event) => {
                    event.preventDefault();
                    apply.mutate();
                  }}
                >
                  {apply.isPending ? (
                    <Spinner />
                  ) : ready ? (
                    <RotateCw />
                  ) : (
                    <Download />
                  )}
                  {apply.isPending ? "提交中" : ready ? "确认重启" : "确认更新"}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        ) : null}

        {ready ? (
          <AlertDialog open={dismissDialogOpen} onOpenChange={setDismissDialogOpen}>
            <AlertDialogTrigger asChild>
              <Button variant="ghost" disabled={dismiss.isPending || apply.isPending}>
                <X />
                忽略更新
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>忽略这次待应用更新？</AlertDialogTitle>
                <AlertDialogDescription>
                  已下载的版本 {latest || ""} 将被移除。之后仍可重新检查并下载该版本。
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel disabled={dismiss.isPending}>取消</AlertDialogCancel>
                <AlertDialogAction
                  disabled={dismiss.isPending}
                  onClick={(event) => {
                    event.preventDefault();
                    dismiss.mutate();
                  }}
                >
                  {dismiss.isPending ? <Spinner /> : <X />}
                  {dismiss.isPending ? "处理中" : "确认忽略"}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        ) : null}
      </PageHeader>

      <ErrorMessage message={error || apply.error?.message || dismiss.error?.message} />

      <section className="grid gap-3 md:grid-cols-3">
        <MetricCard
          icon={<Server />}
          label="当前版本"
          value={version.isPending && !snapshot ? <Skeleton className="h-5 w-16" /> : snapshot?.current_version || version.data?.version || "--"}
        />
        <MetricCard
          icon={<Download />}
          label="最新版本"
          value={status.isPending && !result ? <Skeleton className="h-5 w-16" /> : latest || (upToDate ? "已是最新" : "--")}
        />
        <MetricCard
          icon={<GitCommit />}
          label="Commit"
          value={version.isPending ? <Skeleton className="h-5 w-14" /> : commit ? <span title={commit}>{commit.slice(0, 7)}</span> : "--"}
        />
      </section>

      <Card>
        <CardHeader>
          <CardTitle>版本信息</CardTitle>
          <CardDescription>构建信息来自当前服务；更新操作需要管理员账户。</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-2">
            <Info label="构建时间" value={version.isPending ? <Skeleton className="h-4 w-28" /> : version.data?.build_time} />
            <Info label="更新通道" value={version.data?.update_channel || result?.channel} />
            <Info
              label="更新来源"
              value={version.data?.update_source === "proxy" ? "代理镜像" : version.data?.update_source ? "GitHub 直连" : undefined}
            />
            <Info label="仓库" value={version.data?.update_repo} />
            <Info label="上次检查" value={formatTime(snapshot?.last_check)} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-3">
            <CardTitle>更新状态</CardTitle>
            <Badge variant={updateStateVariant(state)}>{statusLabel(state)}</Badge>
          </div>
          <CardDescription>{busy ? BUSY_STATE_LABELS[state] : "发布说明与更新任务进度会在此处自动刷新。"}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {upToDate ? (
            <Alert>
              <CheckCircle2 />
              <AlertTitle>已是最新版本</AlertTitle>
              <AlertDescription>当前通道没有需要安装的更新。</AlertDescription>
            </Alert>
          ) : null}

          {showProgress ? (
            <Field>
              <FieldLabel htmlFor="update-progress" className="w-full">
                <span>{BUSY_STATE_LABELS[state]}</span>
                <span className="ml-auto tabular-nums">{progress}%</span>
              </FieldLabel>
              <Progress id="update-progress" value={progress} />
            </Field>
          ) : null}

          {status.isPending && !notes ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-20 w-full" />
            </div>
          ) : notes ? (
            <ScrollArea className="h-72 rounded-md border">
              <pre className="whitespace-pre-wrap p-4 text-sm">{notes}</pre>
            </ScrollArea>
          ) : (
            <Empty className="min-h-40">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <FileText />
                </EmptyMedia>
                <EmptyTitle>暂无发布说明</EmptyTitle>
                <EmptyDescription>检查到新版本后，这里会显示对应的变更内容。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Info({ label, value }: { label: string; value?: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm font-medium">{value || "--"}</div>
    </div>
  );
}

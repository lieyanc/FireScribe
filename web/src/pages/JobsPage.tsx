import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, RotateCcw } from "lucide-react";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { cancelJob, listJobs, retryJob } from "../lib/api";
import { formatTime } from "../lib/utils";

export function JobsPage() {
  const queryClient = useQueryClient();
  const jobs = useQuery({ queryKey: ["jobs"], queryFn: listJobs, refetchInterval: 2500 });
  const cancel = useMutation({
    mutationFn: cancelJob,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  });
  const retry = useMutation({
    mutationFn: retryJob,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["jobs"] }),
  });

  return (
    <div className="space-y-4">
      <section className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">任务</h1>
      </section>
      {cancel.error ? <p className="text-sm text-red-700">{cancel.error.message}</p> : null}
      {retry.error ? <p className="text-sm text-red-700">{retry.error.message}</p> : null}
      <section className="panel overflow-hidden">
        <div className="grid grid-cols-[minmax(180px,1fr)_110px_110px_150px_118px] border-b border-border bg-muted px-3 py-2 text-xs font-medium text-muted-foreground">
          <div>类型</div>
          <div>状态</div>
          <div>次数</div>
          <div>创建</div>
          <div>操作</div>
        </div>
        {jobs.data?.length ? (
          jobs.data.map((job) => (
            <div key={job.id} className="grid grid-cols-[minmax(180px,1fr)_110px_110px_150px_118px] items-center border-b border-border px-3 py-3 text-sm last:border-b-0">
              <div className="min-w-0">
                <div className="truncate font-medium">{job.type}</div>
                {job.last_error ? <div className="mt-1 truncate text-xs text-red-700">{job.last_error}</div> : null}
              </div>
              <Badge value={job.status} />
              <div className="text-muted-foreground">
                {job.attempts}/{job.max_attempts}
              </div>
              <div className="text-muted-foreground">{formatTime(job.created_at)}</div>
              <div className="flex gap-2">
                <Button
                  variant="secondary"
                  size="icon"
                  title="重试"
                  disabled={job.status !== "failed" || retry.isPending}
                  onClick={() => retry.mutate(job.id)}
                >
                  <RotateCcw className="h-4 w-4" />
                </Button>
                <Button
                  variant="secondary"
                  size="icon"
                  title="取消"
                  disabled={!["queued", "running"].includes(job.status) || cancel.isPending}
                  onClick={() => cancel.mutate(job.id)}
                >
                  <Ban className="h-4 w-4" />
                </Button>
              </div>
            </div>
          ))
        ) : (
          <div className="px-4 py-10 text-center text-sm text-muted-foreground">{jobs.isLoading ? "加载中" : "暂无任务"}</div>
        )}
      </section>
    </div>
  );
}

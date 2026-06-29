import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatTime(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function statusLabel(value: string) {
  const labels: Record<string, string> = {
    new: "新建",
    importing: "导入中",
    ready: "就绪",
    recognizing: "识别中",
    reviewing: "校对中",
    finalized: "已定稿",
    failed: "失败",
    extracted: "已拆页",
    recognized: "已识别",
    verified: "已确认",
    queued: "排队",
    running: "运行中",
    succeeded: "成功",
    canceled: "已取消",
    draft: "草稿",
    candidate: "候选",
    manual: "人工",
    final: "定稿",
    open: "未解决",
    resolved: "已解决",
    ignored: "忽略",
    page_note: "批注",
    uncertain_text: "存疑",
  };
  return labels[value] ?? value;
}
